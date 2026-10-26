package vultr

import (
	"context"
	"crypto/subtle"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

// Dormant DELETE contract. API-key ACL/IP access must permit snapshot reads and
// deletion. No permissions are changed; no retry window is assumed. Reviewed
// 2026-10-26: https://github.com/vultr/govultr/blob/master/snapshot.go
func (c *Client) SnapshotDelete() provider.SnapshotAction { return snapshotDelete{c} }

type snapshotDelete struct{ c *Client }

func (a snapshotDelete) Impact() string {
	return "Irreversibly deletes the snapshot. Require recent authentication and typed-target confirmation; the source instance is not deleted."
}
func (a snapshotDelete) Inspect(ctx context.Context, b provider.Binding, action, id string) (provider.Preconditions, error) {
	if a.c == nil || b != a.c.binding {
		return provider.Preconditions{}, provider.ErrBinding
	}
	if action != provider.SnapshotDelete {
		return provider.Preconditions{}, provider.ErrActionUnsupported
	}
	o, err := a.c.ReadSnapshot(ctx, b, id)
	if err != nil {
		return provider.Preconditions{}, err
	}
	return provider.SnapshotImageReview(b, action, o)
}
func (a snapshotDelete) Submit(ctx context.Context, b provider.Binding, action, id string, fingerprint [32]byte) (provider.Receipt, error) {
	pre, err := a.Inspect(ctx, b, action, id)
	if err != nil {
		return provider.Receipt{}, err
	}
	if !pre.Allowed || subtle.ConstantTimeCompare(pre.Fingerprint[:], fingerprint[:]) != 1 {
		return provider.Receipt{Outcome: "rejected"}, nil
	}
	r, err := provider.DeleteSnapshotRequest(ctx, b, a.c.binding, outbound.Vultr, a.c.http, snapshotsBase+"/"+id, a.c.credential.Token)
	if err == nil && r.Outcome == "accepted" {
		r.Reference = "delete/" + id
	}
	return r, err
}
func (a snapshotDelete) Observe(ctx context.Context, b provider.Binding, action, id string, reference *string) (provider.ActionObservation, error) {
	if a.c == nil || b != a.c.binding {
		return provider.ActionObservation{}, provider.ErrBinding
	}
	if action != provider.SnapshotDelete {
		return provider.ActionObservation{}, provider.ErrActionUnsupported
	}
	if !nativeID.MatchString(id) {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	if reference == nil {
		return provider.ActionObservation{Outcome: "unknown"}, nil
	}
	if *reference != "delete/"+id {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	var response struct {
		Snapshot *snapshot `json:"snapshot"`
	}
	status, err := provider.SnapshotJSONStatus(ctx, b, a.c.binding, outbound.Vultr, a.c.http, snapshotsBase+"/"+id, a.c.credential.Token, &response)
	if status == 404 && err != nil {
		return provider.ActionObservation{Outcome: "succeeded", NativeEvidence: true}, nil
	}
	if err != nil {
		return provider.ActionObservation{}, err
	}
	if response.Snapshot == nil || response.Snapshot.ID != id {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	o, err := mapSnapshot(*response.Snapshot)
	if err != nil {
		return provider.ActionObservation{}, err
	}
	if o.Status == "deleted" {
		return provider.ActionObservation{Outcome: "succeeded", NativeEvidence: true}, nil
	}
	return provider.ActionObservation{Outcome: "pending"}, nil
}
