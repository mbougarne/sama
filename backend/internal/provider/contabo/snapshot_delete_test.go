package contabo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"sama/backend/internal/provider"
)

func TestSnapshotDeleteBoundReviewReceiptAndNativeAbsence(t *testing.T) {
	status := "running"
	detail := `{"data":[` + snapshotItem + `]}`
	deletes := 0
	c, b := snapshotMutationFixture(t, &status, &detail, func(r *http.Request) (*http.Response, error) {
		deletes++
		if r.Method != "DELETE" || r.URL.Path != "/v1/compute/instances/42/snapshots/snap42" {
			t.Fatal("wrong deletion")
		}
		return reply(204, ""), nil
	})
	a := c.SnapshotDelete()
	ctx := context.Background()
	pre, err := a.Inspect(ctx, b, provider.SnapshotDelete, "42/snap42")
	if err != nil || !pre.Allowed || !pre.HighImpact || pre.TargetName != "safe snapshot" || !strings.Contains(a.Impact(), "recent authentication") || !strings.Contains(a.Impact(), "typed-target") {
		t.Fatal(pre, err)
	}
	detail = strings.ReplaceAll(detail, "safe snapshot", "changed name")
	r, err := a.Submit(ctx, b, provider.SnapshotDelete, "42/snap42", pre.Fingerprint)
	if err != nil || r.Outcome != "rejected" || deletes != 0 {
		t.Fatal("stale review deleted", r, err)
	}
	detail = strings.ReplaceAll(detail, "changed name", "safe snapshot")
	r, err = a.Submit(ctx, b, provider.SnapshotDelete, "42/snap42", pre.Fingerprint)
	if err != nil || r.Outcome != "accepted" || r.Reference != "delete/42/snap42" || deletes != 1 {
		t.Fatal(r, err)
	}
	observed, err := a.Observe(ctx, b, provider.SnapshotDelete, "42/snap42", &r.Reference)
	if err != nil || observed.Outcome != "pending" || observed.NativeEvidence {
		t.Fatal(observed, err)
	}
	original := c.http.Transport
	readCode := 404
	c.http.Transport = transport(func(request *http.Request) (*http.Response, error) {
		if request.Method == "GET" && strings.HasSuffix(request.URL.Path, "/snap42") {
			return reply(readCode, `{}`), nil
		}
		return original.RoundTrip(request)
	})
	observed, err = a.Observe(ctx, b, provider.SnapshotDelete, "42/snap42", nil)
	if err != nil || observed.Outcome != "unknown" || observed.NativeEvidence {
		t.Fatal("unbound absence", observed, err)
	}
	observed, err = a.Observe(ctx, b, provider.SnapshotDelete, "42/snap42", &r.Reference)
	if err != nil || observed.Outcome != "succeeded" || !observed.NativeEvidence {
		t.Fatal(observed, err)
	}
	readCode = 403
	if _, err = a.Observe(ctx, b, provider.SnapshotDelete, "42/snap42", &r.Reference); err == nil {
		t.Fatal("denial proved deletion")
	}
	foreign := "delete/43/snap42"
	if _, err = a.Observe(ctx, b, provider.SnapshotDelete, "42/snap42", &foreign); err == nil {
		t.Fatal("foreign receipt")
	}
	wrong, _ := provider.NewBinding(b.Workspace(), b.Connection(), Family, 2)
	if _, err = a.Inspect(ctx, wrong, provider.SnapshotDelete, "42/snap42"); err != provider.ErrBinding {
		t.Fatal(err)
	}
	if _, err = a.Inspect(ctx, b, provider.SnapshotCreate, "42/snap42"); err != provider.ErrActionUnsupported {
		t.Fatal(err)
	}
}
func TestSnapshotDeleteFailuresAreNotReplayed(t *testing.T) {
	for _, code := range []int{200, 202, 204, 400, 401, 403, 404, 409, 422, 429, 500, 0} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			status := "running"
			detail := `{"data":[` + snapshotItem + `]}`
			deletes := 0
			c, b := snapshotMutationFixture(t, &status, &detail, func(r *http.Request) (*http.Response, error) {
				deletes++
				if code == 0 {
					return nil, errors.New("synthetic connection reset")
				}
				return reply(code, ""), nil
			})
			a := c.SnapshotDelete()
			ctx := context.Background()
			pre, err := a.Inspect(ctx, b, provider.SnapshotDelete, "42/snap42")
			if err != nil {
				t.Fatal(err)
			}
			receipt, _ := a.Submit(ctx, b, provider.SnapshotDelete, "42/snap42", pre.Fingerprint)
			want := "ambiguous"
			switch code {
			case 204:
				want = "accepted"
			case 400, 401, 403, 404, 409, 422:
				want = "rejected"
			}
			if receipt.Outcome != want || deletes != 1 || want != "accepted" && receipt.Reference != "" {
				t.Fatal(receipt, deletes)
			}
		})
	}
}
