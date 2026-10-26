package contabo

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

// Compute Management v1 snapshot reads, reviewed 2026-10-26:
// https://api.contabo.com/#tag/Snapshots
// API GET and target resource/tag permissions are required. The documented
// model reports neither size nor creation status. Presence is not readiness.
// Native IDs include the parent instance because snapshot paths are nested.
// Description, image metadata and contents are deliberately not retained.
type snapshot struct {
	ID       string      `json:"snapshotId"`
	Instance json.Number `json:"instanceId"`
	Name     string      `json:"name"`
	Created  string      `json:"createdDate"`
	Tenant   string      `json:"tenantId"`
	Customer string      `json:"customerId"`
}
type snapshotResponse struct {
	Data       *[]snapshot `json:"data"`
	Pagination *struct {
		Page  int `json:"page"`
		Pages int `json:"totalPages"`
		Size  int `json:"size"`
		Total int `json:"totalElements"`
	} `json:"_pagination"`
	Links struct {
		Next string `json:"next"`
	} `json:"_links"`
}

func snapshotParts(id string) (string, string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != 2 || !identityPart.MatchString(parts[1]) {
		return "", "", provider.InvalidResponse()
	}
	if _, err := provider.NumericID(json.Number(parts[0])); err != nil {
		return "", "", err
	}
	return parts[0], parts[1], nil
}
func snapshotURL(instanceID string) string { return base + "/" + instanceID + "/snapshots" }
func mapSnapshot(s snapshot) (provider.SnapshotObservation, error) {
	instanceID, err := provider.NumericID(s.Instance)
	if err != nil || !identityPart.MatchString(s.ID) || !identityPart.MatchString(s.Tenant) || !identityPart.MatchString(s.Customer) {
		return provider.SnapshotObservation{}, provider.InvalidResponse()
	}
	o := provider.SnapshotObservation{NativeID: instanceID + "/" + s.ID, Name: s.Name, SourceNativeID: instanceID, SourceType: "compute.server", Status: "unknown", StatusReason: "not_reported", NativeCreatedAt: s.Created, Regions: []string{}, ObservedAt: time.Now().UTC()}
	if s.Created == "" {
		o.CreationTimeReason = "not_reported"
	} else {
		created, err := time.Parse(time.RFC3339, s.Created)
		if err != nil {
			return provider.SnapshotObservation{}, provider.InvalidResponse()
		}
		utc := created.UTC()
		o.CreatedAt = &utc
	}
	return provider.CheckedSnapshot(o)
}

// Each call reads one bounded instance page and one snapshot page. The cursor
// records instance page/index, snapshot page, total step and expected source.
// Reordered/disappearing sources fail closed. At most 100 steps are allowed;
// hitting the cap is an incomplete scan, never permission to tombstone.
func (c *Client) SnapshotPage(ctx context.Context, b provider.Binding, cursor string) (provider.SnapshotPage, error) {
	ip, index, sp, step, expected := 1, 0, 1, 1, ""
	if cursor != "" {
		parts := strings.Split(cursor, ":")
		if len(parts) != 5 {
			return provider.SnapshotPage{}, provider.InvalidResponse()
		}
		values := []*int{&ip, &index, &sp, &step}
		for i, dest := range values {
			n, err := strconv.Atoi(parts[i])
			if err != nil || strconv.Itoa(n) != parts[i] {
				return provider.SnapshotPage{}, provider.InvalidResponse()
			}
			*dest = n
		}
		expected = parts[4]
		if ip < 1 || ip > 100 || index < 0 || index >= 50 || sp < 1 || sp > 100 || step < 2 || step > 100 {
			return provider.SnapshotPage{}, provider.InvalidResponse()
		}
		if expected != "" {
			if _, err := provider.NumericID(json.Number(expected)); err != nil {
				return provider.SnapshotPage{}, err
			}
		}
	}
	sources, err := c.Page(ctx, b, strconv.Itoa(ip))
	if err != nil {
		return provider.SnapshotPage{}, err
	}
	result := provider.SnapshotPage{Data: []provider.SnapshotObservation{}}
	if len(sources.Data) == 0 {
		if index != 0 || sp != 1 || expected != "" {
			return result, provider.InvalidResponse()
		}
		if sources.Next != "" {
			return result, provider.InvalidResponse()
		}
		return result, nil
	}
	if index >= len(sources.Data) {
		return result, provider.InvalidResponse()
	}
	id := sources.Data[index].NativeID
	if expected != "" && expected != id {
		return result, provider.InvalidResponse()
	}
	var response snapshotResponse
	raw := snapshotURL(id) + "?page=" + strconv.Itoa(sp) + "&size=50"
	if err = c.get(ctx, b, raw, &response); err != nil {
		return result, err
	}
	p := response.Pagination
	if response.Data == nil || p == nil || p.Page != sp || p.Size < 1 || p.Size > 50 || p.Pages < 0 || p.Pages > 100 || p.Total < 0 || p.Total > 5000 || len(*response.Data) > 50 || p.Pages < sp && !(sp == 1 && p.Pages == 0 && len(*response.Data) == 0) {
		return result, provider.InvalidResponse()
	}
	seen := map[string]bool{}
	for _, s := range *response.Data {
		o, err := mapSnapshot(s)
		if err != nil || o.SourceNativeID != id || seen[o.NativeID] {
			return provider.SnapshotPage{}, provider.InvalidResponse()
		}
		seen[o.NativeID] = true
		result.Data = append(result.Data, o)
	}
	next := response.Links.Next
	if next != "" {
		if strings.HasPrefix(next, "/v1/") {
			next = "https://api.contabo.com" + next
		}
		u, err := outbound.Contabo.URL(next)
		if err != nil {
			return provider.SnapshotPage{}, provider.InvalidResponse()
		}
		q, err := provider.StrictQuery(u.RawQuery, "page", "size")
		if err != nil || u.Path != "/v1/compute/instances/"+id+"/snapshots" || q.Get("page") != strconv.Itoa(sp+1) || q.Get("size") != "" && q.Get("size") != "50" || p.Pages <= sp {
			return provider.SnapshotPage{}, provider.InvalidResponse()
		}
		sp++
	} else {
		if p.Pages > sp {
			return provider.SnapshotPage{}, provider.InvalidResponse()
		}
		sp = 1
		index++
		if index < len(sources.Data) {
			id = sources.Data[index].NativeID
		} else {
			if sources.Next == "" {
				return result, nil
			}
			ip++
			index = 0
			id = ""
		}
	}
	if step == 100 {
		return provider.SnapshotPage{}, provider.InvalidResponse()
	}
	result.Next = strconv.Itoa(ip) + ":" + strconv.Itoa(index) + ":" + strconv.Itoa(sp) + ":" + strconv.Itoa(step+1) + ":" + id
	return result, nil
}
func (c *Client) ReadSnapshot(ctx context.Context, b provider.Binding, id string) (provider.SnapshotObservation, error) {
	instanceID, nativeID, err := snapshotParts(id)
	if err != nil {
		return provider.SnapshotObservation{}, err
	}
	var response snapshotResponse
	if err = c.get(ctx, b, snapshotURL(instanceID)+"/"+nativeID, &response); err != nil {
		return provider.SnapshotObservation{}, err
	}
	if response.Data == nil || len(*response.Data) != 1 {
		return provider.SnapshotObservation{}, provider.InvalidResponse()
	}
	o, err := mapSnapshot((*response.Data)[0])
	if err != nil || o.NativeID != id {
		return provider.SnapshotObservation{}, provider.InvalidResponse()
	}
	return o, nil
}

var _ provider.SnapshotReader = (*Client)(nil)
