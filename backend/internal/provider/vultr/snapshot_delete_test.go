package vultr

import (
	"context"
	"errors"
	"net/http"
	"sama/backend/internal/provider"
	"strings"
	"testing"
)

func snapshotDeleteFixture(t *testing.T, image *string, code *int, remove func(*http.Request) (*http.Response, error)) (*Client, provider.Binding) {
	c, b := fixture(t, func(*http.Request) *http.Response { return reply(500, "") })
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.vultr.com" || r.URL.Path != "/v2/snapshots/snap-7" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Fatal("wrong boundary")
		}
		if r.Method == "DELETE" {
			return remove(r)
		}
		if r.Method != "GET" {
			t.Fatal("unexpected method")
		}
		return reply(*code, `{"snapshot":`+*image+`}`), nil
	})
	return c, b
}
func TestSnapshotDeleteHighImpactNativeStateAndAbsence(t *testing.T) {
	image, code, deletes := snapshotJSON, 200, 0
	c, b := snapshotDeleteFixture(t, &image, &code, func(*http.Request) (*http.Response, error) { deletes++; return reply(204, ""), nil })
	a, ctx := c.SnapshotDelete(), context.Background()
	pre, err := a.Inspect(ctx, b, provider.SnapshotDelete, "snap-7")
	if err != nil || !pre.Allowed || !pre.HighImpact || !strings.Contains(a.Impact(), "recent authentication") {
		t.Fatal(pre, err)
	}
	image = strings.Replace(snapshotJSON, `"complete"`, `"pending"`, 1)
	if r, e := a.Submit(ctx, b, provider.SnapshotDelete, "snap-7", pre.Fingerprint); e != nil || r.Outcome != "rejected" || deletes != 0 {
		t.Fatal("pending snapshot deleted", r, e)
	}
	image = snapshotJSON
	r, err := a.Submit(ctx, b, provider.SnapshotDelete, "snap-7", pre.Fingerprint)
	if err != nil || r.Outcome != "accepted" || r.Reference != "delete/snap-7" || deletes != 1 {
		t.Fatal(r, err, deletes)
	}
	if o, e := a.Observe(ctx, b, provider.SnapshotDelete, "snap-7", &r.Reference); e != nil || o.Outcome != "pending" {
		t.Fatal(o, e)
	}
	image = strings.Replace(snapshotJSON, `"complete"`, `"deleted"`, 1)
	if o, e := a.Observe(ctx, b, provider.SnapshotDelete, "snap-7", &r.Reference); e != nil || o.Outcome != "succeeded" || !o.NativeEvidence {
		t.Fatal(o, e)
	}
	code = 403
	if _, err = a.Observe(ctx, b, provider.SnapshotDelete, "snap-7", &r.Reference); err == nil {
		t.Fatal("denial is not absence")
	}
	code = 404
	if o, e := a.Observe(ctx, b, provider.SnapshotDelete, "snap-7", &r.Reference); e != nil || o.Outcome != "succeeded" || !o.NativeEvidence {
		t.Fatal(o, e)
	}
	if o, e := a.Observe(ctx, b, provider.SnapshotDelete, "snap-7", nil); e != nil || o.Outcome != "unknown" || deletes != 1 {
		t.Fatal(o, e, deletes)
	}
	wrong, _ := provider.NewBinding(b.Workspace(), b.Connection(), Family, b.Version()+1)
	if _, err = a.Observe(ctx, wrong, provider.SnapshotDelete, "snap-7", &r.Reference); err != provider.ErrBinding {
		t.Fatal("binding escaped")
	}
}
func TestSnapshotDeleteUnknownAndAmbiguous(t *testing.T) {
	image, code := strings.Replace(snapshotJSON, `"complete"`, `"future"`, 1), 200
	c, b := snapshotDeleteFixture(t, &image, &code, func(*http.Request) (*http.Response, error) { t.Fatal("unexpected DELETE"); return nil, nil })
	if p, e := c.SnapshotDelete().Inspect(context.Background(), b, provider.SnapshotDelete, "snap-7"); e == nil && p.Allowed {
		t.Fatal("unknown state authorized")
	}
	for _, p := range []struct {
		code int
		fail bool
	}{{403, false}, {429, false}, {500, false}, {200, false}, {302, false}, {0, true}} {
		image, code, deletes := snapshotJSON, 200, 0
		c, b := snapshotDeleteFixture(t, &image, &code, func(*http.Request) (*http.Response, error) {
			deletes++
			if p.fail {
				return nil, errors.New("synthetic-secret")
			}
			return reply(p.code, `{}`), nil
		})
		a, ctx := c.SnapshotDelete(), context.Background()
		pre, _ := a.Inspect(ctx, b, provider.SnapshotDelete, "snap-7")
		r, err := a.Submit(ctx, b, provider.SnapshotDelete, "snap-7", pre.Fingerprint)
		if deletes != 1 || r.Reference != "" || err != nil && strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal(r, err, deletes)
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
