package hetzner

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"sama/backend/internal/provider"
)

func deleteFixture(t *testing.T, image *string, readCode *int, remove func(*http.Request) (*http.Response, error)) (*Client, provider.Binding) {
	c, b := fixture(t, func(*http.Request) *http.Response { return reply(500, "") })
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.hetzner.cloud" || r.URL.Path != "/v1/images/7" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Fatal("wrong delete target")
		}
		switch r.Method {
		case "GET":
			return reply(*readCode, `{"image":`+*image+`}`), nil
		case "DELETE":
			return remove(r)
		default:
			t.Fatal("unexpected method", r.Method)
			return nil, errors.New("unexpected")
		}
	})
	return c, b
}

func deleteImage(protected bool) string {
	value := "false"
	if protected {
		value = "true"
	}
	return strings.TrimSuffix(snapshotJSON, "}") + `,"protection":{"delete":` + value + `},"deleted":null}`
}

func TestSnapshotDeleteRecentAuthReviewAndNativeAbsence(t *testing.T) {
	image, code, deletes := deleteImage(false), 200, 0
	c, b := deleteFixture(t, &image, &code, func(*http.Request) (*http.Response, error) { deletes++; return reply(204, ""), nil })
	a, ctx := c.SnapshotDelete(), context.Background()
	pre, err := a.Inspect(ctx, b, provider.SnapshotDelete, "7")
	if err != nil || !pre.Allowed || !pre.HighImpact || !strings.Contains(a.Impact(), "irreversibly") || !strings.Contains(a.Impact(), "recent authentication") {
		t.Fatal(pre, err)
	}
	image = deleteImage(true)
	if r, e := a.Submit(ctx, b, provider.SnapshotDelete, "7", pre.Fingerprint); e != nil || r.Outcome != "rejected" || deletes != 0 {
		t.Fatal("protection bypass", r, e)
	}
	image = deleteImage(false)
	r, err := a.Submit(ctx, b, provider.SnapshotDelete, "7", pre.Fingerprint)
	if err != nil || r.Outcome != "accepted" || r.Reference != "delete/7" || deletes != 1 {
		t.Fatal(r, err, deletes)
	}
	o, err := a.Observe(ctx, b, provider.SnapshotDelete, "7", &r.Reference)
	if err != nil || o.Outcome != "pending" {
		t.Fatal("ack is not observed absence", o, err)
	}
	code = 403
	if _, err = a.Observe(ctx, b, provider.SnapshotDelete, "7", &r.Reference); err == nil {
		t.Fatal("permission denial treated as deletion")
	}
	code = 404
	o, err = a.Observe(ctx, b, provider.SnapshotDelete, "7", &r.Reference)
	if err != nil || o.Outcome != "succeeded" || !o.NativeEvidence {
		t.Fatal(o, err)
	}
	o, err = a.Observe(ctx, b, provider.SnapshotDelete, "7", nil)
	if err != nil || o.Outcome != "unknown" || deletes != 1 {
		t.Fatal("lost receipt must retain uncertainty", o, err, deletes)
	}
	wrong, _ := provider.NewBinding(b.Workspace(), b.Connection(), Family, b.Version()+1)
	if _, err = a.Observe(ctx, wrong, provider.SnapshotDelete, "7", &r.Reference); err != provider.ErrBinding {
		t.Fatal("binding escaped")
	}
	bad := "delete/8"
	if _, err = a.Observe(ctx, b, provider.SnapshotDelete, "7", &bad); err == nil {
		t.Fatal("cross-target evidence")
	}
}

func TestSnapshotDeleteRejectsUnsafeReviews(t *testing.T) {
	for _, image := range []string{
		snapshotJSON, deleteImage(true), strings.Replace(deleteImage(false), `"available"`, `"creating"`, 1),
		strings.Replace(deleteImage(false), `"delete":false`, `"delete":null`, 1),
		strings.Replace(deleteImage(false), `"snapshot"`, `"backup"`, 1),
		strings.Replace(deleteImage(false), `"deleted":null`, `"deleted":"2026-10-26T00:00:00Z"`, 1),
	} {
		code := 200
		deletes := 0
		c, b := deleteFixture(t, &image, &code, func(*http.Request) (*http.Response, error) { deletes++; return reply(204, ""), nil })
		a := c.SnapshotDelete()
		pre, err := a.Inspect(context.Background(), b, provider.SnapshotDelete, "7")
		if err == nil && pre.Allowed {
			t.Fatal("unsafe review allowed")
		}
		if _, err = a.Submit(context.Background(), b, provider.SnapshotDelete, "7", pre.Fingerprint); deletes != 0 {
			t.Fatal("unsafe deletion")
		}
	}
}

func TestSnapshotDeleteAmbiguityNeverRepeats(t *testing.T) {
	for _, probe := range []struct {
		code int
		fail bool
	}{{403, false}, {429, false}, {500, false}, {200, false}, {302, false}, {0, true}} {
		image, code, deletes := deleteImage(false), 200, 0
		c, b := deleteFixture(t, &image, &code, func(*http.Request) (*http.Response, error) {
			deletes++
			if probe.fail {
				return nil, errors.New("synthetic-secret")
			}
			return reply(probe.code, `{"secret":"synthetic-secret"}`), nil
		})
		a, ctx := c.SnapshotDelete(), context.Background()
		pre, _ := a.Inspect(ctx, b, provider.SnapshotDelete, "7")
		r, err := a.Submit(ctx, b, provider.SnapshotDelete, "7", pre.Fingerprint)
		if deletes != 1 || r.Reference != "" || err != nil && strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("unsafe receipt", r, err, deletes)
		}
		if probe.code == 403 {
			if r.Outcome != "rejected" {
				t.Fatal(r)
			}
		} else if r.Outcome != "ambiguous" {
			t.Fatal(r, err)
		}
		if o, e := a.Observe(ctx, b, provider.SnapshotDelete, "7", nil); e != nil || o.Outcome != "unknown" || deletes != 1 {
			t.Fatal(o, e, deletes)
		}
	}
}
