package digitalocean

import (
	"context"
	"crypto/subtle"
	"encoding/json"

	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

// Dormant Platform v2 contract, reviewed 2026-10-26. Creation needs droplet:read,
// droplet:update and image:create. Native snapshot action completion is proof;
// the snapshot inventory model has no completion field or correlated image ID.
// No idempotency/retry window is assumed. No registration is performed.
// https://github.com/digitalocean/openapi/blob/master/specification/resources/droplets/dropletActions_post.yml
func (c *Client) SnapshotCreate() provider.SnapshotAction { return snapshotCreate{c} }

type snapshotCreate struct{ c *Client }

func (a snapshotCreate) Impact() string {
	return "Snapshot storage incurs provider charges until deletion. Quiesce applications for disk consistency; no hard spending estimate is available."
}

func (a snapshotCreate) Inspect(ctx context.Context, b provider.Binding, action, id string) (provider.Preconditions, error) {
	if a.c == nil || b != a.c.binding {
		return provider.Preconditions{}, provider.ErrBinding
	}
	if action != provider.SnapshotCreate {
		return provider.Preconditions{}, provider.ErrActionUnsupported
	}
	if _, err := provider.NumericID(json.Number(id)); err != nil {
		return provider.Preconditions{}, err
	}
	var response struct {
		Droplet *struct {
			droplet
			Locked *bool `json:"locked"`
		} `json:"droplet"`
	}
	if err := provider.GetJSON(ctx, b, a.c.binding, outbound.DigitalOcean, a.c.http, base+"/"+id, a.c.credential.Token, &response); err != nil {
		return provider.Preconditions{}, err
	}
	d := response.Droplet
	if d == nil || string(d.ID) != id || d.Locked == nil {
		return provider.Preconditions{}, provider.InvalidResponse()
	}
	o, err := mapDroplet(d.droplet)
	if err != nil {
		return provider.Preconditions{}, err
	}
	return provider.SnapshotServerReview(b, action, o, *d.Locked)
}

func matchesSnapshotAction(a *powerAction, id string) bool {
	if a == nil || a.Type != "snapshot" || string(a.ResourceID) != id || a.ResourceType != "droplet" {
		return false
	}
	_, err := provider.NumericID(a.ID)
	return err == nil
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
		Type string `json:"type"`
		Name string `json:"name"`
	}{"snapshot", "sama-" + id})
	var response struct {
		Action *powerAction `json:"action"`
	}
	r, err := provider.PostMutation(ctx, b, a.c.binding, outbound.DigitalOcean, a.c.http, base+"/"+id+"/actions", a.c.credential.Token, body, nil, &response)
	if err != nil || r.Outcome == "rejected" {
		return r, err
	}
	if !matchesSnapshotAction(response.Action, id) {
		return provider.Receipt{Outcome: "ambiguous"}, provider.InvalidResponse()
	}
	return provider.Receipt{Outcome: "accepted", Reference: string(response.Action.ID)}, nil
}

func (a snapshotCreate) Observe(ctx context.Context, b provider.Binding, action, id string, reference *string) (provider.ActionObservation, error) {
	if a.c == nil || b != a.c.binding {
		return provider.ActionObservation{}, provider.ErrBinding
	}
	if action != provider.SnapshotCreate {
		return provider.ActionObservation{}, provider.ErrActionUnsupported
	}
	if _, err := provider.NumericID(json.Number(id)); err != nil {
		return provider.ActionObservation{}, err
	}
	if reference == nil {
		return provider.ActionObservation{Outcome: "unknown"}, nil
	}
	if _, err := provider.NumericID(json.Number(*reference)); err != nil {
		return provider.ActionObservation{}, err
	}
	var response struct {
		Action *powerAction `json:"action"`
	}
	if err := provider.GetJSON(ctx, b, a.c.binding, outbound.DigitalOcean, a.c.http, base+"/"+id+"/actions/"+*reference, a.c.credential.Token, &response); err != nil {
		return provider.ActionObservation{}, err
	}
	native := response.Action
	if !matchesSnapshotAction(native, id) || string(native.ID) != *reference {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	switch native.Status {
	case "in-progress":
		return provider.ActionObservation{Outcome: "pending"}, nil
	case "errored":
		return provider.ActionObservation{Outcome: "failed", NativeEvidence: true}, nil
	case "completed":
		if native.CompletedAt == nil || native.CompletedAt.IsZero() {
			return provider.ActionObservation{Outcome: "unknown"}, nil
		}
		return provider.ActionObservation{Outcome: "succeeded", NativeEvidence: true}, nil
	default:
		return provider.ActionObservation{Outcome: "unknown"}, nil
	}
}
