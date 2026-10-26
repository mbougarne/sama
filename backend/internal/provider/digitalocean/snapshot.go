package digitalocean

import (
	"context"
	"regexp"
	"strconv"
	"time"

	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

const snapshotsBase = "https://api.digitalocean.com/v2/snapshots"

var snapshotID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,127}$`)

// Official Platform API v2 snapshot schemas, reviewed 2026-10-26:
// https://github.com/digitalocean/openapi/tree/master/specification/resources/snapshots
// GET list uses snapshot:read; get documents snapshot:read and
// block_storage_snapshot:read. No status field is documented. Never infer
// completion from a timestamp, listed presence, or an undocumented status.
type snapshot struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	ResourceID   string     `json:"resource_id"`
	ResourceType string     `json:"resource_type"`
	Created      *time.Time `json:"created_at"`
	Regions      []string   `json:"regions"`
	Size         *float64   `json:"size_gigabytes"`
	MinimumDisk  *float64   `json:"min_disk_size"`
}

func mapSnapshot(s snapshot) (provider.SnapshotObservation, error) {
	if !snapshotID.MatchString(s.ID) || !snapshotID.MatchString(s.ResourceID) {
		return provider.SnapshotObservation{}, provider.InvalidResponse()
	}
	kind := ""
	switch s.ResourceType {
	case "droplet":
		kind = "compute.server"
	case "volume":
		kind = "storage.volume"
	default:
		return provider.SnapshotObservation{}, provider.InvalidResponse()
	}
	return provider.CheckedSnapshot(provider.SnapshotObservation{NativeID: s.ID, Name: s.Name, SourceNativeID: s.ResourceID, SourceType: kind, Status: "unknown", StatusReason: "not_reported", CreatedAt: s.Created, Regions: s.Regions, SizeGB: s.Size, MinimumDiskGB: s.MinimumDisk, ObservedAt: time.Now().UTC()})
}

func (c *Client) SnapshotPage(ctx context.Context, b provider.Binding, cursor string) (provider.SnapshotPage, error) {
	page := 1
	var err error
	if cursor != "" {
		page, err = strconv.Atoi(cursor)
	}
	if err != nil || page < 1 || page > 100 {
		return provider.SnapshotPage{}, provider.InvalidResponse()
	}
	var response struct {
		Snapshots *[]snapshot `json:"snapshots"`
		Links     *struct {
			Pages struct {
				Next string `json:"next"`
			} `json:"pages"`
		} `json:"links"`
	}
	if err = provider.GetJSON(ctx, b, c.binding, outbound.DigitalOcean, c.http, snapshotsBase+"?per_page=50&page="+strconv.Itoa(page), c.credential.Token, &response); err != nil {
		return provider.SnapshotPage{}, err
	}
	if response.Snapshots == nil || response.Links == nil || len(*response.Snapshots) > 50 {
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
	if next := response.Links.Pages.Next; next != "" {
		u, err := outbound.DigitalOcean.URL(next)
		if err != nil {
			return provider.SnapshotPage{}, provider.InvalidResponse()
		}
		q, err := provider.StrictQuery(u.RawQuery, "page", "per_page")
		if err != nil || u.Path != "/v2/snapshots" || q.Get("page") != strconv.Itoa(page+1) || q.Get("per_page") != "50" || page == 100 {
			return provider.SnapshotPage{}, provider.InvalidResponse()
		}
		result.Next = strconv.Itoa(page + 1)
	}
	return result, nil
}

func (c *Client) ReadSnapshot(ctx context.Context, b provider.Binding, id string) (provider.SnapshotObservation, error) {
	if !snapshotID.MatchString(id) {
		return provider.SnapshotObservation{}, provider.InvalidResponse()
	}
	var response struct {
		Snapshot *snapshot `json:"snapshot"`
	}
	if err := provider.GetJSON(ctx, b, c.binding, outbound.DigitalOcean, c.http, snapshotsBase+"/"+id, c.credential.Token, &response); err != nil {
		return provider.SnapshotObservation{}, err
	}
	if response.Snapshot == nil || response.Snapshot.ID != id {
		return provider.SnapshotObservation{}, provider.InvalidResponse()
	}
	return mapSnapshot(*response.Snapshot)
}

var _ provider.SnapshotReader = (*Client)(nil)
