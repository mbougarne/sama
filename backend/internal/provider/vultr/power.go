package vultr

import (
	"context"
	"crypto/subtle"

	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

// Compute API v2, reviewed 2026-10-19. API-key ACL/IP access must permit reads
// and the selected mutation. No retry window or durable action receipt is
// documented by the official client. Halt is never used as graceful shutdown.
// Reboot therefore stays unresolved even when the instance reads running.
// https://github.com/vultr/govultr/blob/master/instance.go
// https://www.vultr.com/api/#tag/instances
func powerPath(action string) string {
	switch action {
	case "compute.server.power_on":
		return "start"
	case "compute.server.reboot":
		return "reboot"
	case "compute.server.power_off":
		return "halt"
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
	// The native 204 has no action ID. Preserve that fact for reconciliation.
	return provider.PostMutation(ctx, b, c.binding, outbound.Vultr, c.http, base+"/"+id+"/"+powerPath(action), c.credential.Token, nil, nil, nil)
}

func (c *Client) Observe(ctx context.Context, b provider.Binding, action, id string, reference *string) (provider.ActionObservation, error) {
	if b != c.binding {
		return provider.ActionObservation{}, provider.ErrBinding
	}
	if powerPath(action) == "" {
		return provider.ActionObservation{}, provider.ErrActionUnsupported
	}
	if !nativeID.MatchString(id) || reference != nil {
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
	if action == "compute.server.power_off" {
		want = "stopped"
	}
	if o.Status == want {
		return provider.ActionObservation{Outcome: "succeeded", NativeEvidence: true}, nil
	}
	return provider.ActionObservation{Outcome: "pending"}, nil
}

var _ provider.PowerReviewer = (*Client)(nil)
var _ provider.PowerMutation = (*Client)(nil)
