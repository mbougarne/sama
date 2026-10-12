//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
	"sama/backend/internal/identity"
	"sama/backend/internal/operation"
	"sama/backend/internal/provider"
)

type powerReader struct {
	inspect func(context.Context, provider.Binding, string, string) (provider.Preconditions, error)
}

func (r powerReader) Inspect(ctx context.Context, b provider.Binding, a, target string) (provider.Preconditions, error) {
	return r.inspect(ctx, b, a, target)
}

func reviewService(t *testing.T, high bool) (*operation.Service, uuid.UUID, uuid.UUID, uuid.UUID, func()) {
	t.Helper()
	pool, lease, actor, target := operationFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE connections SET verified_capabilities=ARRAY['compute.server.power_on'],permissions_verified_at=clock_timestamp() WHERE id=$1`, lease.Connection); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE connection_grants SET actions=ARRAY['read','operate','high_impact'] WHERE user_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	sessions, _ := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	service := operation.NewService(pool, sessions, true, map[string]operation.ReviewFactory{"fixture/compute": func(_ context.Context, b provider.Binding) (provider.PowerReviewer, error) {
		if b.Workspace() != lease.Workspace || b.Connection() != lease.Connection || b.Version() != 1 {
			t.Fatal("binding escaped")
		}
		return powerReader{func(_ context.Context, _ provider.Binding, action, native string) (provider.Preconditions, error) {
			if action != "compute.server.power_on" || native != "operation-target" {
				t.Fatal("wrong precondition target")
			}
			return provider.Preconditions{Fingerprint: sha256.Sum256([]byte("stopped")), TargetName: "Safe", Allowed: true, HighImpact: high}, nil
		}}, nil
	}})
	revoke := func() {
		if _, err := pool.Exec(ctx, `UPDATE connection_grants SET actions=ARRAY['read'] WHERE user_id=$1`, actor); err != nil {
			t.Fatal(err)
		}
	}
	return service, actor, lease.Workspace, target, revoke
}

func TestFreshReviewsBindVersionsAndRequireCurrentAuthority(t *testing.T) {
	service, actor, scope, target, revoke := reviewService(t, false)
	intent := operation.Intent{Action: "compute.server.power_on", Parameters: &operation.PowerParameters{}}
	review, err := service.Prepare(context.Background(), identity.Principal{UserID: actor}, scope, target, intent)
	if err != nil || review.ID.Version() != 7 || review.ResourceID != target || review.ExpiresAt.Before(time.Now()) || review.TargetName != "Safe" {
		t.Fatal("review", err)
	}
	revoke()
	if _, err = service.Prepare(context.Background(), identity.Principal{UserID: actor}, scope, target, intent); err != operation.ErrDenied {
		t.Fatal("revoked operator prepared review", err)
	}
}

func TestHighImpactReviewRequiresVerifiedRecentAuthentication(t *testing.T) {
	service, actor, scope, target, _ := reviewService(t, true)
	intent := operation.Intent{Action: "compute.server.power_on", Parameters: &operation.PowerParameters{}}
	p := identity.Principal{UserID: actor, AuthenticatedAt: time.Now()}
	if _, err := service.Prepare(context.Background(), p, scope, target, intent); err != identity.ErrRecentRequired {
		t.Fatal("fresh token iat accepted", err)
	}
	p.Reauthenticated = true
	p.AuthenticatedAt = time.Now().Add(-time.Minute)
	review, err := service.Prepare(context.Background(), p, scope, target, intent)
	if err != nil || !review.HighImpact {
		t.Fatal("verified authentication rejected", err)
	}
}
