package hetzner

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"strings"

	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

// SnapshotCreate is dormant: no factory, intent or capability is registered.
// Cloud v1 read/write project token; POST /servers/{id}/actions/create_image
// with type=snapshot returns an action and image. No retry/idempotency promise
// is assumed. Reviewed 2026-10-26 against the official server/image clients:
// https://github.com/hetznercloud/hcloud-go/blob/main/hcloud/server.go
// https://github.com/hetznercloud/hcloud-go/blob/main/hcloud/image.go
func (c *Client) SnapshotCreate() provider.SnapshotAction { return snapshotCreate{c} }

type snapshotCreate struct{ c *Client }

func (a snapshotCreate) Impact() string {
	return "Snapshot storage incurs provider charges until deletion. Quiesce applications for a consistent disk image; no hard spending estimate is available."
}

func snapshotReview(b provider.Binding, action, id, name string, facts any, allowed bool) (provider.Preconditions, error) {
	data, err := json.Marshal(struct {
		Workspace, Connection, Family string
		Version                       int64
		Action, NativeID              string
		Facts                         any
	}{b.Workspace().String(), b.Connection().String(), b.Family(), b.Version(), action, id, facts})
	if err != nil {
		return provider.Preconditions{}, provider.InvalidResponse()
	}
	if name == "" {
		name = id
	}
	return provider.Preconditions{Fingerprint: sha256.Sum256(data), TargetName: name, Allowed: allowed, HighImpact: true}, nil
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
		Server *server `json:"server"`
	}
	if err := provider.GetJSON(ctx, b, a.c.binding, outbound.HetznerCloud, a.c.http, base+"/"+id, a.c.credential.Token, &response); err != nil {
		return provider.Preconditions{}, err
	}
	s := response.Server
	if s == nil || string(s.ID) != id || s.Locked == nil {
		return provider.Preconditions{}, provider.InvalidResponse()
	}
	if _, err := mapServer(*s); err != nil {
		return provider.Preconditions{}, err
	}
	return snapshotReview(b, action, id, s.Name, s, !*s.Locked && (s.Status == "running" || s.Status == "off"))
}

func createMatches(a *powerAction, serverID, imageID string) bool {
	if a == nil || a.Command != "create_image" || len(a.Resources) > 100 {
		return false
	}
	if _, err := provider.NumericID(a.ID); err != nil {
		return false
	}
	serverFound, imageFound := false, false
	for _, r := range a.Resources {
		serverFound = serverFound || r.Type == "server" && string(r.ID) == serverID
		imageFound = imageFound || r.Type == "image" && string(r.ID) == imageID
	}
	return serverFound && imageFound
}

func (a snapshotCreate) Submit(ctx context.Context, b provider.Binding, action, id string, fingerprint [32]byte) (provider.Receipt, error) {
	pre, err := a.Inspect(ctx, b, action, id)
	if err != nil {
		return provider.Receipt{}, err
	}
	if !pre.Allowed || subtle.ConstantTimeCompare(pre.Fingerprint[:], fingerprint[:]) != 1 {
		return provider.Receipt{Outcome: "rejected"}, nil
	}
	var response struct {
		Action *powerAction   `json:"action"`
		Image  *snapshotImage `json:"image"`
	}
	receipt, err := provider.PostMutation(ctx, b, a.c.binding, outbound.HetznerCloud, a.c.http, base+"/"+id+"/actions/create_image", a.c.credential.Token, []byte(`{"type":"snapshot"}`), nil, &response)
	if err != nil || receipt.Outcome == "rejected" {
		return receipt, err
	}
	if response.Image == nil {
		return provider.Receipt{Outcome: "ambiguous"}, provider.InvalidResponse()
	}
	imageID, err := provider.NumericID(response.Image.ID)
	if err != nil || response.Image.Type != "snapshot" || !createMatches(response.Action, id, imageID) {
		return provider.Receipt{Outcome: "ambiguous"}, provider.InvalidResponse()
	}
	return provider.Receipt{Outcome: "accepted", Reference: string(response.Action.ID) + "/" + imageID}, nil
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
	parts := strings.Split(*reference, "/")
	if len(parts) != 2 {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	for _, part := range parts {
		if _, err := provider.NumericID(json.Number(part)); err != nil {
			return provider.ActionObservation{}, err
		}
	}
	var response struct {
		Action *powerAction `json:"action"`
	}
	if err := provider.GetJSON(ctx, b, a.c.binding, outbound.HetznerCloud, a.c.http, "https://api.hetzner.cloud/v1/actions/"+parts[0], a.c.credential.Token, &response); err != nil {
		return provider.ActionObservation{}, err
	}
	native := response.Action
	if !createMatches(native, id, parts[1]) || string(native.ID) != parts[0] {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	switch native.Status {
	case "running":
		return provider.ActionObservation{Outcome: "pending"}, nil
	case "error":
		return provider.ActionObservation{Outcome: "failed", NativeEvidence: true}, nil
	case "success":
		image, err := a.c.ReadSnapshot(ctx, b, parts[1])
		if err != nil {
			return provider.ActionObservation{}, err
		}
		if image.SourceNativeID != id || image.SourceType != "compute.server" {
			return provider.ActionObservation{}, provider.InvalidResponse()
		}
		if image.Status == "available" {
			return provider.ActionObservation{Outcome: "succeeded", NativeEvidence: true}, nil
		}
		return provider.ActionObservation{Outcome: "pending"}, nil
	default:
		return provider.ActionObservation{Outcome: "unknown"}, nil
	}
}
