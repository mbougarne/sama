package hetzner

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"time"

	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

// SnapshotDelete is dormant. Read/write project token and unprotected snapshot
// are required. DELETE /images/{id} returns 204, not a native action ID.
// Never retry DELETE, including after a lost response. Reviewed 2026-10-26:
// https://github.com/hetznercloud/hcloud-go/blob/main/hcloud/image.go
func (c *Client) SnapshotDelete() provider.SnapshotAction { return snapshotDelete{c} }

type snapshotDelete struct{ c *Client }

func (a snapshotDelete) Impact() string {
	return "Permanently and irreversibly deletes this snapshot. Require recent authentication and typed-target confirmation. The source server is not deleted."
}

func (a snapshotDelete) Inspect(ctx context.Context, b provider.Binding, action, id string) (provider.Preconditions, error) {
	if a.c == nil || a.c.http == nil || b != a.c.binding {
		return provider.Preconditions{}, provider.ErrBinding
	}
	if action != provider.SnapshotDelete {
		return provider.Preconditions{}, provider.ErrActionUnsupported
	}
	if _, err := provider.NumericID(json.Number(id)); err != nil {
		return provider.Preconditions{}, err
	}
	var response struct {
		Image *struct {
			snapshotImage
			Protection *struct {
				Delete *bool `json:"delete"`
			} `json:"protection"`
			Deleted *time.Time `json:"deleted"`
		} `json:"image"`
	}
	if err := provider.GetJSON(ctx, b, a.c.binding, outbound.HetznerCloud, a.c.http, imagesBase+"/"+id, a.c.credential.Token, &response); err != nil {
		return provider.Preconditions{}, err
	}
	image := response.Image
	if image == nil || string(image.ID) != id || image.Protection == nil || image.Protection.Delete == nil {
		return provider.Preconditions{}, provider.InvalidResponse()
	}
	o, err := mapSnapshot(image.snapshotImage)
	if err != nil {
		return provider.Preconditions{}, err
	}
	return snapshotReview(b, action, id, o.Name, image, o.Status == "available" && !*image.Protection.Delete && image.Deleted == nil)
}

// captureSnapshotStatus preserves the HTTP fact, not a provider body. Used only
// with an exact binding/validated endpoint and the existing capped transport.
type captureSnapshotStatus struct {
	base   http.RoundTripper
	status *int
}

func (t captureSnapshotStatus) RoundTrip(r *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	response, err := base.RoundTrip(r)
	if err == nil && response != nil {
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
	u, err := outbound.HetznerCloud.URL(imagesBase + "/" + id)
	if err != nil {
		return provider.Receipt{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return provider.Receipt{}, provider.ErrBinding
	}
	request.Header.Set("Authorization", "Bearer "+a.c.credential.Token)
	request.Header.Set("Accept", "application/json")
	status := 0
	client := *a.c.http
	client.Transport = captureSnapshotStatus{a.c.http.Transport, &status}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return outbound.ErrEndpoint }
	err = outbound.RequestJSON(ctx, &client, request, nil)
	if err != nil {
		switch status {
		case 400, 401, 403, 404, 409, 422:
			return provider.Receipt{Outcome: "rejected"}, nil
		}
		return provider.Receipt{Outcome: "ambiguous"}, err
	}
	if status != 204 {
		return provider.Receipt{Outcome: "ambiguous"}, provider.InvalidResponse()
	}
	return provider.Receipt{Outcome: "accepted", Reference: "delete/" + id}, nil
}

func (a snapshotDelete) Observe(ctx context.Context, b provider.Binding, action, id string, reference *string) (provider.ActionObservation, error) {
	if a.c == nil || a.c.http == nil || b != a.c.binding {
		return provider.ActionObservation{}, provider.ErrBinding
	}
	if action != provider.SnapshotDelete {
		return provider.ActionObservation{}, provider.ErrActionUnsupported
	}
	if _, err := provider.NumericID(json.Number(id)); err != nil {
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
	client.Transport = captureSnapshotStatus{a.c.http.Transport, &status}
	var response struct {
		Image *snapshotImage `json:"image"`
	}
	err := provider.GetJSON(ctx, b, a.c.binding, outbound.HetznerCloud, &client, imagesBase+"/"+id, a.c.credential.Token, &response)
	if status == 404 && err != nil {
		return provider.ActionObservation{Outcome: "succeeded", NativeEvidence: true}, nil
	}
	if err != nil {
		return provider.ActionObservation{}, err
	}
	if response.Image == nil || string(response.Image.ID) != id || response.Image.Type != "snapshot" {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	return provider.ActionObservation{Outcome: "pending"}, nil
}
