package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"sama/backend/internal/platform/outbound"
)

type mutationTransport func(*http.Request) (*http.Response, error)

func (f mutationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMutationNeverRetriesOrLeaksResponses(t *testing.T) {
	w, _ := uuid.NewV7()
	c, _ := uuid.NewV7()
	b, _ := NewBinding(w, c, "hetzner.cloud", 1)
	for _, code := range []int{200, 400, 401, 403, 404, 408, 409, 422, 429, 500, 503} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: mutationTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "POST" || r.URL.Host != "api.hetzner.cloud" || r.Header.Get("Authorization") != "Bearer synthetic-only" {
					t.Fatal("request boundary")
				}
				return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"message":"synthetic-secret"}`))}, nil
			})}
			r, err := PostMutation(context.Background(), b, b, outbound.HetznerCloud, client, "https://api.hetzner.cloud/v1/servers/42/actions/poweron", "synthetic-only", nil, nil, nil)
			definite := code == 400 || code == 401 || code == 403 || code == 404 || code == 409 || code == 422
			if calls != 1 || (r.Outcome == "rejected") != definite || definite && err != nil || err != nil && strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("mutation classification", r, err, calls)
			}
			if code == 429 && !outbound.IsFailure(err, "provider_rate_limited") {
				t.Fatal("cooldown signal lost")
			}
		})
	}
	client := &http.Client{Transport: mutationTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic-secret") })}
	if r, err := PostMutation(context.Background(), b, b, outbound.HetznerCloud, client, "https://api.hetzner.cloud/v1/servers/42/actions/poweron", "synthetic-only", nil, nil, nil); r.Outcome != "ambiguous" || err == nil || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("unsafe transport error", r, err)
	}
}

func TestPowerFingerprintBindsReviewFacts(t *testing.T) {
	w, _ := uuid.NewV7()
	c, _ := uuid.NewV7()
	b, _ := NewBinding(w, c, "hetzner.cloud", 1)
	o := Observation{NativeID: "42", Name: "target", Status: "stopped", NativeStatus: "off", ObservedAt: time.Now()}
	p, _ := PowerPreconditions(b, "compute.server.power_on", o)
	o.ObservedAt = o.ObservedAt.Add(time.Minute)
	stable, _ := PowerPreconditions(b, "compute.server.power_on", o)
	if !p.Allowed || stable.Fingerprint != p.Fingerprint {
		t.Fatal("clock changed review facts")
	}
	o.Name = "renamed"
	changed, _ := PowerPreconditions(b, "compute.server.power_on", o)
	other, _ := NewBinding(w, c, b.Family(), 2)
	rotated, _ := PowerPreconditions(other, "compute.server.power_on", o)
	if changed.Fingerprint == p.Fingerprint || rotated.Fingerprint == changed.Fingerprint {
		t.Fatal("unbound review")
	}
	for _, status := range []string{"unknown", "pending", "failed", "running"} {
		o.Status = status
		pre, _ := PowerPreconditions(b, "compute.server.power_on", o)
		if pre.Allowed {
			t.Fatal("power on accepted invalid state", status)
		}
	}
}
