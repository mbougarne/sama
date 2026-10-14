//go:build integration

package integration

import (
	"context"
	"testing"

	"sama/backend/internal/identity"
	"sama/backend/internal/operation"
)

func TestPolicySwitchInvalidatesReviewWithoutEnablingUnqualifiedAction(t *testing.T) {
	s, _, lease, actor, target, _, _ := acceptanceSetup(t)
	key := "fixture/compute:compute.server.power_on"
	s.WithPolicy(operation.NewPolicy([]string{key}, nil))
	if _, err := s.Prepare(context.Background(), identity.Principal{UserID: actor}, lease.Workspace, target, operation.Intent{Action: "compute.server.power_on", Parameters: &operation.PowerParameters{}}); err != operation.ErrUnsupported {
		t.Fatal("switch qualified missing adapter", err)
	}
	s.WithPolicy(operation.NewPolicy([]string{key}, map[string]bool{key: true}))
	c := prepareAcceptance(t, s, actor, lease.Workspace, target)
	s.WithPolicy(operation.NewPolicy(nil, map[string]bool{key: true}))
	if _, err := s.Confirm(context.Background(), identity.Principal{UserID: actor}, lease.Workspace, "policy-change", c, "policy-test"); err != operation.ErrStale {
		t.Fatal("configuration reused bound review", err)
	}
}
