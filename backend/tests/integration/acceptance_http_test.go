//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"sama/backend/internal/operation"
)

func TestOperationAcceptanceHTTPRequiresConfirmationAndReturnsDurableReference(t *testing.T) {
	s, pool, lease, actor, target, _, _ := acceptanceSetup(t)
	c := prepareAcceptance(t, s, actor, lease.Workspace, target)
	request := operationHTTP(t, pool, actor, s)
	path := "/api/v1/workspaces/" + lease.Workspace.String() + "/operations"
	body := `{"review_id":"` + c.ReviewID.String() + `","confirmed":true}`
	if request(path, body, "", true).Code != 400 || request(path, body, "http-key", false).Code != 403 {
		t.Fatal("missing key/CSRF accepted")
	}
	for _, input := range []string{
		`{"review_id":"` + c.ReviewID.String() + `","confirmed":false}`,
		`{"review_id":"` + c.ReviewID.String() + `","confirmed":true,"parameters":{"edited":true}}`,
	} {
		if w := request(path, input, "invalid-key", true); w.Code < 400 {
			t.Fatal("editable/unconfirmed intent accepted")
		}
	}
	first, second := request(path, body, "http-key", true), request(path, body, "http-key", true)
	var accepted operation.Accepted
	if first.Code != 202 || second.Code != 202 || json.Unmarshal(first.Body.Bytes(), &accepted) != nil || accepted.ID.Version() != 7 || accepted.State != "queued" || first.Body.String() != second.Body.String() || !strings.HasSuffix(first.Header().Get("Location"), accepted.ID.String()) {
		t.Fatal("durable response", first.Code, first.Body.String(), second.Code)
	}
	var jobs int
	if pool.QueryRow(context.Background(), `SELECT count(*) FROM jobs WHERE operation_id=$1 AND type='operation_dispatch' AND state='queued'`, accepted.ID).Scan(&jobs) != nil || jobs != 1 {
		t.Fatal("202 preceded durable job")
	}
	if strings.Contains(first.Body.String(), "intent") || strings.Contains(first.Body.String(), "credential") {
		t.Fatal("internal intent leaked")
	}
}
