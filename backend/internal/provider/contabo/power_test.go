package contabo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"sama/backend/internal/provider"
)

func powerFixture(t *testing.T, status *string, post func(*http.Request) (*http.Response, error)) (*Client, provider.Binding) {
	t.Helper()
	c, b := fixture(t, func(*http.Request) *http.Response { return nil }, func(*http.Request) *http.Response { return tokenReply() })
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		request, err := uuid.Parse(r.Header.Get("x-request-id"))
		if err != nil || request.Version() != 4 || r.URL.Host != "api.contabo.com" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Fatal("request/authentication boundary")
		}
		if r.Method == "POST" {
			return post(r)
		}
		if r.URL.Path != "/v1/compute/instances/42" {
			t.Fatal("read target changed")
		}
		return reply(200, fmt.Sprintf(`{"data":[{"instanceId":42,"name":"target","status":%q,"suspended":false,"tenantId":"DE","customerId":"54321"}]}`, *status)), nil
	})
	return c, b
}

func TestContaboPowerContracts(t *testing.T) {
	for _, probe := range []struct{ action, path, initial, final string }{
		{"compute.server.power_on", "start", "stopped", "running"},
		{"compute.server.shutdown", "shutdown", "running", "stopped"},
		{"compute.server.reboot", "restart", "running", "running"},
		{"compute.server.power_off", "stop", "running", "stopped"},
	} {
		t.Run(probe.action, func(t *testing.T) {
			status := probe.initial
			posts := 0
			c, b := powerFixture(t, &status, func(r *http.Request) (*http.Response, error) {
				posts++
				if r.URL.Path != "/v1/compute/instances/42/actions/"+probe.path {
					t.Fatal("native action substituted")
				}
				return reply(201, fmt.Sprintf(`{"data":[{"instanceId":42,"action":%q}]}`, probe.path)), nil
			})
			ctx := context.Background()
			pre, err := c.Inspect(ctx, b, probe.action, "42")
			if err != nil || !pre.Allowed || pre.HighImpact != (probe.path == "stop") {
				t.Fatal(pre, err)
			}
			r, err := c.Submit(ctx, b, probe.action, "42", pre.Fingerprint)
			if err != nil || r.Reference != "" || r.Outcome != "ambiguous" || posts != 1 {
				t.Fatal("correlation used as action receipt", r, err)
			}
			status = probe.final
			o, err := c.Observe(ctx, b, probe.action, "42", nil)
			if err != nil {
				t.Fatal(err)
			}
			if probe.path == "restart" {
				if o.Outcome != "unknown" || o.NativeEvidence {
					t.Fatal("running state proved reboot")
				}
			} else if o.Outcome != "succeeded" || !o.NativeEvidence {
				t.Fatal(o)
			}
		})
	}
}

func TestContaboMutationNeverRefreshesAndResubmits(t *testing.T) {
	for _, code := range []int{201, 401, 403, 408, 429, 500, 0} {
		status := "stopped"
		posts := 0
		c, b := powerFixture(t, &status, func(*http.Request) (*http.Response, error) {
			posts++
			if code == 0 {
				return nil, errors.New("synthetic-secret")
			}
			return reply(code, `{}`), nil
		})
		pre, _ := c.Inspect(context.Background(), b, "compute.server.power_on", "42")
		r, err := c.Submit(context.Background(), b, "compute.server.power_on", "42", pre.Fingerprint)
		if posts != 1 || r.Reference != "" || r.Outcome == "accepted" || err != nil && strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("unsafe retry/receipt", r, err, posts)
		}
		if (code == 401 || code == 403) && (r.Outcome != "rejected" || err != nil) {
			t.Fatal("denial not definitive")
		}
		if _, err = c.Inspect(context.Background(), provider.Binding{}, "compute.server.power_on", "42"); err != provider.ErrBinding {
			t.Fatal("binding fallback")
		}
		if _, err = c.Inspect(context.Background(), b, "compute.server.power_on", "../escape"); err == nil {
			t.Fatal("path injection")
		}
	}
}
