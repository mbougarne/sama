package vultr

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
		if r.Method != "GET" || r.URL.Host != "api.vultr.com" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Fatal("endpoint/authentication")
		}
		return handler(r), nil
	}), CheckRedirect: func(*http.Request, []*http.Request) error { return outbound.ErrEndpoint }}
	return c, b
}

func reply(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestReadContractsIdentityAndConnectionIsolation(t *testing.T) {
	c, b := fixture(t, func(r *http.Request) *http.Response {
		if strings.HasSuffix(r.URL.Path, "/native-42") {
			return reply(200, `{"instance":{"id":"native-42","status":"active","power_status":"stopped","server_status":"ok"}}`)
		}
		if strings.HasSuffix(r.URL.Path, "/account") {
			return reply(200, `{"account":{"name":"synthetic-secret","acl":["subscriptions"]}}`)
		}
		return reply(200, `{"instances":[{"id":"native-42","label":"safe","status":"active","power_status":"new-state","server_status":"ok","region":"ams","plan":"vc2","default_password":"synthetic-secret"}],"meta":{"links":{"next":"next-cursor"}}}`)
	})
	page, err := c.Page(context.Background(), b, "")
	if err != nil || len(page.Data) != 1 || page.Data[0].Status != "unknown" || page.Next == "" {
		t.Fatal(page, err)
	}
	encoded, _ := json.Marshal(page)
	if strings.Contains(string(encoded), "synthetic-secret") {
		t.Fatal("raw fields leaked")
	}
	detail, err := c.Read(context.Background(), b, "native-42")
	if err != nil || detail.Status != "stopped" {
		t.Fatal(detail, err)
	}
	account, _, err := c.Validate(context.Background(), c.credential)
	if err != provider.ErrIdentityUnverified || account != "" {
		t.Fatal("invented account identity")
	}
	wrong, _ := provider.NewBinding(b.Workspace(), b.Connection(), Family, 2)
	if _, err = c.Page(context.Background(), wrong, ""); err != provider.ErrBinding {
		t.Fatal("credential version fallback")
	}
	if _, err = c.Read(context.Background(), b, "../account"); err == nil {
		t.Fatal("path injection")
	}
}

func TestUnsafePagesDenialsAndResponseBounds(t *testing.T) {
	for _, probe := range []struct {
		code int
		body string
	}{
		{401, `{"message":"synthetic-secret"}`}, {403, `{}`}, {200, `invalid`}, {200, `{}`}, {200, strings.Repeat("x", (8<<20)+1)},
		{200, `{"instances":[],"meta":{"links":{"next":"https://evil.example"}}}`}, {200, `{"instances":[{"id":"../unsafe"}],"meta":{"links":{}}}`},
	} {
		calls := 0
		c, b := fixture(t, func(*http.Request) *http.Response { calls++; return reply(probe.code, probe.body) })
		if _, err := c.Page(context.Background(), b, ""); err == nil || strings.Contains(err.Error(), "synthetic-secret") || calls != 1 {
			t.Fatal("unsafe/denied response accepted")
		}
	}
	c, b := fixture(t, func(*http.Request) *http.Response { return reply(429, `{}`) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := c.Page(ctx, b, ""); err == nil {
		t.Fatal("throttle ignored")
	}
	if _, err := c.Page(context.Background(), b, "https://evil.example"); err == nil {
		t.Fatal("URL cursor accepted")
	}
}

func TestEmptyInventoryAndLaterPageFailure(t *testing.T) {
	c, b := fixture(t, func(r *http.Request) *http.Response {
		if r.URL.Query().Get("cursor") != "" && r.URL.Query().Get("cursor") != "1" {
			return reply(403, `{}`)
		}
		return reply(200, `{"instances":[],"meta":{"links":{"next":""}}}`)
	})
	page, err := c.Page(context.Background(), b, "")
	if err != nil || len(page.Data) != 0 || page.Next != "" {
		t.Fatal(page, err)
	}
	if _, err := c.Page(context.Background(), b, "next-cursor"); err == nil {
		t.Fatal("partial read completed")
	}
}
