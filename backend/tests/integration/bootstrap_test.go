//go:build integration

package integration

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"sama/backend/internal/httpapi"
	"sama/backend/internal/workspace"
)

func TestOwnerBootstrapAtomicAndNeverReplacesOwner(t *testing.T) {
	pool := identityDatabase(t)
	ctx := context.Background()
	issuer := "https://issuer.example"
	if _, err := workspace.Bootstrap(ctx, pool, issuer, "https://another.example", "subject", "Initial workspace"); !errors.Is(err, workspace.ErrInvalid) {
		t.Fatal("unconfigured issuer accepted")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE audit_events ADD CONSTRAINT reject_bootstrap CHECK(event_type<>'workspace.created')`); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Bootstrap(ctx, pool, issuer, issuer, "subject", "Initial workspace"); err == nil {
		t.Fatal("audit failure accepted")
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users)+(SELECT count(*) FROM workspaces)+(SELECT count(*) FROM memberships)+(SELECT count(*) FROM installation_bootstrap)`).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("bootstrap was not atomic")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE audit_events DROP CONSTRAINT reject_bootstrap`); err != nil {
		t.Fatal(err)
	}
	id, err := workspace.Bootstrap(ctx, pool, issuer, issuer, "subject", "Initial workspace")
	if err != nil {
		t.Fatal(err)
	}
	for _, subject := range []string{"subject", "replacement"} {
		if _, err := workspace.Bootstrap(ctx, pool, issuer, issuer, subject, "Replacement"); !errors.Is(err, workspace.ErrBootstrapExists) {
			t.Fatal("bootstrap replaced owner")
		}
	}
	var role, subject string
	if err := pool.QueryRow(ctx, `SELECT m.role,u.subject FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.workspace_id=$1`, id).Scan(&role, &subject); err != nil || role != "owner" || subject != "subject" {
		t.Fatal("wrong owner")
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE workspace_id=$1 AND actor_kind='installation'`, id).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("missing bootstrap audit")
	}
	handler := httpapi.NewHandler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("POST", "/auth/bootstrap", nil))
	if recorder.Code != 404 {
		t.Fatal("anonymous bootstrap exposed")
	}
}
