package digitalocean

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

func powerFixture(t *testing.T, handler func(*http.Request) (*http.Response, error)) (*Client, provider.Binding) {
	t.Helper()
	w, _ := uuid.NewV7()
	id, _ := uuid.NewV7()
	b, _ := provider.NewBinding(w, id, Family, 1)
	c, err := New(b, connection.Credential{Type: "bearer_v1", Token: "synthetic-token"}, outbound.NewBudget())
	if err != nil {
		t.Fatal(err)
	}
	c.http = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.digitalocean.com" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Fatal("request boundary")
		}
		return handler(r)
	})}
	return c, b
}

func powerReply(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func actionBody(kind, status string) string {
	return fmt.Sprintf(`{"action":{"id":9,"type":%q,"status":%q,"resource_id":42,"resource_type":"droplet","completed_at":"2026-10-19T00:00:00Z"}}`, kind, status)
}

func TestDropletPowerContracts(t *testing.T) {
	for _, probe := range []struct {
		action, kind, initial, final string
		high                         bool
	}{
		{"compute.server.power_on", "power_on", "off", "active", false},
		{"compute.server.shutdown", "shutdown", "active", "off", false},
		{"compute.server.reboot", "reboot", "active", "active", false},
		{"compute.server.power_off", "power_off", "active", "off", true},
	} {
		t.Run(probe.action, func(t *testing.T) {
			status := probe.initial
			native := "in-progress"
			posts := 0
			c, b := powerFixture(t, func(r *http.Request) (*http.Response, error) {
				if r.Method == "POST" {
					posts++
					var body struct {
						Type string `json:"type"`
					}
					if r.URL.Path != "/v2/droplets/42/actions" || json.NewDecoder(r.Body).Decode(&body) != nil || body.Type != probe.kind {
						t.Fatal("native action substituted")
					}
					return powerReply(201, actionBody(probe.kind, native)), nil
				}
				if r.URL.Path == "/v2/droplets/42/actions/9" {
					return powerReply(200, actionBody(probe.kind, native)), nil
				}
				if r.URL.Path != "/v2/droplets/42" {
					t.Fatal("unsafe read path")
				}
				return powerReply(200, fmt.Sprintf(`{"droplet":{"id":42,"name":"target","status":%q,"locked":false}}`, status)), nil
			})
			ctx := context.Background()
			pre, err := c.Inspect(ctx, b, probe.action, "42")
			if err != nil || !pre.Allowed || pre.HighImpact != probe.high {
				t.Fatal(pre, err)
			}
			r, err := c.Submit(ctx, b, probe.action, "42", pre.Fingerprint)
			if err != nil || r.Reference != "9" || posts != 1 {
				t.Fatal(r, err)
			}
			if o, err := c.Observe(ctx, b, probe.action, "42", &r.Reference); err != nil || o.Outcome != "pending" {
				t.Fatal(o, err)
			}
			if o, err := c.Observe(ctx, b, probe.action, "42", nil); err != nil || o.Outcome != "unknown" {
				t.Fatal("missing receipt became success")
			}
			native = "errored"
			if o, err := c.Observe(ctx, b, probe.action, "42", &r.Reference); err != nil || o.Outcome != "failed" || !o.NativeEvidence {
				t.Fatal(o, err)
			}
			native = "completed"
			status = probe.final
			if o, err := c.Observe(ctx, b, probe.action, "42", &r.Reference); err != nil || o.Outcome != "succeeded" || !o.NativeEvidence {
				t.Fatal(o, err)
			}
		})
	}
}

func TestDropletPowerFailuresAndStaleReview(t *testing.T) {
	for _, code := range []int{201, 403, 408, 429, 500, 0} {
		posts := 0
		c, b := powerFixture(t, func(r *http.Request) (*http.Response, error) {
			if r.Method == "POST" {
				posts++
				if code == 0 {
					return nil, errors.New("synthetic-secret")
				}
				return powerReply(code, `{}`), nil
			}
			return powerReply(200, `{"droplet":{"id":42,"name":"target","status":"off","locked":false}}`), nil
		})
		pre, _ := c.Inspect(context.Background(), b, "compute.server.power_on", "42")
		r, err := c.Submit(context.Background(), b, "compute.server.power_on", "42", [32]byte{})
		if err != nil || r.Outcome != "rejected" || posts != 0 {
			t.Fatal("unreviewed action sent")
		}
		r, err = c.Submit(context.Background(), b, "compute.server.power_on", "42", pre.Fingerprint)
		if posts != 1 || r.Reference != "" || r.Outcome == "accepted" || err != nil && strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal(r, err, posts)
		}
		if _, err = c.Inspect(context.Background(), provider.Binding{}, "compute.server.power_on", "42"); err != provider.ErrBinding {
			t.Fatal("binding fallback")
		}
	}
}

func TestDropletObservationRejectsUnrelatedOrIncompleteEvidence(t *testing.T) {
	correct := actionBody("reboot", "completed")
	for _, body := range []string{
		strings.ReplaceAll(correct, `"resource_id":42`, `"resource_id":43`),
		strings.ReplaceAll(correct, `"type":"reboot"`, `"type":"power_on"`),
		strings.ReplaceAll(correct, `"id":9`, `"id":10`),
		strings.ReplaceAll(correct, `"resource_type":"droplet"`, `"resource_type":"volume"`),
		strings.ReplaceAll(correct, `"completed_at":"2026-10-19T00:00:00Z"`, `"completed_at":null`),
	} {
		c, b := powerFixture(t, func(r *http.Request) (*http.Response, error) {
			if strings.Contains(r.URL.Path, "/actions/") {
				return powerReply(200, body), nil
			}
			return powerReply(200, `{"droplet":{"id":42,"name":"target","status":"active","locked":false}}`), nil
		})
		reference := "9"
		o, err := c.Observe(context.Background(), b, "compute.server.reboot", "42", &reference)
		if err == nil && (o.Outcome != "unknown" || o.NativeEvidence) {
			t.Fatal("unrelated/incomplete action proved reboot", o)
		}
	}
}
