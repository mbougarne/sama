// Package hetzner implements read-only Cloud API v1 contracts. Production
// validation remains closed until stable project identity can be established.
package hetzner

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

const Family = "hetzner.cloud"
const base = "https://api.hetzner.cloud/v1/servers"

type Client struct {
	binding    provider.Binding
	credential connection.Credential
	http       *http.Client
}

func New(b provider.Binding, c connection.Credential, budget *outbound.Budget) (*Client, error) {
	if b.Family() != Family || c.Type != "bearer_v1" || c.Validate() != nil || budget == nil {
		return nil, provider.ErrBinding
	}
	// Project-wide cooldown cannot be keyed safely until identity is verified.
	// These clients are explicit read interfaces, not production registration.
	return &Client{b, c, budget.Client(outbound.HetznerCloud, b.Connection(), b.Workspace().String()+"/"+b.Connection().String())}, nil
}

type server struct {
	ID       json.Number `json:"id"`
	Name     string      `json:"name"`
	Status   string      `json:"status"`
	Location struct {
		Name string `json:"name"`
	} `json:"location"`
	Type struct {
		Name string `json:"name"`
	} `json:"server_type"`
}

func mapServer(s server) (provider.Observation, error) {
	id, err := provider.NumericID(s.ID)
	if err != nil {
		return provider.Observation{}, err
	}
	status := provider.NormalizeStatus(s.Status, map[string]string{"running": "running", "off": "stopped", "initializing": "pending", "starting": "pending", "stopping": "pending", "deleting": "pending", "migrating": "pending", "rebuilding": "pending"})
	return provider.CheckedObservation(provider.Observation{NativeID: id, NativeStatus: s.Status, Status: status, Name: s.Name, ObservedAt: time.Now().UTC(), Details: provider.ServerDetails{Region: s.Location.Name, MachineType: s.Type.Name}})
}

func (c *Client) Page(ctx context.Context, b provider.Binding, cursor string) (provider.InventoryPage, error) {
	page := 1
	var err error
	if cursor != "" {
		page, err = strconv.Atoi(cursor)
	}
	if err != nil || page < 1 || page > 100 {
		return provider.InventoryPage{}, provider.InvalidResponse()
	}
	var response struct {
		Servers *[]server `json:"servers"`
		Meta    struct {
			Pagination *struct {
				Page int  `json:"page"`
				Next *int `json:"next_page"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	if err = provider.GetJSON(ctx, b, c.binding, outbound.HetznerCloud, c.http, base+"?per_page=50&page="+strconv.Itoa(page), c.credential.Token, &response); err != nil {
		return provider.InventoryPage{}, err
	}
	if response.Servers == nil || response.Meta.Pagination == nil || response.Meta.Pagination.Page != page || len(*response.Servers) > 50 {
		return provider.InventoryPage{}, provider.InvalidResponse()
	}
	result := provider.InventoryPage{Data: []provider.Observation{}}
	for _, s := range *response.Servers {
		o, err := mapServer(s)
		if err != nil {
			return provider.InventoryPage{}, err
		}
		result.Data = append(result.Data, o)
	}
	if next := response.Meta.Pagination.Next; next != nil {
		if *next != page+1 || *next > 100 {
			return provider.InventoryPage{}, provider.InvalidResponse()
		}
		result.Next = strconv.Itoa(*next)
	}
	return result, nil
}

func (c *Client) Read(ctx context.Context, b provider.Binding, id string) (provider.Observation, error) {
	if _, err := provider.NumericID(json.Number(id)); err != nil {
		return provider.Observation{}, err
	}
	var response struct {
		Server *server `json:"server"`
	}
	if err := provider.GetJSON(ctx, b, c.binding, outbound.HetznerCloud, c.http, base+"/"+id, c.credential.Token, &response); err != nil {
		return provider.Observation{}, err
	}
	if response.Server == nil || string(response.Server.ID) != id {
		return provider.Observation{}, provider.InvalidResponse()
	}
	return mapServer(*response.Server)
}

func (c *Client) Validate(ctx context.Context, input connection.Credential) (string, []string, error) {
	if input != c.credential {
		return "", nil, provider.ErrBinding
	}
	if _, err := c.Page(ctx, c.binding, ""); err != nil {
		return "", nil, err
	}
	// Successful server reads establish read access, never a project identity.
	return "", nil, provider.ErrIdentityUnverified
}
