package digitalocean

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sama/backend/internal/provider"
	"strings"
	"testing"
)

func snapshotNative(status, completed string) string {
	return fmt.Sprintf(`{"action":{"id":9,"type":"snapshot","status":%q,"resource_id":42,"resource_type":"droplet","completed_at":%s}}`, status, completed)
}
func snapshotCreateFixture(t *testing.T, state, native *string, post func(*http.Request) (*http.Response, error)) (*Client, provider.Binding) {
	c, b := fixture(t, func(*http.Request) *http.Response { return reply(500, "") })
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.digitalocean.com" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Fatal("wrong boundary")
		}
		if r.Method == "POST" {
			body, _ := io.ReadAll(r.Body)
			if r.URL.Path != "/v2/droplets/42/actions" || string(body) != `{"type":"snapshot","name":"sama-42"}` {
				t.Fatal("wrong body/path", string(body))
			}
			return post(r)
		}
		if r.URL.Path == "/v2/droplets/42/actions/9" {
			return reply(200, *native), nil
		}
		if r.URL.Path != "/v2/droplets/42" {
			t.Fatal("unrelated read", r.URL.Path)
		}
		return reply(200, fmt.Sprintf(`{"droplet":{"id":42,"name":"safe","status":%q,"locked":false}}`, *state)), nil
	})
	return c, b
}
func TestSnapshotCreateUsesNativeActionProof(t *testing.T) {
	state, native, posts := "off", snapshotNative("in-progress", "null"), 0
	c, b := snapshotCreateFixture(t, &state, &native, func(*http.Request) (*http.Response, error) { posts++; return reply(201, native), nil })
	a, ctx := c.SnapshotCreate(), context.Background()
	pre, err := a.Inspect(ctx, b, provider.SnapshotCreate, "42")
	if err != nil || !pre.Allowed || !pre.HighImpact || !strings.Contains(a.Impact(), "charges") {
		t.Fatal(pre, err)
	}
	r, err := a.Submit(ctx, b, provider.SnapshotCreate, "42", pre.Fingerprint)
	if err != nil || r.Outcome != "accepted" || r.Reference != "9" || posts != 1 {
		t.Fatal(r, err, posts)
	}
	if o, e := a.Observe(ctx, b, provider.SnapshotCreate, "42", &r.Reference); e != nil || o.Outcome != "pending" {
		t.Fatal(o, e)
	}
	native = snapshotNative("completed", "null")
	if o, e := a.Observe(ctx, b, provider.SnapshotCreate, "42", &r.Reference); e != nil || o.Outcome != "unknown" {
		t.Fatal("missing completion timestamp", o, e)
	}
	native = snapshotNative("completed", `"2026-10-26T10:00:00Z"`)
	if o, e := a.Observe(ctx, b, provider.SnapshotCreate, "42", &r.Reference); e != nil || o.Outcome != "succeeded" || !o.NativeEvidence {
		t.Fatal(o, e)
	}
	native = strings.Replace(native, `"resource_id":42`, `"resource_id":43`, 1)
	if _, err = a.Observe(ctx, b, provider.SnapshotCreate, "42", &r.Reference); err == nil {
		t.Fatal("wrong source accepted")
	}
	if o, e := a.Observe(ctx, b, provider.SnapshotCreate, "42", nil); e != nil || o.Outcome != "unknown" || posts != 1 {
		t.Fatal(o, e, posts)
	}
	state = "active"
	if r, e := a.Submit(ctx, b, provider.SnapshotCreate, "42", pre.Fingerprint); e != nil || r.Outcome != "rejected" || posts != 1 {
		t.Fatal("stale review", r, e)
	}
	wrong, _ := provider.NewBinding(b.Workspace(), b.Connection(), Family, b.Version()+1)
	if _, err = a.Inspect(ctx, wrong, provider.SnapshotCreate, "42"); err != provider.ErrBinding {
		t.Fatal("binding escaped")
	}
}
func TestSnapshotCreateRejectsLocksAndAmbiguousReceipts(t *testing.T) {
	for _, locked := range []string{"true", "null"} {
		c, b := fixture(t, func(*http.Request) *http.Response {
			return reply(200, `{"droplet":{"id":42,"status":"off","locked":`+locked+`}}`)
		})
		if pre, err := c.SnapshotCreate().Inspect(context.Background(), b, provider.SnapshotCreate, "42"); err == nil && pre.Allowed {
			t.Fatal("unsafe source allowed")
		}
	}
	for _, p := range []struct {
		code int
		body string
		fail bool
	}{
		{403, `{}`, false}, {429, `{}`, false}, {500, `{}`, false}, {201, `broken`, false},
		{201, strings.Replace(snapshotNative("in-progress", "null"), `"snapshot"`, `"reboot"`, 1), false}, {0, "", true},
	} {
		state, native, posts := "off", snapshotNative("in-progress", "null"), 0
		c, b := snapshotCreateFixture(t, &state, &native, func(*http.Request) (*http.Response, error) {
			posts++
			if p.fail {
				return nil, errors.New("synthetic-secret")
			}
			return reply(p.code, p.body), nil
		})
		a := c.SnapshotCreate()
		pre, _ := a.Inspect(context.Background(), b, provider.SnapshotCreate, "42")
		r, err := a.Submit(context.Background(), b, provider.SnapshotCreate, "42", pre.Fingerprint)
		if posts != 1 || r.Reference != "" || err != nil && strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal(r, err, posts)
		}
		if p.code == 403 {
			if r.Outcome != "rejected" {
				t.Fatal(r)
			}
		} else if r.Outcome != "ambiguous" {
			t.Fatal(r, err)
		}
	}
}
