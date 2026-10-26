package operation

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"sama/backend/internal/provider"
)

func TestSnapshotContractsRemainDormant(t *testing.T) {
	for _, action := range []string{provider.SnapshotCreate, provider.SnapshotDelete} {
		if _, err := DecodeIntent([]byte(`{"action":"` + action + `","parameters":{}}`)); err != nil {
			t.Fatal("canonical snapshot syntax rejected", action)
		}
		policy := NewPolicy([]string{"hetzner/hetzner.cloud:" + action}, map[string]bool{})
		if policy.permits("hetzner/hetzner.cloud", action) {
			t.Fatal("switch bypassed closed registry")
		}
	}
}

func TestSnapshotCompositeReceiptsRemainActionBound(t *testing.T) {
	for _, reference := range []string{"42/image-1", "delete/42/image-1"} {
		receipt := provider.Receipt{Outcome: "accepted", Reference: reference}
		state, ref := classifyActionReceipt(provider.SnapshotCreate, receipt, nil)
		if state != "waiting_provider" || ref == nil || *ref != reference {
			t.Fatal("snapshot receipt lost", state)
		}
		if state, ref = classifyActionReceipt("compute.server.reboot", receipt, nil); state != "reconciling" || ref != nil {
			t.Fatal("power receipt widened")
		}
	}
	for _, reference := range []string{"https://example.invalid", "delete/../secret", "a/b/c/d", "a//b"} {
		if state, _ := classifyActionReceipt(provider.SnapshotDelete, provider.Receipt{Outcome: "accepted", Reference: reference}, nil); state != "reconciling" {
			t.Fatal("unsafe reference", reference)
		}
	}
}

func TestSnapshotFactoriesNeverFallBackToFamilyPowerAdapter(t *testing.T) {
	binding, _ := provider.NewBinding(uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), "compute", 1)
	service := NewService(nil, nil, true, map[string]ReviewFactory{"fixture/compute": func(context.Context, provider.Binding) (provider.PowerReviewer, error) {
		t.Fatal("power fallback invoked")
		return nil, nil
	}})
	for _, action := range []string{provider.SnapshotCreate, provider.SnapshotDelete} {
		if _, err := service.inspect(context.Background(), authority{binding: binding, provider: "fixture"}, Intent{Action: action, Parameters: &PowerParameters{}}); err != ErrUnsupported {
			t.Fatal("unqualified snapshot factory", err)
		}
		if _, err := DecodeIntent([]byte(`{"action":"` + action + `","parameters":{"name":"unreviewed"}}`)); err == nil {
			t.Fatal("editable snapshot parameters")
		}
	}
}
