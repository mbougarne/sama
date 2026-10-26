package contabo

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

// Dormant contract, reviewed 2026-10-26: https://api.contabo.com/#tag/Snapshots
// POST and resource/tag permissions are required in addition to read access.
// The native response contains a snapshot ID, not an action/completion token.
// x-request-id is correlation, not idempotency; never replay an uncertain POST.
// Owner's narrow SAMA-159/160 exception permits mock implementation only while
// SAMA-132 qualification remains unverified. Production registration is closed.
func (c *Client) SnapshotCreate() provider.SnapshotAction { return snapshotCreate{c} }

type snapshotCreate struct{ c *Client }

func (a snapshotCreate) Impact() string {
	return "Snapshot storage may incur provider charges; no hard spending estimate is available. Quiesce applications for consistency. The snapshot receives a generated review-bound name."
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
	// API names allow letters/numbers/spaces/dashes, maximum 30 characters.
	name := "sama-" + hex.EncodeToString(fingerprint[:12])
	body, _ := json.Marshal(struct {
		Name string `json:"name"`
	}{name})
	token, err := a.c.tokens.get(ctx, nil)
	if err != nil {
		return provider.Receipt{}, err
	}
	requestID, err := uuid.NewRandom()
	if err != nil {
		return provider.Receipt{}, provider.ErrBinding
	}
	var response snapshotResponse
	receipt, err := provider.PostMutation(ctx, b, a.c.binding, outbound.Contabo, a.c.http, snapshotURL(id), token.value, body, http.Header{"X-Request-Id": {requestID.String()}}, &response)
	if err != nil || receipt.Outcome == "rejected" {
		return receipt, err
	}
	if response.Data == nil || len(*response.Data) != 1 {
		return provider.Receipt{Outcome: "ambiguous"}, provider.InvalidResponse()
	}
	o, err := mapSnapshot((*response.Data)[0])
	if err != nil || o.SourceNativeID != id || o.Name != name {
		return provider.Receipt{Outcome: "ambiguous"}, provider.InvalidResponse()
	}
	return provider.Receipt{Outcome: "accepted", Reference: o.NativeID}, nil
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
	source, _, err := snapshotParts(*reference)
	if err != nil || source != id {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	if _, err = a.c.ReadSnapshot(ctx, b, *reference); err != nil {
		return provider.ActionObservation{}, err
	}
	// Presence alone cannot establish readiness: this schema has no documented
	// status or completion field. Retain reservation until qualified reconciliation.
	return provider.ActionObservation{Outcome: "unknown"}, nil
}
