package inventory

import (
	"math"
	"testing"
	"time"

	"sama/backend/internal/provider"
)

func TestSnapshotMetadataPreservesUnknownAndRejectsInvalidFacts(t *testing.T) {
	observed := provider.SnapshotObservation{NativeID: "image", Status: "unknown", StatusReason: "not_reported", Regions: []string{"EU"}, ObservedAt: time.Now()}
	details, err := SnapshotMetadata(observed)
	if err != nil || details.Snapshot == nil || details.Snapshot.SizeBytes != nil || details.Snapshot.CreatedAt != nil || details.Snapshot.StatusReason != "not_reported" {
		t.Fatal(details, err)
	}
	observed.Regions[0] = "changed"
	if details.Snapshot.Regions[0] != "EU" {
		t.Fatal("mutable provider slice retained")
	}
	for _, mutate := range []func(*provider.SnapshotObservation){
		func(o *provider.SnapshotObservation) { o.Status = "running" },
		func(o *provider.SnapshotObservation) { o.SourceType = "compute.server" },
		func(o *provider.SnapshotObservation) { size := int64(-1); o.SizeBytes = &size },
		func(o *provider.SnapshotObservation) { size := math.NaN(); o.SizeGB = &size },
		func(o *provider.SnapshotObservation) { o.ObservedAt = time.Time{} },
	} {
		copy := observed
		mutate(&copy)
		if _, err := SnapshotMetadata(copy); err != ErrObservation {
			t.Fatal("invalid provider facts admitted", err)
		}
	}
}
