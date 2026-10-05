//go:build integration

package integration

import (
	"context"
	"sama/backend/internal/workspace"
	"testing"
	"time"
)

func TestInvitationRedemptionExpiryAuthorityAndRollback(t *testing.T) {
	pool := identityDatabase(t)
	owner, scope := seedIdentity(t, pool)
	ctx := context.Background()
	now := time.Now()
	proof, err := workspace.IssueInvitation(ctx, pool, owner, scope, "https://issuer.example", "new-subject", "viewer", "invite", now)
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := workspace.InvitationDigest(proof)
	redeem := func(issuer, subject string, at time.Time, commit bool) error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		err = workspace.RedeemInvitation(ctx, tx, digest, issuer, subject, "redeem", at)
		if err == nil && commit {
			err = tx.Commit(ctx)
		}
		return err
	}
	for _, test := range []struct {
		issuer, subject string
		at              time.Time
	}{{"https://other.example", "new-subject", now}, {"https://issuer.example", "different", now}, {"https://issuer.example", "new-subject", now.Add(24 * time.Hour)}} {
		if redeem(test.issuer, test.subject, test.at, true) == nil {
			t.Fatal("mismatched/expired proof admitted")
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE memberships SET version=2 WHERE workspace_id=$1`, scope); err != nil {
		t.Fatal(err)
	}
	if redeem("https://issuer.example", "new-subject", now, true) == nil {
		t.Fatal("changed inviter authority accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE memberships SET version=1 WHERE workspace_id=$1`, scope); err != nil {
		t.Fatal(err)
	}
	if err := redeem("https://issuer.example", "new-subject", now, false); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE subject='new-subject'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial admission survived rollback")
	}
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- redeem("https://issuer.example", "new-subject", now, true) }()
	}
	a, b := <-results, <-results
	if (a == nil) == (b == nil) {
		t.Fatalf("one-use failure %v %v", a, b)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.workspace_id=$1 AND u.subject='new-subject'`, scope).Scan(&count); err != nil || count != 1 {
		t.Fatal("admission not persisted")
	}
}
