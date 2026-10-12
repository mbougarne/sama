// Package digitalocean implements the documented Platform API v2 read contract.
package digitalocean

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

const Family = "digitalocean.platform"
const base = "https://api.digitalocean.com/v2/droplets"

type Client struct {
	binding    provider.Binding
	credential connection.Credential
	http       *http.Client
}

func New(b provider.Binding, c connection.Credential, budget *outbound.Budget) (*Client, error) {
	if b.Family() != Family || c.Type != "bearer_v1" || c.Validate() != nil || budget == nil {
		return nil, provider.ErrBinding
	}
	return &Client{b, c, budget.Client(outbound.DigitalOcean, b.Connection(), b.Workspace().String()+"/"+b.Connection().String())}, nil
}

type droplet struct {
	ID     json.Number `json:"id"`
	Name   string      `json:"name"`
	Status string      `json:"status"`
	Size   string      `json:"size_slug"`
	Region struct {
		Slug string `json:"slug"`
	} `json:"region"`
}

func mapDroplet(d droplet) (provider.Observation, error) {
	id, err := provider.NumericID(d.ID)
	if err != nil {
		return provider.Observation{}, err
	}
	status := provider.NormalizeStatus(d.Status, map[string]string{"active": "running", "off": "stopped", "new": "pending"})
	return provider.CheckedObservation(provider.Observation{NativeID: id, NativeStatus: d.Status, Status: status, Name: d.Name, ObservedAt: time.Now().UTC(), Details: provider.ServerDetails{Region: d.Region.Slug, MachineType: d.Size}})
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
		Droplets *[]droplet `json:"droplets"`
		Links    *struct {
			Pages struct {
				Next string `json:"next"`
			} `json:"pages"`
		} `json:"links"`
	}
	err := provider.GetJSON(ctx, b, c.binding, outbound.DigitalOcean, c.http, base+"?per_page=50&page="+strconv.Itoa(page), c.credential.Token, &response)
	if err != nil {
		return provider.InventoryPage{}, err
	}
	if response.Droplets == nil || response.Links == nil || len(*response.Droplets) > 50 {
		return provider.InventoryPage{}, provider.InvalidResponse()
	}
	result := provider.InventoryPage{Data: []provider.Observation{}}
	for _, d := range *response.Droplets {
		o, err := mapDroplet(d)
		if err != nil {
			return provider.InventoryPage{}, err
		}
		result.Data = append(result.Data, o)
	}
	if next := response.Links.Pages.Next; next != "" {
		u, err := outbound.DigitalOcean.URL(next)
		if err != nil {
			return provider.InventoryPage{}, provider.InvalidResponse()
		}
		q, err := provider.StrictQuery(u.RawQuery, "page", "per_page")
		if err != nil || u.Path != "/v2/droplets" || q.Get("page") != strconv.Itoa(page+1) || q.Get("per_page") != "50" || page == 100 {
			return provider.InventoryPage{}, provider.InvalidResponse()
		}
		result.Next = strconv.Itoa(page + 1)
	}
	return result, nil
}

func (c *Client) Read(ctx context.Context, b provider.Binding, id string) (provider.Observation, error) {
	if _, err := provider.NumericID(json.Number(id)); err != nil {
		return provider.Observation{}, err
	}
	var response struct {
		Droplet *droplet `json:"droplet"`
	}
	if err := provider.GetJSON(ctx, b, c.binding, outbound.DigitalOcean, c.http, base+"/"+id, c.credential.Token, &response); err != nil {
		return provider.Observation{}, err
	}
	if response.Droplet == nil || string(response.Droplet.ID) != id {
		return provider.Observation{}, provider.InvalidResponse()
	}
	return mapDroplet(*response.Droplet)
}

// AccountUserID is deliberately named for the documented account.uuid semantics.
// It must not be substituted for a rotation-stable team or project identity.
func (c *Client) AccountUserID(ctx context.Context, b provider.Binding) (string, error) {
	var response struct {
		Account *struct {
			UUID string `json:"uuid"`
		} `json:"account"`
	}
	if err := provider.GetJSON(ctx, b, c.binding, outbound.DigitalOcean, c.http, "https://api.digitalocean.com/v2/account", c.credential.Token, &response); err != nil {
		return "", err
	}
	if response.Account == nil {
		return "", provider.InvalidResponse()
	}
	id, err := uuid.Parse(response.Account.UUID)
	if err != nil || id == uuid.Nil {
		return "", provider.InvalidResponse()
	}
	return id.String(), nil
}

func (c *Client) Validate(ctx context.Context, input connection.Credential) (string, []string, error) {
	if input != c.credential {
		return "", nil, provider.ErrBinding
	}
	if _, err := c.AccountUserID(ctx, c.binding); err != nil {
		return "", nil, err
	}
	if _, err := c.Page(ctx, c.binding, ""); err != nil {
		return "", nil, err
	}
	return "", nil, provider.ErrIdentityUnverified
}
