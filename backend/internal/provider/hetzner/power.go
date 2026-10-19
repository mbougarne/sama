package hetzner

import (
	"context"
	"crypto/subtle"
	"encoding/json"

	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

// Cloud API v1, reviewed 2026-10-19: read/write project token and server reads
// are required. No mutation retry/idempotency window is assumed. ACPI shutdown
// never falls back to hard poweroff. Native action completion and final state
// are required; registry enablement still requires live account qualification.
// https://docs.hetzner.cloud/reference/cloud#server-actions
// https://github.com/hetznercloud/hcloud-go/blob/main/hcloud/server.go
type powerAction struct {
	ID        json.Number `json:"id"`
	Command   string      `json:"command"`
	Status    string      `json:"status"`
	Resources []struct {
		ID   json.Number `json:"id"`
		Type string      `json:"type"`
	} `json:"resources"`
}

func powerCommand(action string) string {
	switch action {
	case "compute.server.power_on":
		return "start_server"
	case "compute.server.shutdown":
		return "shutdown_server"
	case "compute.server.reboot":
		return "reboot_server"
	case "compute.server.power_off":
		return "stop_server"
	}
	return ""
}

func powerPath(action string) string {
	switch action {
	case "compute.server.power_on":
		return "poweron"
	case "compute.server.shutdown":
		return "shutdown"
	case "compute.server.reboot":
		return "reboot"
	case "compute.server.power_off":
		return "poweroff"
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
	if _, err := provider.NumericID(json.Number(id)); err != nil {
		return provider.Preconditions{}, err
	}
	var response struct {
		Server *server `json:"server"`
	}
	if err := provider.GetJSON(ctx, b, c.binding, outbound.HetznerCloud, c.http, base+"/"+id, c.credential.Token, &response); err != nil {
		return provider.Preconditions{}, err
	}
	if response.Server == nil || string(response.Server.ID) != id || response.Server.Locked == nil {
		return provider.Preconditions{}, provider.InvalidResponse()
	}
	o, err := mapServer(*response.Server)
	if err != nil {
		return provider.Preconditions{}, err
	}
	pre, err := provider.PowerPreconditions(b, action, o)
	pre.Allowed = pre.Allowed && !*response.Server.Locked
	return pre, err
}

func (a *powerAction) matches(action, target string) bool {
	if a == nil || a.Command != powerCommand(action) || len(a.Resources) > 100 {
		return false
	}
	if _, err := provider.NumericID(a.ID); err != nil {
		return false
	}
	for _, r := range a.Resources {
		if string(r.ID) == target && r.Type == "server" {
			return true
		}
	}
	return false
}

func (c *Client) Submit(ctx context.Context, b provider.Binding, action, id string, fingerprint [32]byte) (provider.Receipt, error) {
	pre, err := c.Inspect(ctx, b, action, id)
	if err != nil {
		return provider.Receipt{}, err
	}
	if !pre.Allowed || subtle.ConstantTimeCompare(pre.Fingerprint[:], fingerprint[:]) != 1 {
		return provider.Receipt{Outcome: "rejected"}, nil
	}
	var response struct {
		Action *powerAction `json:"action"`
	}
	receipt, err := provider.PostMutation(ctx, b, c.binding, outbound.HetznerCloud, c.http, base+"/"+id+"/actions/"+powerPath(action), c.credential.Token, nil, nil, &response)
	if err != nil || receipt.Outcome == "rejected" {
		return receipt, err
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
	if powerPath(action) == "" {
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
	if err := provider.GetJSON(ctx, b, c.binding, outbound.HetznerCloud, c.http, "https://api.hetzner.cloud/v1/actions/"+*reference, c.credential.Token, &response); err != nil {
		return provider.ActionObservation{}, err
	}
	a := response.Action
	if !a.matches(action, id) || string(a.ID) != *reference {
		return provider.ActionObservation{}, provider.InvalidResponse()
	}
	switch a.Status {
	case "running":
		return provider.ActionObservation{Outcome: "pending"}, nil
	case "error":
		return provider.ActionObservation{Outcome: "failed", NativeEvidence: true}, nil
	case "success":
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
