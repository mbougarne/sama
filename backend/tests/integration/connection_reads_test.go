//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"sama/backend/internal/connection"
	"sama/backend/internal/workspace"
)

func TestAuthorizedConnectionPagesAndCapabilityReasons(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	other, foreign := seedIdentity(t, pool)
	ctx := context.Background()
	ids := []uuid.UUID{entityID(t), entityID(t), entityID(t)}
	for _, id := range ids {
		if _, err := pool.Exec(ctx, `INSERT INTO connections(id,workspace_id,provider,api_family,account_identity,label) VALUES($1,$2,'fixture','compute','safe-account','Fixture')`, id, scope); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range ids[:2] {
		if err := workspace.SetGrant(ctx, pool, owner, scope, id, owner, []string{"read"}, "read-grant"); err != nil {
			t.Fatal(err)
		}
	}
	supported := map[string][]string{"fixture/compute": {"compute.server.read", "compute.server.reboot"}}
	page, err := connection.ReadAuthorized(ctx, pool, owner, scope, uuid.Nil, uuid.Nil, 1, supported)
	if err != nil || len(page.Data) != 1 || page.NextCursor == nil || page.Data[0].Capabilities[0].Reason != "unverified" {
		t.Fatal(page, err)
	}
	next, err := connection.ReadAuthorized(ctx, pool, owner, scope, *page.NextCursor, uuid.Nil, 1, supported)
	if err != nil || len(next.Data) != 1 || next.NextCursor != nil || next.Data[0].ID == page.Data[0].ID {
		t.Fatal("pagination", next, err)
	}
	if _, err := connection.ReadAuthorized(ctx, pool, owner, scope, uuid.Nil, ids[2], 1, supported); err != workspace.ErrNotFound {
		t.Fatal("ungranted owner visibility")
	}
	if _, err := connection.ReadAuthorized(ctx, pool, other, foreign, uuid.Nil, ids[0], 1, supported); err != workspace.ErrNotFound {
		t.Fatal("cross tenant")
	}
	if _, err := pool.Exec(ctx, `UPDATE connections SET permissions_verified_at=clock_timestamp(),verified_capabilities=ARRAY['compute.server.read','compute.server.reboot'] WHERE id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	view, err := connection.ReadAuthorized(ctx, pool, owner, scope, uuid.Nil, ids[0], 1, supported)
	if err != nil || !view.Data[0].Capabilities[0].Available || view.Data[0].Capabilities[4].Reason != "denied" {
		t.Fatal("effective grant", view, err)
	}
	if err := workspace.SetGrant(ctx, pool, owner, scope, ids[0], owner, []string{"read", "operate"}, "read-grant"); err != nil {
		t.Fatal(err)
	}
	view, err = connection.ReadAuthorized(ctx, pool, owner, scope, uuid.Nil, ids[0], 1, supported)
	if err != nil || view.Data[0].Capabilities[4].Reason != "state_restricted" || view.Data[0].Capabilities[1].Reason != "unsupported" {
		t.Fatal("reason codes", view, err)
	}
	data, _ := json.Marshal(view)
	for _, secret := range []string{"ciphertext", "nonce", "wrapped_data_key", "master_key_id", "token"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("secret DTO", secret)
		}
	}
	if err := workspace.SetGrant(ctx, pool, owner, scope, ids[0], owner, nil, "revoke-grant"); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.ReadAuthorized(ctx, pool, owner, scope, uuid.Nil, ids[0], 1, supported); err != workspace.ErrNotFound {
		t.Fatal("revocation delayed")
	}
}
