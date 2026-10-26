package operation

import (
	"sama/backend/internal/provider"
	"testing"
)

func TestSnapshotContractsRemainDormant(t *testing.T) {
	for _, action := range []string{provider.SnapshotCreate, provider.SnapshotDelete} {
		if _, err := DecodeIntent([]byte(`{"action":"` + action + `","parameters":{}}`)); err == nil {
			t.Fatal("snapshot intent enabled without qualification", action)
		}
		policy := NewPolicy([]string{"hetzner/hetzner.cloud:" + action}, map[string]bool{})
		if policy.permits("hetzner/hetzner.cloud", action) {
			t.Fatal("switch bypassed closed registry")
		}
	}
}
