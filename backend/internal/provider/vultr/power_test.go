package vultr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"sama/backend/internal/provider"
)

func TestVultrPowerContracts(t *testing.T) {
	for _, probe := range []struct{ action, path, initial, final string }{
		{"compute.server.power_on", "start", "stopped", "running"},
		{"compute.server.reboot", "reboot", "running", "running"},
		{"compute.server.power_off", "halt", "running", "stopped"},
	} {
		t.Run(probe.action, func(t *testing.T) {
			status := probe.initial
			posts := 0
			c, b := fixture(t, func(*http.Request) *http.Response { return nil })
			c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "api.vultr.com" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
					t.Fatal("request boundary")
				}
				if r.Method == "POST" {
					posts++
					if r.URL.Path != "/v2/instances/native-42/"+probe.path {
						t.Fatal("native action substituted")
					}
					return reply(204, ""), nil
				}
				if r.URL.Path != "/v2/instances/native-42" {
					t.Fatal("read target changed")
				}
				return reply(200, fmt.Sprintf(`{"instance":{"id":"native-42","label":"target","status":"active","server_status":"ok","power_status":%q}}`, status)), nil
			})
			ctx := context.Background()
			pre, err := c.Inspect(ctx, b, probe.action, "native-42")
			if err != nil || !pre.Allowed || pre.HighImpact != (probe.path == "halt") {
				t.Fatal(pre, err)
			}
			r, err := c.Submit(ctx, b, probe.action, "native-42", pre.Fingerprint)
			if err != nil || r.Reference != "" || r.Outcome != "ambiguous" || posts != 1 {
				t.Fatal("invented native action receipt", r, err)
			}
			status = probe.final
			o, err := c.Observe(ctx, b, probe.action, "native-42", nil)
			if err != nil {
				t.Fatal(err)
			}
			if probe.path == "reboot" {
				if o.Outcome != "unknown" || o.NativeEvidence {
					t.Fatal("running state proved reboot")
				}
			} else if o.Outcome != "succeeded" || !o.NativeEvidence {
				t.Fatal(o)
			}
		})
	}
}

func TestVultrGracefulShutdownNeverUsesHalt(t *testing.T) {
	c, b := fixture(t, func(*http.Request) *http.Response { t.Fatal("unsupported action called provider"); return nil })
	if _, err := c.Inspect(context.Background(), b, "compute.server.shutdown", "native-42"); err != provider.ErrActionUnsupported {
		t.Fatal(err)
	}
	if _, err := c.Submit(context.Background(), b, "compute.server.shutdown", "native-42", [32]byte{}); err != provider.ErrActionUnsupported {
		t.Fatal(err)
	}
	if _, err := c.Observe(context.Background(), b, "compute.server.shutdown", "native-42", nil); err != provider.ErrActionUnsupported {
		t.Fatal(err)
	}
}

func TestVultrPowerFailuresNeverResubmit(t *testing.T) {
	for _, code := range []int{403, 408, 429, 500, 0} {
		posts := 0
		c, b := fixture(t, func(*http.Request) *http.Response { return nil })
		c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
			if r.Method == "POST" {
				posts++
				if code == 0 {
					return nil, errors.New("synthetic-secret")
				}
				return reply(code, `{}`), nil
			}
			return reply(200, `{"instance":{"id":"native-42","label":"target","status":"active","server_status":"ok","power_status":"stopped"}}`), nil
		})
		pre, _ := c.Inspect(context.Background(), b, "compute.server.power_on", "native-42")
		r, err := c.Submit(context.Background(), b, "compute.server.power_on", "native-42", pre.Fingerprint)
		if posts != 1 || r.Reference != "" || r.Outcome == "accepted" || err != nil && strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal(r, err, posts)
		}
		if _, err = c.Inspect(context.Background(), provider.Binding{}, "compute.server.power_on", "native-42"); err != provider.ErrBinding {
			t.Fatal("binding fallback")
		}
		if _, err = c.Inspect(context.Background(), b, "compute.server.power_on", "../escape"); err == nil {
			t.Fatal("path injection")
		}
	}
}
