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

type instance struct {
	ID       json.Number `json:"instanceId"`
	Name     string      `json:"name"`
	Display  string      `json:"displayName"`
	Status   string      `json:"status"`
	Region   string      `json:"region"`
	Product  string      `json:"productId"`
	Tenant   string      `json:"tenantId"`
	Customer string      `json:"customerId"`
}

func mapInstance(i instance) (provider.Observation, error) {
	id, err := provider.NumericID(i.ID)
	if err != nil {
		return provider.Observation{}, err
	}
	if !identityPart.MatchString(i.Tenant) || !identityPart.MatchString(i.Customer) {
		return provider.Observation{}, provider.InvalidResponse()
	}
	name := i.Display
	if name == "" {
		name = i.Name
	}
	status := provider.NormalizeStatus(i.Status, map[string]string{"running": "running", "stopped": "stopped", "provisioning": "pending", "installing": "pending", "error": "failed"})
	return provider.CheckedObservation(provider.Observation{NativeID: id, NativeStatus: i.Status, Status: status, Name: name, ObservedAt: time.Now().UTC(), Details: provider.ServerDetails{Region: i.Region, MachineType: i.Product}})
}

func (c *Client) Page(ctx context.Context, b provider.Binding, cursor string) (provider.InventoryPage, error) {
	page := 1
	if cursor != "" {
		p, err := strconv.Atoi(cursor)
		if err != nil {
			return provider.InventoryPage{}, provider.InvalidResponse()
		}
		page = p
	}
	if page < 1 || page > 100 {
		return provider.InventoryPage{}, provider.InvalidResponse()
	}
	var response struct {
		Data       *[]instance `json:"data"`
		Pagination *struct {
			Page  int `json:"page"`
			Pages int `json:"totalPages"`
			Size  int `json:"size"`
			Total int `json:"totalElements"`
		} `json:"_pagination"`
		Links *struct {
			Next string `json:"next"`
		} `json:"_links"`
	}
	if err := c.get(ctx, b, base+"?page="+strconv.Itoa(page)+"&size=50", &response); err != nil {
		return provider.InventoryPage{}, err
	}
	p := response.Pagination
	if response.Data == nil || response.Links == nil || p == nil || p.Page != page || p.Size < 1 || p.Size > 50 || len(*response.Data) > p.Size || p.Total < 0 || p.Pages < 0 || p.Pages > 100 || (p.Total > 0 && p.Pages < page) {
		return provider.InventoryPage{}, provider.InvalidResponse()
	}
	result := provider.InventoryPage{Data: []provider.Observation{}}
	account := ""
	for _, i := range *response.Data {
		current := i.Tenant + "/" + i.Customer
		if account != "" && account != current {
			return provider.InventoryPage{}, provider.ErrIdentityUnverified
		}
		account = current
		o, err := mapInstance(i)
		if err != nil {
			return provider.InventoryPage{}, err
		}
		result.Data = append(result.Data, o)
	}
	next := response.Links.Next
	if next != "" {
		raw := next
		if strings.HasPrefix(next, "/v1/") {
			raw = "https://api.contabo.com" + next
		}
		u, err := outbound.Contabo.URL(raw)
		if err != nil {
			return provider.InventoryPage{}, provider.InvalidResponse()
		}
		q, err := provider.StrictQuery(u.RawQuery, "page", "size")
		if err != nil || u.Path != "/v1/compute/instances" || q.Get("page") != strconv.Itoa(page+1) || (q.Get("size") != "" && q.Get("size") != "50") || p.Pages <= page || page == 100 {
			return provider.InventoryPage{}, provider.InvalidResponse()
		}
		// Reconstruct approved URL with explicit page size; never follow raw links.
		result.Next = strconv.Itoa(page + 1)
	} else if p.Pages > page {
		return provider.InventoryPage{}, provider.InvalidResponse()
	}
	return result, nil
}

func (c *Client) Read(ctx context.Context, b provider.Binding, id string) (provider.Observation, error) {
	if _, err := provider.NumericID(json.Number(id)); err != nil {
		return provider.Observation{}, err
	}
	var response struct {
		Data *[]instance `json:"data"`
	}
	if err := c.get(ctx, b, base+"/"+id, &response); err != nil {
		return provider.Observation{}, err
	}
	if response.Data == nil || len(*response.Data) != 1 || string((*response.Data)[0].ID) != id {
		return provider.Observation{}, provider.InvalidResponse()
	}
	return mapInstance((*response.Data)[0])
}
