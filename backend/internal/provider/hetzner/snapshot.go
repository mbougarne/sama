package hetzner

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

const imagesBase = "https://api.hetzner.cloud/v1/images"

// Read token; GET /images?type=snapshot and /images/{id}. Official schema:
// https://github.com/hetznercloud/hcloud-go/blob/main/hcloud/schema/image.go
// Reviewed 2026-10-26. Description, labels and image contents are excluded.
type snapshotImage struct {
	ID          json.Number `json:"id"`
	Name        *string     `json:"name"`
	Type        string      `json:"type"`
	Status      string      `json:"status"`
	Created     *time.Time  `json:"created"`
	ImageSize   *float64    `json:"image_size"`
	DiskSize    *float64    `json:"disk_size"`
	CreatedFrom *struct {
		ID json.Number `json:"id"`
	} `json:"created_from"`
}

func snapshotStatus(native string) string {
	switch native {
	case "creating":
		return "pending"
	case "available":
		return "available"
	default:
		return "unknown"
	}
}

func mapSnapshot(s snapshotImage) (provider.SnapshotObservation, error) {
	id, err := provider.NumericID(s.ID)
	if err != nil || s.Type != "snapshot" {
		return provider.SnapshotObservation{}, provider.InvalidResponse()
	}
	o := provider.SnapshotObservation{NativeID: id, NativeStatus: s.Status, Status: snapshotStatus(s.Status), CreatedAt: s.Created, SizeGB: s.ImageSize, MinimumDiskGB: s.DiskSize, Regions: []string{}, ObservedAt: time.Now().UTC()}
	if s.Name != nil {
		o.Name = *s.Name
	}
	if s.CreatedFrom != nil {
		o.SourceNativeID, err = provider.NumericID(s.CreatedFrom.ID)
		if err != nil {
			return provider.SnapshotObservation{}, err
		}
		o.SourceType = "compute.server"
	}
	return provider.CheckedSnapshot(o)
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
		Images *[]snapshotImage `json:"images"`
		Meta   struct {
			Pagination *struct {
				Page int  `json:"page"`
				Next *int `json:"next_page"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	if err = provider.GetJSON(ctx, b, c.binding, outbound.HetznerCloud, c.http, imagesBase+"?type=snapshot&per_page=50&page="+strconv.Itoa(page), c.credential.Token, &response); err != nil {
		return provider.SnapshotPage{}, err
	}
	if response.Images == nil || response.Meta.Pagination == nil || response.Meta.Pagination.Page != page || len(*response.Images) > 50 {
		return provider.SnapshotPage{}, provider.InvalidResponse()
	}
	result := provider.SnapshotPage{Data: []provider.SnapshotObservation{}}
	for _, s := range *response.Images {
		o, err := mapSnapshot(s)
		if err != nil {
			return provider.SnapshotPage{}, err
		}
		result.Data = append(result.Data, o)
	}
	if next := response.Meta.Pagination.Next; next != nil {
		if *next != page+1 || *next > 100 {
			return provider.SnapshotPage{}, provider.InvalidResponse()
		}
		result.Next = strconv.Itoa(*next)
	}
	return result, nil
}

func (c *Client) ReadSnapshot(ctx context.Context, b provider.Binding, id string) (provider.SnapshotObservation, error) {
	if _, err := provider.NumericID(json.Number(id)); err != nil {
		return provider.SnapshotObservation{}, err
	}
	var response struct {
		Image *snapshotImage `json:"image"`
	}
	if err := provider.GetJSON(ctx, b, c.binding, outbound.HetznerCloud, c.http, imagesBase+"/"+id, c.credential.Token, &response); err != nil {
		return provider.SnapshotObservation{}, err
	}
	if response.Image == nil || string(response.Image.ID) != id {
		return provider.SnapshotObservation{}, provider.InvalidResponse()
	}
	return mapSnapshot(*response.Image)
}

var _ provider.SnapshotReader = (*Client)(nil)
