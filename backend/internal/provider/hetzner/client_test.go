package hetzner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

type transport func(*http.Request) (*http.Response, error)

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }

func fixture(t *testing.T, handler func(*http.Request) *http.Response) (*Client, provider.Binding) {
	t.Helper()
	w, _ := uuid.NewV7()
	id, _ := uuid.NewV7()
	b, _ := provider.NewBinding(w, id, Family, 1)
	c, err := New(b, connection.Credential{Type: "bearer_v1", Token: "synthetic-token"}, outbound.NewBudget())
	if err != nil {
		t.Fatal(err)
	}
	c.http = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Host != "api.hetzner.cloud" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Fatal("authentication/endpoint")
		}
		return handler(r), nil
	}), CheckRedirect: func(*http.Request, []*http.Request) error { return outbound.ErrEndpoint }}
	return c, b
}

func reply(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestCloudPagesDetailsAndIdentityRemainTruthful(t *testing.T) {
	c, b := fixture(t, func(r *http.Request) *http.Response {
		if strings.HasSuffix(r.URL.Path, "/42") {
			return reply(200, `{"server":{"id":42,"name":"Safe","status":"off","location":{"name":"nbg1"},"server_type":{"name":"cx22"},"user_data":"synthetic-secret"}}`)
		}
		return reply(200, `{"servers":[{"id":42,"name":"Safe","status":"new-state","location":{"name":"nbg1"},"server_type":{"name":"cx22"},"root_password":"synthetic-secret"}],"meta":{"pagination":{"page":1,"next_page":2}}}`)
	})
	page, err := c.Page(context.Background(), b, "")
	if err != nil || page.Next != "2" || len(page.Data) != 1 || page.Data[0].Status != "unknown" || page.Data[0].NativeStatus != "new-state" {
		t.Fatal(page, err)
	}
	encoded, _ := json.Marshal(page)
	if strings.Contains(string(encoded), "synthetic-secret") {
		t.Fatal("raw SDK metadata leaked")
	}
	detail, err := c.Read(context.Background(), b, "42")
	if err != nil || detail.Status != "stopped" || detail.Details.Region != "nbg1" {
		t.Fatal(detail, err)
	}
	if account, _, err := c.Validate(context.Background(), c.credential); err != provider.ErrIdentityUnverified || account != "" {
		t.Fatal("invented project identity")
	}
	other, _ := uuid.NewV7()
	wrong, _ := provider.NewBinding(b.Workspace(), other, Family, 1)
	if _, err = c.Page(context.Background(), wrong, ""); err != provider.ErrBinding {
		t.Fatal("connection fallback")
	}
}

func TestCloudRejectsMalformedDeniedAndRunawayPages(t *testing.T) {
	for _, probe := range []struct {
		code int
		body string
	}{
		{401, `{"error":{"message":"synthetic-secret"}}`}, {403, `{}`},
		{200, `not json`}, {200, `{}`}, {200, `{"servers":[],"meta":{"pagination":{"page":1,"next_page":1}}}`},
		{200, `{"servers":[{"id":0}],"meta":{"pagination":{"page":1}}}`},
		{200, strings.Repeat("x", (8<<20)+1)},
	} {
		calls := 0
		c, b := fixture(t, func(*http.Request) *http.Response { calls++; return reply(probe.code, probe.body) })
		if _, err := c.Page(context.Background(), b, ""); err == nil || strings.Contains(err.Error(), "synthetic-secret") || calls != 1 {
			t.Fatal("unsafe response accepted")
		}
	}
	c, b := fixture(t, func(*http.Request) *http.Response { return reply(429, `{}`) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := c.Page(ctx, b, ""); err == nil {
		t.Fatal("throttle ignored")
	}
	if _, err := c.Page(context.Background(), b, "https://evil.example/page"); err == nil {
		t.Fatal("URL cursor accepted")
	}
}
