//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"sama/backend/internal/workspace"
)

func TestConnectionGrantsCurrentAuthorityAndAudit(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	admin, _ := seedIdentity(t, pool)
	viewer, other := seedIdentity(t, pool)
	ctx := context.Background()
	connection := entityID(t)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO memberships VALUES($1,$2,'admin',1),($1,$3,'viewer',1)`, scope, admin, viewer)
	exec(`INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','account','Fixture')`, connection, scope)
	check := func(actor uuid.UUID, action string, want error) {
		t.Helper()
		if err := workspace.ConnectionAuthority(ctx, pool, actor, scope, connection, action); err != want {
			t.Fatalf("%s: %v want %v", action, err, want)
		}
	}
	set := func(actor, target uuid.UUID, actions ...string) error {
		return workspace.SetGrant(ctx, pool, actor, scope, connection, target, actions, "grant-test")
	}
	check(owner, "operate", workspace.ErrNotFound)
	actions, err := workspace.ReadGrant(ctx, pool, owner, scope, connection)
	if err != nil || len(actions) != 0 {
		t.Fatal("default grant", actions, err)
	}
	if set(owner, owner, "read", "operate") != nil {
		t.Fatal("explicit owner grant")
	}
	check(owner, "read", nil)
	check(owner, "operate", workspace.ErrDenied) // Disabled connection cannot dispatch.
	exec(`INSERT INTO credential_versions(workspace_id,connection_id,version,ciphertext,nonce,wrapped_data_key,wrapping_nonce,encryption_format,master_key_id) VALUES($1,$2,1,'x','n','k','w',1,'fixture')`, scope, connection)
	exec(`UPDATE connections SET status='active',active_credential_version=1 WHERE id=$1`, connection)
	check(owner, "operate", nil)
	if set(admin, viewer, "read") != workspace.ErrDenied {
		t.Fatal("admin exceeded own grant")
	}
	if set(owner, admin, "read", "refresh") != nil || set(admin, viewer, "read") != nil {
		t.Fatal("bounded delegation")
	}
	if set(admin, admin, "read", "operate") != workspace.ErrDenied || set(owner, viewer, "read", "high_impact") != workspace.ErrDenied {
		t.Fatal("ceiling bypass")
	}
	if set(owner, owner, "read", "read") != workspace.ErrInvalid {
		t.Fatal("duplicate action")
	}
	if err := workspace.SetGrant(ctx, pool, owner, other, connection, viewer, []string{"read"}, "cross-tenant"); err != workspace.ErrNotFound {
		t.Fatal("cross tenant", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO connection_grants VALUES($1,$2,$3,'{}')`, other, connection, viewer); err == nil {
		t.Fatal("cross tenant FK")
	}
	foreignConnection := entityID(t)
	exec(`INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','foreign','Foreign')`, foreignConnection, other)
	if err := workspace.SetGrant(ctx, pool, owner, scope, foreignConnection, viewer, []string{"read"}, "foreign-grant"); err != workspace.ErrNotFound {
		t.Fatal("foreign connection accepted", err)
	}
	var denied int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE event_type='connection.grants_denied'`).Scan(&denied); err != nil || denied != 4 {
		t.Fatal("denial audit", denied, err)
	}
	exec(`ALTER TABLE audit_events ADD CONSTRAINT reject_grant CHECK(event_type<>'connection.grants_changed') NOT VALID`)
	if set(owner, owner) != workspace.ErrStore {
		t.Fatal("audit failure not propagated")
	}
	check(owner, "operate", nil)
	exec(`ALTER TABLE audit_events DROP CONSTRAINT reject_grant`)
	exec(`UPDATE memberships SET role='viewer' WHERE workspace_id=$1 AND user_id=$2`, scope, owner)
	check(owner, "operate", workspace.ErrDenied)
	exec(`DELETE FROM memberships WHERE workspace_id=$1 AND user_id=$2`, scope, viewer)
	check(viewer, "read", workspace.ErrNotFound)
	var grants int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM connection_grants WHERE user_id=$1`, viewer).Scan(&grants); err != nil || grants != 0 {
		t.Fatal("removed membership retained grants")
	}
	exec(`UPDATE users SET disabled_at=clock_timestamp() WHERE id=$1`, admin)
	check(admin, "read", workspace.ErrNotFound)
}
