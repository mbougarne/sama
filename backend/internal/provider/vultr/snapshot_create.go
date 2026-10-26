package vultr

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
	"strings"
)

// Dormant v2 contract, reviewed 2026-10-26 against the official snapshot client:
// https://github.com/vultr/govultr/blob/master/snapshot.go
// The POST response supplies a new snapshot ID, not an action or idempotency
// token. The receipt binds that ID to the submitted source. Inventory itself
// does not report source identity, so absence of the receipt stays unknown.
func (c *Client) SnapshotCreate() provider.SnapshotAction { return snapshotCreate{c} }

type snapshotCreate struct{ c *Client }

func (a snapshotCreate) Impact() string {
	return "Snapshot storage incurs provider charges until deletion. Quiesce applications for consistency; no hard spending estimate is available."
}
func (a snapshotCreate) Inspect(ctx context.Context, b provider.Binding, action, id string) (provider.Preconditions, error) {
	if a.c == nil || b != a.c.binding {
		return provider.Preconditions{}, provider.ErrBinding
	}
	if action != provider.SnapshotCreate {
		return provider.Preconditions{}, provider.ErrActionUnsupported
	}
	o, err := a.c.Read(ctx, b, id)
	if err != nil {
		return provider.Preconditions{}, err
	}
	return provider.SnapshotServerReview(b, action, o, false)
}
func (a snapshotCreate) Submit(ctx context.Context, b provider.Binding, action, id string, fingerprint [32]byte) (provider.Receipt, error) {
	pre, err := a.Inspect(ctx, b, action, id)
	if err != nil {
		return provider.Receipt{}, err
	}
	if !pre.Allowed || subtle.ConstantTimeCompare(pre.Fingerprint[:], fingerprint[:]) != 1 {
		return provider.Receipt{Outcome: "rejected"}, nil
	}
	body, _ := json.Marshal(struct {
		Instance string `json:"instance_id"`
	}{id})
	var response struct {
		Snapshot *snapshot `json:"snapshot"`
	}
	r, err := provider.PostMutation(ctx, b, a.c.binding, outbound.Vultr, a.c.http, snapshotsBase, a.c.credential.Token, body, nil, &response)
	if err != nil || r.Outcome == "rejected" {
		return r, err
	}
	if response.Snapshot == nil {
		return provider.Receipt{Outcome: "ambiguous"}, provider.InvalidResponse()
	}
	if _, err = mapSnapshot(*response.Snapshot); err != nil {
		return provider.Receipt{Outcome: "ambiguous"}, err
	}
	return provider.Receipt{Outcome: "accepted", Reference: id + "/" + response.Snapshot.ID}, nil
}
func (a snapshotCreate) Observe(ctx context.Context, b provider.Binding, action, id string, reference *string) (provider.ActionObservation, error) {
	if a.c == nil || b != a.c.binding {
		return provider.ActionObservation{}, provider.ErrBinding
	}
	if action != provider.SnapshotCreate {
		return provider.ActionObservation{}, provider.ErrActionUnsupported
	}
	if !nativeID.MatchString(id) {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	if reference == nil {
		return provider.ActionObservation{Outcome: "unknown"}, nil
	}
	parts := strings.Split(*reference, "/")
	if len(parts) != 2 || parts[0] != id || !nativeID.MatchString(parts[1]) {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	o, err := a.c.ReadSnapshot(ctx, b, parts[1])
	if err != nil {
		return provider.ActionObservation{}, err
	}
	switch o.Status {
	case "available":
		return provider.ActionObservation{Outcome: "succeeded", NativeEvidence: true}, nil
	case "pending":
		return provider.ActionObservation{Outcome: "pending"}, nil
	default:
		return provider.ActionObservation{Outcome: "unknown"}, nil
	}
}
