package contabo

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

// Compute Management v1, reviewed 2026-10-19. API POST permissions and target
// resource/tag permission are required. Shutdown is ACPI, stop is abrupt.
// x-request-id is correlation only; there is no assumed retry window. The 201
// instance/action echo is not a completion receipt. Reboot stays unknown without
// stronger native evidence. Production identity qualification remains required.
// https://api.contabo.com/#tag/Instance-Actions
func powerPath(action string) string {
	switch action {
	case "compute.server.power_on":
		return "start"
	case "compute.server.shutdown":
		return "shutdown"
	case "compute.server.reboot":
		return "restart"
	case "compute.server.power_off":
		return "stop"
	}
	return ""
}

func (c *Client) Inspect(ctx context.Context, b provider.Binding, action, id string) (provider.Preconditions, error) {
	if b != c.binding {
		return provider.Preconditions{}, provider.ErrBinding
	}
	if powerPath(action) == "" {
		return provider.Preconditions{}, provider.ErrActionUnsupported
	}
	o, err := c.Read(ctx, b, id)
	if err != nil {
		return provider.Preconditions{}, err
	}
	// The detail schema has no documented suspension/lock flag. Do not invent
	// one; unstable statuses fail closed and native permission/conflict errors
	// remain authoritative at submission.
	return provider.PowerPreconditions(b, action, o)
}

func (c *Client) Submit(ctx context.Context, b provider.Binding, action, id string, fingerprint [32]byte) (provider.Receipt, error) {
	pre, err := c.Inspect(ctx, b, action, id)
	if err != nil {
		return provider.Receipt{}, err
	}
	if !pre.Allowed || subtle.ConstantTimeCompare(pre.Fingerprint[:], fingerprint[:]) != 1 {
		return provider.Receipt{Outcome: "rejected"}, nil
	}
	token, err := c.tokens.get(ctx, nil)
	if err != nil {
		return provider.Receipt{}, err
	}
	requestID, err := uuid.NewRandom()
	if err != nil {
		return provider.Receipt{}, provider.ErrBinding
	}
	var response struct {
		Data *[]struct {
			ID     json.Number `json:"instanceId"`
			Action string      `json:"action"`
		} `json:"data"`
	}
	r, err := provider.PostMutation(ctx, b, c.binding, outbound.Contabo, c.http, base+"/"+id+"/actions/"+powerPath(action), token.value, nil, http.Header{"X-Request-Id": {requestID.String()}}, &response)
	if err != nil || r.Outcome == "rejected" {
		return r, err
	}
	if response.Data == nil || len(*response.Data) != 1 || string((*response.Data)[0].ID) != id || (*response.Data)[0].Action != powerPath(action) {
		return r, provider.InvalidResponse()
	}
	return r, nil // Never persist the request UUID as a native action reference.
}

func (c *Client) Observe(ctx context.Context, b provider.Binding, action, id string, reference *string) (provider.ActionObservation, error) {
	if b != c.binding {
		return provider.ActionObservation{}, provider.ErrBinding
	}
	if powerPath(action) == "" {
		return provider.ActionObservation{}, provider.ErrActionUnsupported
	}
	if _, err := provider.NumericID(json.Number(id)); err != nil {
		return provider.ActionObservation{}, err
	}
	if reference != nil {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	if action == "compute.server.reboot" {
		return provider.ActionObservation{Outcome: "unknown"}, nil
	}
	o, err := c.Read(ctx, b, id)
	if err != nil {
		return provider.ActionObservation{}, err
	}
	want := "running"
	if action == "compute.server.shutdown" || action == "compute.server.power_off" {
		want = "stopped"
	}
	if o.Status == want {
		return provider.ActionObservation{Outcome: "succeeded", NativeEvidence: true}, nil
	}
	return provider.ActionObservation{Outcome: "pending"}, nil
}

var _ provider.PowerReviewer = (*Client)(nil)
var _ provider.PowerMutation = (*Client)(nil)
