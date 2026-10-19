package digitalocean

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"time"

	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

// Platform API v2, reviewed 2026-10-19. Required scopes: droplet:read and
// droplet:update (account:read for prior identity checks). Shutdown sends ACPI;
// power_off is abrupt. Per-Droplet action polling uses droplet:read.
// POST has no assumed retry window. Actions bind type, resource and native ID;
// running state alone never proves reboot. Production registration stays closed.
// https://docs.digitalocean.com/reference/api/scopes/
// https://github.com/digitalocean/godo/blob/main/droplet_actions.go
// https://github.com/digitalocean/godo/blob/main/action.go
// https://github.com/digitalocean/openapi/blob/main/specification/resources/droplets/dropletActions_post.yml
// https://github.com/digitalocean/openapi/blob/main/specification/resources/droplets/dropletActions_get.yml
type powerAction struct {
	ID           json.Number `json:"id"`
	Status       string      `json:"status"`
	Type         string      `json:"type"`
	ResourceID   json.Number `json:"resource_id"`
	ResourceType string      `json:"resource_type"`
	CompletedAt  *time.Time  `json:"completed_at"`
}

func powerType(action string) string {
	switch action {
	case "compute.server.power_on":
		return "power_on"
	case "compute.server.shutdown":
		return "shutdown"
	case "compute.server.reboot":
		return "reboot"
	case "compute.server.power_off":
		return "power_off"
	}
	return ""
}

func (c *Client) Inspect(ctx context.Context, b provider.Binding, action, id string) (provider.Preconditions, error) {
	if b != c.binding {
		return provider.Preconditions{}, provider.ErrBinding
	}
	if powerType(action) == "" {
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
	if err := provider.GetJSON(ctx, b, c.binding, outbound.DigitalOcean, c.http, base+"/"+id, c.credential.Token, &response); err != nil {
		return provider.Preconditions{}, err
	}
	if response.Droplet == nil || string(response.Droplet.ID) != id || response.Droplet.Locked == nil {
		return provider.Preconditions{}, provider.InvalidResponse()
	}
	o, err := mapDroplet(response.Droplet.droplet)
	if err != nil {
		return provider.Preconditions{}, err
	}
	pre, err := provider.PowerPreconditions(b, action, o)
	pre.Allowed = pre.Allowed && !*response.Droplet.Locked
	return pre, err
}

func (a *powerAction) matches(action, id string) bool {
	if a == nil || a.Type != powerType(action) || string(a.ResourceID) != id || a.ResourceType != "droplet" {
		return false
	}
	_, err := provider.NumericID(a.ID)
	return err == nil
}

func (c *Client) Submit(ctx context.Context, b provider.Binding, action, id string, fingerprint [32]byte) (provider.Receipt, error) {
	pre, err := c.Inspect(ctx, b, action, id)
	if err != nil {
		return provider.Receipt{}, err
	}
	if !pre.Allowed || subtle.ConstantTimeCompare(pre.Fingerprint[:], fingerprint[:]) != 1 {
		return provider.Receipt{Outcome: "rejected"}, nil
	}
	body, _ := json.Marshal(struct {
		Type string `json:"type"`
	}{powerType(action)})
	var response struct {
		Action *powerAction `json:"action"`
	}
	r, err := provider.PostMutation(ctx, b, c.binding, outbound.DigitalOcean, c.http, base+"/"+id+"/actions", c.credential.Token, body, nil, &response)
	if err != nil || r.Outcome == "rejected" {
		return r, err
	}
	if !response.Action.matches(action, id) {
		return provider.Receipt{Outcome: "ambiguous"}, provider.InvalidResponse()
	}
	return provider.Receipt{Outcome: "accepted", Reference: string(response.Action.ID)}, nil
}

func (c *Client) Observe(ctx context.Context, b provider.Binding, action, id string, reference *string) (provider.ActionObservation, error) {
	if b != c.binding {
		return provider.ActionObservation{}, provider.ErrBinding
	}
	if powerType(action) == "" {
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
	if err := provider.GetJSON(ctx, b, c.binding, outbound.DigitalOcean, c.http, base+"/"+id+"/actions/"+*reference, c.credential.Token, &response); err != nil {
		return provider.ActionObservation{}, err
	}
	a := response.Action
	if !a.matches(action, id) || string(a.ID) != *reference {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	switch a.Status {
	case "in-progress":
		return provider.ActionObservation{Outcome: "pending"}, nil
	case "errored":
		return provider.ActionObservation{Outcome: "failed", NativeEvidence: true}, nil
	case "completed":
		if a.CompletedAt == nil || a.CompletedAt.IsZero() {
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
	default:
		return provider.ActionObservation{Outcome: "unknown"}, nil
	}
}

var _ provider.PowerReviewer = (*Client)(nil)
var _ provider.PowerMutation = (*Client)(nil)
