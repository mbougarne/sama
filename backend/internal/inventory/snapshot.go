package inventory

import (
	"time"

	"sama/backend/internal/provider"
)

const SnapshotType = "storage.snapshot"

// SnapshotDetails contains allowlisted metadata only. Nil sizes/time mean
// unavailable, not zero. Native units and unknown-state reasons stay explicit.
type SnapshotDetails struct {
	SourceNativeID     string     `json:"source_native_id,omitempty"`
	SourceType         string     `json:"source_type,omitempty"`
	StatusReason       string     `json:"status_reason,omitempty"`
	CreatedAt          *time.Time `json:"created_at"`
	NativeCreatedAt    string     `json:"native_created_at,omitempty"`
	CreationTimeReason string     `json:"creation_time_reason,omitempty"`
	SizeBytes          *int64     `json:"size_bytes"`
	SizeGB             *float64   `json:"size_gb"`
	MinimumDiskGB      *float64   `json:"minimum_disk_gb"`
	Regions            []string   `json:"regions"`
}

func SnapshotMetadata(o provider.SnapshotObservation) (Details, error) {
	if _, err := provider.CheckedSnapshot(o); err != nil {
		return Details{}, ErrObservation
	}
	switch o.Status {
	case "unknown", "available", "pending", "failed", "deleted":
	default:
		return Details{}, ErrObservation
	}
	return Details{Snapshot: &SnapshotDetails{
		SourceNativeID:     o.SourceNativeID,
		SourceType:         o.SourceType,
		StatusReason:       o.StatusReason,
		CreatedAt:          o.CreatedAt,
		NativeCreatedAt:    o.NativeCreatedAt,
		CreationTimeReason: o.CreationTimeReason,
		SizeBytes:          o.SizeBytes,
		SizeGB:             o.SizeGB,
		MinimumDiskGB:      o.MinimumDiskGB,
		Regions:            append([]string{}, o.Regions...),
	}}, nil
}
