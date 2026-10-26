package provider

import (
	"crypto/sha256"
	"encoding/json"
	"time"
)

// SnapshotServerReview is a provider-fact projection, never actor authority.
// The shared service must bind actor/policy/review and reserve the source before
// submission. Observation timestamps are deliberately excluded from the hash.
func SnapshotServerReview(b Binding, action string, o Observation, locked bool) (Preconditions, error) {
	if action != SnapshotCreate {
		return Preconditions{}, ErrActionUnsupported
	}
	o.ObservedAt = time.Time{}
	return snapshotPreconditions(b, action, o.NativeID, o.Name, struct {
		Observation
		Locked bool
	}{o, locked}, !locked && (o.Status == "running" || o.Status == "stopped"))
}

func SnapshotImageReview(b Binding, action string, o SnapshotObservation) (Preconditions, error) {
	if action != SnapshotDelete {
		return Preconditions{}, ErrActionUnsupported
	}
	o.ObservedAt = time.Time{}
	// An absent documented status (DigitalOcean) differs from an unknown native
	// state. The latter may not authorize deletion.
	allowed := o.Status == "available" || o.Status == "unknown" && o.StatusReason == "not_reported"
	return snapshotPreconditions(b, action, o.NativeID, o.Name, o, allowed)
}

func snapshotPreconditions(b Binding, action, id, name string, facts any, allowed bool) (Preconditions, error) {
	data, err := json.Marshal(struct {
		Workspace, Connection, Family string
		Version                       int64
		Action, NativeID              string
		Facts                         any
	}{b.Workspace().String(), b.Connection().String(), b.Family(), b.Version(), action, id, facts})
	if err != nil {
		return Preconditions{}, InvalidResponse()
	}
	if name == "" {
		name = id
	}
	return Preconditions{Fingerprint: sha256.Sum256(data), TargetName: name, Allowed: allowed, HighImpact: true}, nil
}
