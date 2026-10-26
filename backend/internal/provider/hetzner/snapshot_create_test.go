package hetzner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"sama/backend/internal/provider"
)

func createNative(status string) string {
	return fmt.Sprintf(`{"action":{"id":9,"command":"create_image","status":%q,"resources":[{"id":42,"type":"server"},{"id":7,"type":"image"}]},"image":{"id":7,"type":"snapshot"}}`, status)
}

func createFixture(t *testing.T, status *string, post func(*http.Request) (*http.Response, error), native *string, image *string) (*Client, provider.Binding) {
	c, b := fixture(t, func(*http.Request) *http.Response { return reply(500, "") })
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.hetzner.cloud" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Fatal("wrong boundary")
		}
		if r.Method == "POST" {
			body, _ := io.ReadAll(r.Body)
			if r.URL.Path != "/v1/servers/42/actions/create_image" || string(body) != `{"type":"snapshot"}` {
				t.Fatal("wrong mutation", r.URL.Path, string(body))
			}
			return post(r)
		}
		switch r.URL.Path {
		case "/v1/servers/42":
			return reply(200, fmt.Sprintf(`{"server":{"id":42,"name":"target","status":%q,"locked":false}}`, *status)), nil
		case "/v1/actions/9":
			return reply(200, *native), nil
		case "/v1/images/7":
			return reply(200, `{"image":`+*image+`}`), nil
		default:
			t.Fatal("unexpected call", r.Method, r.URL.Path)
			return nil, errors.New("unexpected")
		}
	})
	return c, b
}

func TestSnapshotCreateReviewedNativeCompletion(t *testing.T) {
	state, native, image := "off", createNative("running"), snapshotJSON
	posts := 0
	c, b := createFixture(t, &state, func(*http.Request) (*http.Response, error) { posts++; return reply(201, native), nil }, &native, &image)
	a, ctx := c.SnapshotCreate(), context.Background()
	pre, err := a.Inspect(ctx, b, provider.SnapshotCreate, "42")
	if err != nil || !pre.Allowed || !pre.HighImpact || !strings.Contains(a.Impact(), "charges") {
		t.Fatal(pre, err)
	}
	receipt, err := a.Submit(ctx, b, provider.SnapshotCreate, "42", pre.Fingerprint)
	if err != nil || receipt.Outcome != "accepted" || receipt.Reference != "9/7" || posts != 1 {
		t.Fatal(receipt, err, posts)
	}
	o, err := a.Observe(ctx, b, provider.SnapshotCreate, "42", &receipt.Reference)
	if err != nil || o.Outcome != "pending" {
		t.Fatal(o, err)
	}
	native = createNative("success")
	image = strings.Replace(snapshotJSON, `"available"`, `"creating"`, 1)
	o, err = a.Observe(ctx, b, provider.SnapshotCreate, "42", &receipt.Reference)
	if err != nil || o.Outcome != "pending" {
		t.Fatal("native success needs image completion", o, err)
	}
	image = snapshotJSON
	o, err = a.Observe(ctx, b, provider.SnapshotCreate, "42", &receipt.Reference)
	if err != nil || o.Outcome != "succeeded" || !o.NativeEvidence {
		t.Fatal(o, err)
	}
	o, err = a.Observe(ctx, b, provider.SnapshotCreate, "42", nil)
	if err != nil || o.Outcome != "unknown" || posts != 1 {
		t.Fatal("missing receipt must not repeat", o, err, posts)
	}
	state = "running"
	if r, e := a.Submit(ctx, b, provider.SnapshotCreate, "42", pre.Fingerprint); e != nil || r.Outcome != "rejected" || posts != 1 {
		t.Fatal("stale review submitted", r, e)
	}
	image = strings.Replace(snapshotJSON, `"id":42`, `"id":43`, 1)
	if _, err = a.Observe(ctx, b, provider.SnapshotCreate, "42", &receipt.Reference); err == nil {
		t.Fatal("wrong source proved success")
	}
	native = createNative("future")
	o, err = a.Observe(ctx, b, provider.SnapshotCreate, "42", &receipt.Reference)
	if err != nil || o.Outcome != "unknown" {
		t.Fatal(o, err)
	}
	wrong, _ := provider.NewBinding(b.Workspace(), b.Connection(), Family, b.Version()+1)
	if _, err = a.Inspect(ctx, wrong, provider.SnapshotCreate, "42"); err != provider.ErrBinding {
		t.Fatal("cross-version review")
	}
}

func TestSnapshotCreateAmbiguityNeverRetries(t *testing.T) {
	for _, probe := range []struct {
		code int
		body string
		fail bool
	}{
		{403, `{"secret":"synthetic-secret"}`, false}, {429, `{}`, false}, {500, `{}`, false},
		{201, `broken`, false}, {201, strings.Replace(createNative("running"), `"id":7`, `"id":8`, 1), false},
		{201, strings.Replace(createNative("running"), `"create_image"`, `"reboot_server"`, 1), false}, {0, "", true},
	} {
		posts := 0
		state, native, image := "off", createNative("running"), snapshotJSON
		c, b := createFixture(t, &state, func(*http.Request) (*http.Response, error) {
			posts++
			if probe.fail {
				return nil, errors.New("synthetic-secret")
			}
			return reply(probe.code, probe.body), nil
		}, &native, &image)
		a, ctx := c.SnapshotCreate(), context.Background()
		pre, _ := a.Inspect(ctx, b, provider.SnapshotCreate, "42")
		r, err := a.Submit(ctx, b, provider.SnapshotCreate, "42", pre.Fingerprint)
		if posts != 1 || r.Reference != "" || err != nil && strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("unsafe mutation", r, err, posts)
		}
		if probe.code == 403 {
			if r.Outcome != "rejected" {
				t.Fatal(r)
			}
		} else if r.Outcome != "ambiguous" {
			t.Fatal(r, err)
		}
		if o, e := a.Observe(ctx, b, provider.SnapshotCreate, "42", nil); e != nil || o.Outcome != "unknown" || posts != 1 {
			t.Fatal(o, e, posts)
		}
	}
}

func TestSnapshotCreateRestrictionsAndEvidenceBinding(t *testing.T) {
	for _, body := range []string{
		`{"server":{"id":42,"name":"target","status":"off","locked":true}}`,
		`{"server":{"id":42,"name":"target","status":"off"}}`,
		`{"server":{"id":42,"name":"target","status":"future","locked":false}}`,
	} {
		c, b := fixture(t, func(*http.Request) *http.Response { return reply(200, body) })
		a := c.SnapshotCreate()
		pre, err := a.Inspect(context.Background(), b, provider.SnapshotCreate, "42")
		if err == nil && pre.Allowed {
			t.Fatal("restricted source allowed", pre)
		}
	}
	state, native, image := "off", createNative("success"), snapshotJSON
	c, b := createFixture(t, &state, func(*http.Request) (*http.Response, error) { t.Fatal("unexpected POST"); return nil, nil }, &native, &image)
	a, ctx := c.SnapshotCreate(), context.Background()
	reference := "9/7"
	native = strings.Replace(native, `"id":9`, `"id":10`, 1)
	if _, err := a.Observe(ctx, b, provider.SnapshotCreate, "42", &reference); err == nil {
		t.Fatal("wrong native action accepted")
	}
	native = createNative("error")
	if o, err := a.Observe(ctx, b, provider.SnapshotCreate, "42", &reference); err != nil || o.Outcome != "failed" || !o.NativeEvidence {
		t.Fatal(o, err)
	}
	if _, err := a.Inspect(ctx, b, provider.SnapshotDelete, "42"); err != provider.ErrActionUnsupported {
		t.Fatal("action crossed contract")
	}
}
