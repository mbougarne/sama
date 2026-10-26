package digitalocean

import (
	"context"
	"crypto/subtle"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

// Dormant v2 DELETE contract, reviewed 2026-10-26. Official scopes are
// image:delete and snapshot:delete plus snapshot read permissions for review.
// https://github.com/digitalocean/openapi/blob/master/specification/resources/snapshots/snapshots_delete.yml
func (c *Client) SnapshotDelete() provider.SnapshotAction { return snapshotDelete{c} }

type snapshotDelete struct{ c *Client }

func (a snapshotDelete) Impact() string {
	return "Irreversibly deletes the snapshot. Require recent authentication and typed-target confirmation; the source resource is not deleted."
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
	r, err := provider.DeleteSnapshotRequest(ctx, b, a.c.binding, outbound.DigitalOcean, a.c.http, snapshotsBase+"/"+id, a.c.credential.Token)
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
	if !snapshotID.MatchString(id) {
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
	status, err := provider.SnapshotJSONStatus(ctx, b, a.c.binding, outbound.DigitalOcean, a.c.http, snapshotsBase+"/"+id, a.c.credential.Token, &response)
	if status == 404 && err != nil {
		return provider.ActionObservation{Outcome: "succeeded", NativeEvidence: true}, nil
	}
	if err != nil {
		return provider.ActionObservation{}, err
	}
	if response.Snapshot == nil || response.Snapshot.ID != id {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	if _, err = mapSnapshot(*response.Snapshot); err != nil {
		return provider.ActionObservation{}, err
	}
	return provider.ActionObservation{Outcome: "pending"}, nil
}
