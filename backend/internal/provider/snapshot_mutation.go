package provider

const SnapshotCreate = "compute.server.snapshot_create"
const SnapshotDelete = "storage.snapshot.delete"

// SnapshotAction reuses the existing review/worker ports without registration.
// The durable worker owns reservations: ambiguous receipts and unknown outcomes
// retain them, including when a native receipt was lost before DB persistence.
// Impact is server-owned text for future review composition, never an estimate.
type SnapshotAction interface {
	PowerReviewer
	PowerMutation
	Impact() string
}
