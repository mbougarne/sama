// Package provider defines small adapter ports and safe capability projections.
package provider

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
)

var ErrBinding = errors.New("provider binding unavailable")

type Binding struct {
	workspace  uuid.UUID
	connection uuid.UUID
	family     string
	version    int64
}

func NewBinding(workspace, connection uuid.UUID, family string, version int64) (Binding, error) {
	if workspace.Version() != 7 || connection.Version() != 7 || family == "" || len(family) > 128 || version < 1 {
		return Binding{}, ErrBinding
	}
	return Binding{workspace, connection, family, version}, nil
}

func (b Binding) Workspace() uuid.UUID  { return b.workspace }
func (b Binding) Connection() uuid.UUID { return b.connection }
func (b Binding) Family() string        { return b.family }
func (b Binding) Version() int64        { return b.version }

type Observation struct {
	NativeID     string
	NativeStatus string
	Status       string
	Name         string
	ObservedAt   time.Time
	Details      ServerDetails
}

type ServerDetails struct {
	Region      string `json:"region,omitempty"`
	MachineType string `json:"machine_type,omitempty"`
}

type InventoryPage struct {
	Data []Observation
	Next string
}

type InventoryReader interface {
	Read(context.Context, Binding, string) (Observation, error)
	Page(context.Context, Binding, string) (InventoryPage, error)
}

type Submission struct {
	NativeID  string
	Ambiguous bool
}

type ActionSubmitter interface {
	Submit(context.Context, Binding, string, string) (Submission, error)
}

type Capability struct {
	Action    string `json:"action"`
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

type Support struct {
	Action string
	Class  string
}

// Effective computes an intersection. Unknown support, permissions or state never
// grant authority; this projection is not a substitute for dispatch revalidation.
func Effective(catalog []Support, implemented, verified, granted, stateAllowed []string) []Capability {
	result := make([]Capability, 0, len(catalog))
	for _, capability := range catalog {
		reason := "available"
		switch {
		case !slices.Contains(implemented, capability.Action):
			reason = "unsupported"
		case !slices.Contains(verified, capability.Action):
			reason = "unverified"
		case !slices.Contains(granted, capability.Class):
			reason = "denied"
		case !slices.Contains(stateAllowed, capability.Action):
			reason = "state_restricted"
		}
		result = append(result, Capability{capability.Action, reason == "available", reason})
	}
	return result
}

// NormalizeStatus is supplied an adapter-owned allowlist, never a fallback state.
func NormalizeStatus(native string, known map[string]string) string {
	switch known[native] {
	case "running", "stopped", "pending", "failed":
		return known[native]
	default:
		return "unknown"
	}
}

// ServerCatalog describes the shared action vocabulary, not enabled adapters.
var ServerCatalog = []Support{
	{Action: "compute.server.read", Class: "read"},
	{Action: "compute.server.power_on", Class: "operate"},
	{Action: "compute.server.shutdown", Class: "operate"},
	{Action: "compute.server.power_off", Class: "operate"},
	{Action: "compute.server.reboot", Class: "operate"},
}
