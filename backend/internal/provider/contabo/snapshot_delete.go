package contabo

import (
	"context"
	"crypto/subtle"
	"net/http"

	"github.com/google/uuid"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

// Dormant v1 DELETE contract, reviewed 2026-10-26:
// https://api.contabo.com/#tag/Snapshots
// Requires API DELETE plus target resource/tag permissions and read access.
// Native204 acknowledges deletion; request UUID is correlation only. No retry
// guarantee or automatic token-refresh replay is assumed for mutations.
// The approved159/160 implementation exception leaves132 live qualification open.
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

// The private wrapper adds required correlation to the common one-shot DELETE
// transport, and captures read status without mutating the shared client.
type snapshotTransport struct {
	base      http.RoundTripper
	requestID string
	status    *int
}

func (t snapshotTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	request := r.Clone(r.Context())
	if t.requestID != "" {
		request.Header.Set("x-request-id", t.requestID)
	}
	response, err := t.base.RoundTrip(request)
	if response != nil && t.status != nil {
		*t.status = response.StatusCode
	}
	return response, err
}
func (a snapshotDelete) Submit(ctx context.Context, b provider.Binding, action, id string, fingerprint [32]byte) (provider.Receipt, error) {
	pre, err := a.Inspect(ctx, b, action, id)
	if err != nil {
		return provider.Receipt{}, err
	}
	if !pre.Allowed || subtle.ConstantTimeCompare(pre.Fingerprint[:], fingerprint[:]) != 1 {
		return provider.Receipt{Outcome: "rejected"}, nil
	}
	token, err := a.c.tokens.get(ctx, nil)
	if err != nil {
		return provider.Receipt{}, err
	}
	requestID, err := uuid.NewRandom()
	if err != nil {
		return provider.Receipt{}, provider.ErrBinding
	}
	instanceID, nativeID, err := snapshotParts(id)
	if err != nil {
		return provider.Receipt{}, err
	}
	client := *a.c.http
	client.Transport = snapshotTransport{base: client.Transport, requestID: requestID.String()}
	receipt, err := provider.DeleteSnapshotRequest(ctx, b, a.c.binding, outbound.Contabo, &client, snapshotURL(instanceID)+"/"+nativeID, token.value)
	if err == nil && receipt.Outcome == "accepted" {
		receipt.Reference = "delete/" + id
	}
	return receipt, err
}
func (a snapshotDelete) Observe(ctx context.Context, b provider.Binding, action, id string, reference *string) (provider.ActionObservation, error) {
	if a.c == nil || b != a.c.binding {
		return provider.ActionObservation{}, provider.ErrBinding
	}
	if action != provider.SnapshotDelete {
		return provider.ActionObservation{}, provider.ErrActionUnsupported
	}
	instanceID, nativeID, err := snapshotParts(id)
	if err != nil {
		return provider.ActionObservation{}, err
	}
	if reference == nil {
		return provider.ActionObservation{Outcome: "unknown"}, nil
	}
	if *reference != "delete/"+id {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	status := 0
	client := *a.c.http
	client.Transport = snapshotTransport{base: client.Transport, status: &status}
	reader := *a.c
	reader.http = &client
	var response snapshotResponse
	err = reader.get(ctx, b, snapshotURL(instanceID)+"/"+nativeID, &response)
	if status == 404 && err != nil {
		return provider.ActionObservation{Outcome: "succeeded", NativeEvidence: true}, nil
	}
	if err != nil {
		return provider.ActionObservation{}, err
	}
	if response.Data == nil || len(*response.Data) != 1 {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	o, err := mapSnapshot((*response.Data)[0])
	if err != nil || o.NativeID != id {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	return provider.ActionObservation{Outcome: "pending"}, nil
}
