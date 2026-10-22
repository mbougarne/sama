package provider

import (
	"context"
	"time"
)

// DNSReader is an optional read-only port, separate from compute inventory.
// Authorization precedes binding construction; implementations reject a binding
// from another workspace, connection, family or credential version before I/O.
// Native zone IDs are interpreted only by the owning adapter.
type DNSReader interface {
	ReadDNS(context.Context, Binding, string) (DNSObservation, error)
}

type DNSValue struct {
	Data     string `json:"data"`
	Priority string `json:"priority,omitempty"`
}

// KnownType and Immutable are provider facts, never action permissions.
// A nil TTL/Immutable means unknown; its source must not be fabricated.
type DNSRecordSet struct {
	Name      string     `json:"name"`
	Type      string     `json:"type"`
	TTL       *int64     `json:"ttl"`
	TTLSource string     `json:"ttl_source"`
	Values    []DNSValue `json:"values"`
	KnownType bool       `json:"known_type"`
	Immutable *bool      `json:"immutable"`
}

// DNSObservation is an allowlisted projection. It carries no credentials,
// provider response body, capability grant or provider mutation interface.
// Version/fingerprint are read evidence rather than conditional-write tokens.
type DNSObservation struct {
	NativeID    string         `json:"native_id"`
	Name        string         `json:"name"`
	Version     string         `json:"native_version"`
	Fingerprint string         `json:"fingerprint"`
	ObservedAt  time.Time      `json:"observed_at"`
	RecordSets  []DNSRecordSet `json:"record_sets"`
}
