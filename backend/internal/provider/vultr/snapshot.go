package vultr

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

const snapshotsBase = "https://api.vultr.com/v2/snapshots"

// API v2 GET contracts, reviewed 2026-10-26 against the official models:
// https://github.com/vultr/govultr/blob/master/snapshot.go
// https://github.com/vultr/vultr-csharp/blob/main/docs/Snapshot.md
// Size is bytes. No source instance ID is returned by this model; never infer
// it from a description, URL or unrelated instance inventory. Description and
// compressed_size (whose unit is unqualified here) are intentionally omitted.
type snapshot struct {
	ID      string `json:"id"`
	Created string `json:"date_created"`
	Size    *int64 `json:"size"`
	Status  string `json:"status"`
}

func mapSnapshot(s snapshot) (provider.SnapshotObservation, error) {
	if !nativeID.MatchString(s.ID) {
		return provider.SnapshotObservation{}, provider.InvalidResponse()
	}
	o := provider.SnapshotObservation{NativeID: s.ID, NativeStatus: s.Status, Status: "unknown", SizeBytes: s.Size, Regions: []string{}, NativeCreatedAt: s.Created, ObservedAt: time.Now().UTC()}
	switch s.Status {
	case "complete":
		o.Status = "available"
	case "pending":
		o.Status = "pending"
	case "deleted":
		o.Status = "deleted"
	}
	if s.Created != "" {
		if created, err := time.Parse(time.RFC3339, s.Created); err == nil {
			utc := created.UTC()
			o.CreatedAt = &utc
		} else {
			// Official SDK fixtures contain a timestamp without an offset. Its instant
			// is unavailable until the timezone is established; assigning UTC would guess.
			if _, err = time.Parse(time.DateTime, s.Created); err != nil {
				return provider.SnapshotObservation{}, provider.InvalidResponse()
			}
			o.CreationTimeReason = "timezone_not_reported"
		}
	} else {
		o.CreationTimeReason = "not_reported"
	}
	return provider.CheckedSnapshot(o)
}

func (c *Client) SnapshotPage(ctx context.Context, b provider.Binding, cursor string) (provider.SnapshotPage, error) {
	raw := snapshotsBase + "?per_page=50"
	page, nativeCursor := 1, ""
	if cursor != "" {
		parts := strings.SplitN(cursor, ":", 2)
		if len(parts) != 2 {
			return provider.SnapshotPage{}, provider.InvalidResponse()
		}
		var err error
		page, err = strconv.Atoi(parts[0])
		nativeCursor = parts[1]
		if err != nil || page < 2 || page > 100 || strconv.Itoa(page) != parts[0] || !safeCursor.MatchString(nativeCursor) {
			return provider.SnapshotPage{}, provider.InvalidResponse()
		}
		raw += "&cursor=" + url.QueryEscape(nativeCursor)
	}
	var response struct {
		Snapshots *[]snapshot `json:"snapshots"`
		Meta      *struct {
			Links *struct {
				Next string `json:"next"`
			} `json:"links"`
		} `json:"meta"`
	}
	if err := provider.GetJSON(ctx, b, c.binding, outbound.Vultr, c.http, raw, c.credential.Token, &response); err != nil {
		return provider.SnapshotPage{}, err
	}
	if response.Snapshots == nil || response.Meta == nil || response.Meta.Links == nil || len(*response.Snapshots) > 50 {
		return provider.SnapshotPage{}, provider.InvalidResponse()
	}
	result := provider.SnapshotPage{Data: []provider.SnapshotObservation{}}
	for _, s := range *response.Snapshots {
		o, err := mapSnapshot(s)
		if err != nil {
			return provider.SnapshotPage{}, err
		}
		result.Data = append(result.Data, o)
	}
	if next := response.Meta.Links.Next; next != "" {
		if !safeCursor.MatchString(next) || next == nativeCursor || page == 100 {
			return provider.SnapshotPage{}, provider.InvalidResponse()
		}
		result.Next = strconv.Itoa(page+1) + ":" + next
	}
	return result, nil
}

func (c *Client) ReadSnapshot(ctx context.Context, b provider.Binding, id string) (provider.SnapshotObservation, error) {
	if !nativeID.MatchString(id) {
		return provider.SnapshotObservation{}, provider.InvalidResponse()
	}
	var response struct {
		Snapshot *snapshot `json:"snapshot"`
	}
	if err := provider.GetJSON(ctx, b, c.binding, outbound.Vultr, c.http, snapshotsBase+"/"+id, c.credential.Token, &response); err != nil {
		return provider.SnapshotObservation{}, err
	}
	if response.Snapshot == nil || response.Snapshot.ID != id {
		return provider.SnapshotObservation{}, provider.InvalidResponse()
	}
	return mapSnapshot(*response.Snapshot)
}

var _ provider.SnapshotReader = (*Client)(nil)
