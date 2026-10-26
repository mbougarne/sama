package inventory

import (
	"context"
	"time"

	"sama/backend/internal/provider"
)

type scanObservation struct {
	NativeID     string
	NativeStatus string
	Status       string
	Name         string
	ObservedAt   time.Time
	Details      Details
}
type scanPage struct {
	Data []scanObservation
	Next string
}

// Reader factories are injected explicitly. Production composition stays closed;
// selecting a resource type cannot register or qualify a provider adapter.
func (s Scanner) pageReader(ctx context.Context, b provider.Binding, kind string) (func(context.Context, string) (scanPage, error), error) {
	if kind == "compute.server" && s.Reader != nil {
		reader, err := s.Reader(ctx, b)
		if err != nil || reader == nil {
			return nil, ErrScan
		}
		return func(ctx context.Context, cursor string) (scanPage, error) {
			page, err := reader.Page(ctx, b, cursor)
			result := scanPage{Next: page.Next}
			for _, o := range page.Data {
				result.Data = append(result.Data, scanObservation{o.NativeID, o.NativeStatus, o.Status, o.Name, o.ObservedAt, Details{Region: o.Details.Region, MachineType: o.Details.MachineType}})
			}
			return result, err
		}, nil
	}
	if kind == SnapshotType && s.Snapshots != nil {
		reader, err := s.Snapshots(ctx, b)
		if err != nil || reader == nil {
			return nil, ErrScan
		}
		return func(ctx context.Context, cursor string) (scanPage, error) {
			page, err := reader.SnapshotPage(ctx, b, cursor)
			if err != nil {
				return scanPage{}, err
			}
			result := scanPage{Next: page.Next}
			for _, o := range page.Data {
				details, err := SnapshotMetadata(o)
				if err != nil {
					return scanPage{}, provider.InvalidResponse()
				}
				result.Data = append(result.Data, scanObservation{o.NativeID, o.NativeStatus, o.Status, o.Name, o.ObservedAt, details})
			}
			return result, nil
		}, nil
	}
	return nil, ErrScan
}
