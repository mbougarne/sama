package hetzner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"sama/backend/internal/provider"
)

func powerFixture(t *testing.T, status *string, post func(*http.Request) (*http.Response, error), actionJSON *string) (*Client, provider.Binding) {
	t.Helper()
	c, b := fixture(t, func(*http.Request) *http.Response { return reply(500, "") })
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.hetzner.cloud" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Fatal("request boundary")
		}
		if r.Method == "POST" {
			return post(r)
		}
		if strings.HasPrefix(r.URL.Path, "/v1/actions/") {
			return reply(200, *actionJSON), nil
		}
		if r.URL.Path != "/v1/servers/42" {
			t.Fatal("unexpected read", r.URL.Path)
		}
		return reply(200, fmt.Sprintf(`{"server":{"id":42,"name":"target","status":%q,"locked":false}}`, *status)), nil
	})
	return c, b
}

func nativeAction(command, status string) string {
	return fmt.Sprintf(`{"action":{"id":9,"command":%q,"status":%q,"resources":[{"id":42,"type":"server"}]}}`, command, status)
}

func TestPowerOnReviewSubmitObserve(t *testing.T) {
	status := "off"
	action := nativeAction("start_server", "running")
	posts := 0
	c, b := powerFixture(t, &status, func(r *http.Request) (*http.Response, error) {
		posts++
		if r.URL.Path != "/v1/servers/42/actions/poweron" {
			t.Fatal("wrong native action")
		}
		return reply(201, action), nil
	}, &action)
	ctx := context.Background()
	pre, err := c.Inspect(ctx, b, "compute.server.power_on", "42")
	if err != nil || !pre.Allowed || pre.HighImpact {
		t.Fatal(pre, err)
	}
	r, err := c.Submit(ctx, b, "compute.server.power_on", "42", pre.Fingerprint)
	if err != nil || r.Outcome != "accepted" || r.Reference != "9" || posts != 1 {
		t.Fatal(r, err, posts)
	}
	o, err := c.Observe(ctx, b, "compute.server.power_on", "42", &r.Reference)
	if err != nil || o.Outcome != "pending" {
		t.Fatal(o, err)
	}
	action = nativeAction("start_server", "success")
	o, err = c.Observe(ctx, b, "compute.server.power_on", "42", &r.Reference)
	if err != nil || o.Outcome != "pending" {
		t.Fatal("success without desired state", o, err)
	}
	status = "running"
	o, err = c.Observe(ctx, b, "compute.server.power_on", "42", &r.Reference)
	if err != nil || o.Outcome != "succeeded" || !o.NativeEvidence {
		t.Fatal(o, err)
	}
	if o, err = c.Observe(ctx, b, "compute.server.power_on", "42", nil); err != nil || o.Outcome != "unknown" {
		t.Fatal("missing receipt became success")
	}
	if r, err = c.Submit(ctx, b, "compute.server.power_on", "42", pre.Fingerprint); err != nil || r.Outcome != "rejected" || posts != 1 {
		t.Fatal("stale review submitted")
	}
}

func TestPowerOnAmbiguousResponsesSubmitOnlyOnce(t *testing.T) {
	for _, probe := range []struct {
		code int
		body string
		fail bool
	}{
		{403, `{"secret":"synthetic-secret"}`, false}, {429, `{}`, false}, {500, `{}`, false},
		{201, `{}`, false}, {201, `broken`, false}, {201, nativeAction("reboot_server", "running"), false},
		{201, strings.ReplaceAll(nativeAction("start_server", "running"), `"id":42`, `"id":43`), false},
		{0, "", true},
	} {
		t.Run(fmt.Sprint(probe.code, probe.body), func(t *testing.T) {
			status := "off"
			action := "{}"
			posts := 0
			c, b := powerFixture(t, &status, func(*http.Request) (*http.Response, error) {
				posts++
				if probe.fail {
					return nil, errors.New("synthetic-secret")
				}
				return reply(probe.code, probe.body), nil
			}, &action)
			pre, _ := c.Inspect(context.Background(), b, "compute.server.power_on", "42")
			r, err := c.Submit(context.Background(), b, "compute.server.power_on", "42", pre.Fingerprint)
			if posts != 1 || r.Reference != "" || err != nil && strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("unsafe submission", r, err, posts)
			}
			if probe.code == 403 && (r.Outcome != "rejected" || err != nil) || probe.code != 403 && r.Outcome == "accepted" {
				t.Fatal("unsafe receipt", r, err)
			}
		})
	}
}
