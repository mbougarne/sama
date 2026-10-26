package contabo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"sama/backend/internal/provider"
)

func snapshotMutationFixture(t *testing.T, status, snapshotBody *string, mutation func(*http.Request) (*http.Response, error)) (*Client, provider.Binding) {
	t.Helper()
	c, b := fixture(t, func(*http.Request) *http.Response { return nil }, func(*http.Request) *http.Response { return tokenReply() })
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		request, err := uuid.Parse(r.Header.Get("x-request-id"))
		if err != nil || request.Version() != 4 || r.URL.Host != "api.contabo.com" || r.Header.Get("Authorization") != "Bearer synthetic-access" {
			t.Fatal("request boundary")
		}
		if r.Method != "GET" {
			return mutation(r)
		}
		switch r.URL.Path {
		case "/v1/compute/instances/42":
			return reply(200, fmt.Sprintf(`{"data":[{"instanceId":42,"name":"target","status":%q,"tenantId":"DE","customerId":"54321"}]}`, *status)), nil
		case "/v1/compute/instances/42/snapshots/snap42":
			return reply(200, *snapshotBody), nil
		}
		t.Fatal("wrong read target", r.URL)
		return nil, nil
	})
	return c, b
}
func TestSnapshotCreateReceiptAndUnknownCompletion(t *testing.T) {
	status := "running"
	detail := `{"data":[` + snapshotItem + `]}`
	posts := 0
	c, b := snapshotMutationFixture(t, &status, &detail, func(r *http.Request) (*http.Response, error) {
		posts++
		if r.Method != "POST" || r.URL.Path != "/v1/compute/instances/42/snapshots" {
			t.Fatal("wrong mutation")
		}
		var body map[string]string
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 1 || !strings.HasPrefix(body["name"], "sama-") || len(body["name"]) > 30 {
			t.Fatal("unsafe body", body)
		}
		return reply(201, `{"data":[`+strings.ReplaceAll(snapshotItem, "safe snapshot", body["name"])+`]}`), nil
	})
	a := c.SnapshotCreate()
	ctx := context.Background()
	pre, err := a.Inspect(ctx, b, provider.SnapshotCreate, "42")
	if err != nil || !pre.Allowed || !pre.HighImpact || !strings.Contains(a.Impact(), "charges") {
		t.Fatal(pre, err)
	}
	receipt, err := a.Submit(ctx, b, provider.SnapshotCreate, "42", pre.Fingerprint)
	if err != nil || receipt.Outcome != "accepted" || receipt.Reference != "42/snap42" || posts != 1 {
		t.Fatal(receipt, err)
	}
	for _, reference := range []*string{nil, &receipt.Reference} {
		observed, err := a.Observe(ctx, b, provider.SnapshotCreate, "42", reference)
		if err != nil || observed.Outcome != "unknown" || observed.NativeEvidence {
			t.Fatal("presence proved readiness", observed, err)
		}
	}
	status = "stopped"
	receipt, err = a.Submit(ctx, b, provider.SnapshotCreate, "42", pre.Fingerprint)
	if err != nil || receipt.Outcome != "rejected" || posts != 1 {
		t.Fatal("stale review submitted", receipt, err)
	}
	foreign := "43/snap42"
	if _, err = a.Observe(ctx, b, provider.SnapshotCreate, "42", &foreign); err == nil {
		t.Fatal("foreign receipt")
	}
	wrong, _ := provider.NewBinding(b.Workspace(), b.Connection(), Family, 2)
	if _, err = a.Inspect(ctx, wrong, provider.SnapshotCreate, "42"); err != provider.ErrBinding {
		t.Fatal(err)
	}
	if _, err = a.Inspect(ctx, b, provider.SnapshotDelete, "42"); err != provider.ErrActionUnsupported {
		t.Fatal(err)
	}
}
func TestSnapshotCreateNeverReplaysUncertainSubmission(t *testing.T) {
	for _, code := range []int{201, 400, 401, 403, 409, 429, 500, 0} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			status := "running"
			detail := `{"data":[` + snapshotItem + `]}`
			posts := 0
			c, b := snapshotMutationFixture(t, &status, &detail, func(r *http.Request) (*http.Response, error) {
				posts++
				_, _ = io.ReadAll(r.Body)
				if code == 0 {
					return nil, errors.New("synthetic transport loss")
				}
				return reply(code, detail), nil // 201 has a mismatching review-bound name.
			})
			a := c.SnapshotCreate()
			ctx := context.Background()
			pre, err := a.Inspect(ctx, b, provider.SnapshotCreate, "42")
			if err != nil {
				t.Fatal(err)
			}
			receipt, _ := a.Submit(ctx, b, provider.SnapshotCreate, "42", pre.Fingerprint)
			want := "ambiguous"
			if code == 400 || code == 401 || code == 403 || code == 409 {
				want = "rejected"
			}
			if posts != 1 || receipt.Outcome != want || receipt.Reference != "" {
				t.Fatal(receipt, posts)
			}
		})
	}
}
