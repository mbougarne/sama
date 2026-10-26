package vultr

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

func snapshotCreateFixture(t *testing.T, state, image *string, post func(*http.Request) (*http.Response, error)) (*Client, provider.Binding) {
	c, b := fixture(t, func(*http.Request) *http.Response { return reply(500, "") })
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.vultr.com" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Fatal("wrong boundary")
		}
		if r.Method == "POST" {
			body, _ := io.ReadAll(r.Body)
			if r.URL.Path != "/v2/snapshots" || string(body) != `{"instance_id":"source-1"}` {
				t.Fatal("wrong mutation", string(body))
			}
			return post(r)
		}
		switch r.URL.Path {
		case "/v2/instances/source-1":
			return reply(200, fmt.Sprintf(`{"instance":{"id":"source-1","label":"safe","status":"active","power_status":%q,"server_status":"ok"}}`, *state)), nil
		case "/v2/snapshots/snap-7":
			return reply(200, `{"snapshot":`+*image+`}`), nil
		default:
			t.Fatal("unrelated read", r.URL.Path)
			return nil, errors.New("unexpected")
		}
	})
	return c, b
}
func TestSnapshotCreateBindsResponseIDToSubmittedSource(t *testing.T) {
	state, image, posts := "stopped", strings.Replace(snapshotJSON, `"complete"`, `"pending"`, 1), 0
	c, b := snapshotCreateFixture(t, &state, &image, func(*http.Request) (*http.Response, error) { posts++; return reply(201, `{"snapshot":`+image+`}`), nil })
	a, ctx := c.SnapshotCreate(), context.Background()
	pre, err := a.Inspect(ctx, b, provider.SnapshotCreate, "source-1")
	if err != nil || !pre.Allowed || !pre.HighImpact || !strings.Contains(a.Impact(), "charges") {
		t.Fatal(pre, err)
	}
	r, err := a.Submit(ctx, b, provider.SnapshotCreate, "source-1", pre.Fingerprint)
	if err != nil || r.Outcome != "accepted" || r.Reference != "source-1/snap-7" || posts != 1 {
		t.Fatal(r, err, posts)
	}
	if o, e := a.Observe(ctx, b, provider.SnapshotCreate, "source-1", &r.Reference); e != nil || o.Outcome != "pending" {
		t.Fatal(o, e)
	}
	image = snapshotJSON
	if o, e := a.Observe(ctx, b, provider.SnapshotCreate, "source-1", &r.Reference); e != nil || o.Outcome != "succeeded" || !o.NativeEvidence {
		t.Fatal(o, e)
	}
	image = strings.Replace(snapshotJSON, `"complete"`, `"deleted"`, 1)
	if o, e := a.Observe(ctx, b, provider.SnapshotCreate, "source-1", &r.Reference); e != nil || o.Outcome != "unknown" {
		t.Fatal("unusable snapshot is not success", o, e)
	}
	if o, e := a.Observe(ctx, b, provider.SnapshotCreate, "source-1", nil); e != nil || o.Outcome != "unknown" || posts != 1 {
		t.Fatal(o, e, posts)
	}
	bad := "other-source/snap-7"
	if _, err = a.Observe(ctx, b, provider.SnapshotCreate, "source-1", &bad); err == nil {
		t.Fatal("cross-source receipt")
	}
	state = "running"
	if r, e := a.Submit(ctx, b, provider.SnapshotCreate, "source-1", pre.Fingerprint); e != nil || r.Outcome != "rejected" || posts != 1 {
		t.Fatal("stale creation", r, e)
	}
	wrong, _ := provider.NewBinding(b.Workspace(), b.Connection(), Family, b.Version()+1)
	if _, err = a.Inspect(ctx, wrong, provider.SnapshotCreate, "source-1"); err != provider.ErrBinding {
		t.Fatal("binding escaped")
	}
}
func TestSnapshotCreateRejectsUnknownAndNeverRepeats(t *testing.T) {
	state, image := "unknown", snapshotJSON
	c, b := snapshotCreateFixture(t, &state, &image, func(*http.Request) (*http.Response, error) { t.Fatal("unexpected POST"); return nil, nil })
	if p, e := c.SnapshotCreate().Inspect(context.Background(), b, provider.SnapshotCreate, "source-1"); e == nil && p.Allowed {
		t.Fatal("unknown source allowed")
	}
	for _, p := range []struct {
		code int
		body string
		fail bool
	}{
		{403, `{}`, false}, {429, `{}`, false}, {500, `{}`, false}, {201, `{}`, false}, {201, `broken`, false},
		{201, `{"snapshot":{"id":"unsafe/path"}}`, false}, {0, "", true},
	} {
		state, image, posts := "stopped", snapshotJSON, 0
		c, b := snapshotCreateFixture(t, &state, &image, func(*http.Request) (*http.Response, error) {
			posts++
			if p.fail {
				return nil, errors.New("synthetic-secret")
			}
			return reply(p.code, p.body), nil
		})
		a, ctx := c.SnapshotCreate(), context.Background()
		pre, _ := a.Inspect(ctx, b, provider.SnapshotCreate, "source-1")
		r, err := a.Submit(ctx, b, provider.SnapshotCreate, "source-1", pre.Fingerprint)
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
