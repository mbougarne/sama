package hetzner

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"sama/backend/internal/provider"
)

func TestShutdownRebootAndForcedOffUseDistinctNativeActions(t *testing.T) {
	for _, probe := range []struct {
		action, path, command, final string
		high                         bool
	}{
		{"compute.server.shutdown", "shutdown", "shutdown_server", "off", false},
		{"compute.server.reboot", "reboot", "reboot_server", "running", false},
		{"compute.server.power_off", "poweroff", "stop_server", "off", true},
	} {
		t.Run(probe.action, func(t *testing.T) {
			status := "running"
			action := nativeAction(probe.command, "running")
			posts := 0
			c, b := powerFixture(t, &status, func(r *http.Request) (*http.Response, error) {
				posts++
				if r.URL.Path != "/v1/servers/42/actions/"+probe.path {
					t.Fatal("native action substituted")
				}
				return reply(201, action), nil
			}, &action)
			pre, err := c.Inspect(context.Background(), b, probe.action, "42")
			if err != nil || !pre.Allowed || pre.HighImpact != probe.high {
				t.Fatal(pre, err)
			}
			r, err := c.Submit(context.Background(), b, probe.action, "42", pre.Fingerprint)
			if err != nil || r.Reference != "9" || posts != 1 {
				t.Fatal(r, err)
			}
			if o, err := c.Observe(context.Background(), b, probe.action, "42", nil); err != nil || o.Outcome != "unknown" {
				t.Fatal("running state substituted for native proof")
			}
			action = nativeAction(probe.command, "error")
			if o, err := c.Observe(context.Background(), b, probe.action, "42", &r.Reference); err != nil || o.Outcome != "failed" || !o.NativeEvidence {
				t.Fatal(o, err)
			}
			action = nativeAction(probe.command, "success")
			status = probe.final
			if o, err := c.Observe(context.Background(), b, probe.action, "42", &r.Reference); err != nil || o.Outcome != "succeeded" || !o.NativeEvidence {
				t.Fatal(o, err)
			}
		})
	}
}

func TestCloudPowerRejectsLockedWrongBindingAndUnknownState(t *testing.T) {
	for _, body := range []string{
		`{"server":{"id":42,"name":"target","status":"off","locked":true}}`,
		`{"server":{"id":42,"name":"target","status":"unknown","locked":false}}`,
		`{"server":{"id":42,"name":"target","status":"off"}}`,
	} {
		c, b := fixture(t, func(*http.Request) *http.Response { return reply(200, body) })
		pre, err := c.Inspect(context.Background(), b, "compute.server.power_on", "42")
		if err == nil && pre.Allowed {
			t.Fatal("unsafe native state allowed")
		}
		if _, err = c.Inspect(context.Background(), provider.Binding{}, "compute.server.power_on", "42"); err != provider.ErrBinding {
			t.Fatal("binding not checked")
		}
		if _, err = c.Inspect(context.Background(), b, "compute.server.delete", "42"); err != provider.ErrActionUnsupported {
			t.Fatal("unknown action accepted")
		}
		if _, err = c.Inspect(context.Background(), b, "compute.server.power_on", "../42"); err == nil {
			t.Fatal("target path escape")
		}
	}
	status := "running"
	action := strings.ReplaceAll(nativeAction("reboot_server", "success"), `"id":42`, `"id":43`)
	c, b := powerFixture(t, &status, nil, &action)
	reference := "9"
	if _, err := c.Observe(context.Background(), b, "compute.server.reboot", "42", &reference); err == nil {
		t.Fatal("unrelated action proved reboot")
	}
}
