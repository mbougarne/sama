package operation

import "sama/backend/internal/provider"

func snapshotAction(action string) bool {
	return action == provider.SnapshotCreate || action == provider.SnapshotDelete
}
func targetType(action string) string {
	if action == provider.SnapshotDelete {
		return "storage.snapshot"
	}
	return "compute.server"
}

// Snapshot adapters must be injected under an action-specific key. A family's
// existing power adapter is never a fallback for an unqualified snapshot action.
func factoryKey(family, action string) string {
	if snapshotAction(action) {
		return family + ":" + action
	}
	return family
}
