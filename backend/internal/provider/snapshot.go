package provider

import (
	"context"
	"math"
	"time"
	"unicode/utf8"
)

// SnapshotObservation is metadata only, never image contents or a provider body.
// Missing source and nil sizes/creation time mean unavailable. GB retains provider units;
// sizes are not estimates of a bill. NativeStatus is empty when not reported.
type SnapshotObservation struct {
	NativeID       string     `json:"native_id"`
	Name           string     `json:"name"`
	SourceNativeID string     `json:"source_native_id,omitempty"`
	SourceType     string     `json:"source_type,omitempty"`
	Status         string     `json:"status"`
	NativeStatus   string     `json:"native_status"`
	StatusReason   string     `json:"status_reason,omitempty"`
	CreatedAt      *time.Time `json:"created_at"`
	SizeGB         *float64   `json:"size_gb"`
	MinimumDiskGB  *float64   `json:"minimum_disk_gb"`
	Regions        []string   `json:"regions"`
	ObservedAt     time.Time  `json:"observed_at"`
}

type SnapshotPage struct {
	Data []SnapshotObservation
	Next string
}

// SnapshotReader is a separate port from server inventory. A page is not a
// complete scan: publish/tombstone only after a successful bounded page walk.
// Constructors do not register this port or qualify a production capability.
type SnapshotReader interface {
	SnapshotPage(context.Context, Binding, string) (SnapshotPage, error)
	ReadSnapshot(context.Context, Binding, string) (SnapshotObservation, error)
}

func CheckedSnapshot(o SnapshotObservation) (SnapshotObservation, error) {
	if len(o.NativeID) == 0 || len(o.NativeID) > 512 || len(o.SourceNativeID) > 512 || !utf8.ValidString(o.Name) || utf8.RuneCountInString(o.Name) > 200 || len(o.NativeStatus) > 128 || len(o.Regions) > 100 || o.ObservedAt.IsZero() {
		return SnapshotObservation{}, InvalidResponse()
	}
	if o.SourceType != "" && o.SourceType != "compute.server" && o.SourceType != "storage.volume" || (o.SourceType == "") != (o.SourceNativeID == "") {
		return SnapshotObservation{}, InvalidResponse()
	}
	for _, size := range []*float64{o.SizeGB, o.MinimumDiskGB} {
		if size != nil && (math.IsNaN(*size) || math.IsInf(*size, 0) || *size < 0) {
			return SnapshotObservation{}, InvalidResponse()
		}
	}
	for _, region := range o.Regions {
		if region == "" || len(region) > 128 || !utf8.ValidString(region) {
			return SnapshotObservation{}, InvalidResponse()
		}
	}
	if o.CreatedAt != nil && o.CreatedAt.IsZero() {
		return SnapshotObservation{}, InvalidResponse()
	}
	return o, nil
}
