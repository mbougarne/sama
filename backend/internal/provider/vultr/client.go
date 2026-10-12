// Package vultr implements the documented Compute API v2 read contract.
package vultr

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

const Family = "vultr.compute"
const base = "https://api.vultr.com/v2/instances"

var nativeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var safeCursor = regexp.MustCompile(`^[A-Za-z0-9_=-]{1,512}$`)

type Client struct {
	binding    provider.Binding
	credential connection.Credential
	http       *http.Client
}

func New(b provider.Binding, c connection.Credential, budget *outbound.Budget) (*Client, error) {
	if b.Family() != Family || c.Type != "bearer_v1" || c.Validate() != nil || budget == nil {
		return nil, provider.ErrBinding
	}
	return &Client{b, c, budget.Client(outbound.Vultr, b.Connection(), b.Workspace().String()+"/"+b.Connection().String())}, nil
}

type instance struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Power  string `json:"power_status"`
	Server string `json:"server_status"`
	Region string `json:"region"`
	Plan   string `json:"plan"`
}

func mapInstance(i instance) (provider.Observation, error) {
	if !nativeID.MatchString(i.ID) {
		return provider.Observation{}, provider.InvalidResponse()
	}
	native := i.Status + "/" + i.Power + "/" + i.Server
	status := "unknown"
	if i.Status == "active" && i.Server == "ok" {
		status = provider.NormalizeStatus(i.Power, map[string]string{"running": "running", "stopped": "stopped"})
	}
	return provider.CheckedObservation(provider.Observation{NativeID: i.ID, NativeStatus: native, Status: status, Name: i.Label, ObservedAt: time.Now().UTC(), Details: provider.ServerDetails{Region: i.Region, MachineType: i.Plan}})
}

func (c *Client) Page(ctx context.Context, b provider.Binding, cursor string) (provider.InventoryPage, error) {
	raw := base + "?per_page=50"
	if cursor != "" {
		if !safeCursor.MatchString(cursor) {
			return provider.InventoryPage{}, provider.InvalidResponse()
		}
		raw += "&cursor=" + url.QueryEscape(cursor)
	}
	var response struct {
		Instances *[]instance `json:"instances"`
		Meta      *struct {
			Links *struct {
				Next string `json:"next"`
			} `json:"links"`
		} `json:"meta"`
	}
	if err := provider.GetJSON(ctx, b, c.binding, outbound.Vultr, c.http, raw, c.credential.Token, &response); err != nil {
		return provider.InventoryPage{}, err
	}
	if response.Instances == nil || response.Meta == nil || response.Meta.Links == nil || len(*response.Instances) > 50 {
		return provider.InventoryPage{}, provider.InvalidResponse()
	}
	result := provider.InventoryPage{Data: []provider.Observation{}}
	for _, i := range *response.Instances {
		o, err := mapInstance(i)
		if err != nil {
			return provider.InventoryPage{}, err
		}
		result.Data = append(result.Data, o)
	}
	next := response.Meta.Links.Next
	// The official SDK treats meta.links.next as an opaque cursor, not a URL.
	if next != "" {
		if !safeCursor.MatchString(next) || next == cursor {
			return provider.InventoryPage{}, provider.InvalidResponse()
		}
		result.Next = next
	}
	return result, nil
}

func (c *Client) Read(ctx context.Context, b provider.Binding, id string) (provider.Observation, error) {
	if !nativeID.MatchString(id) {
		return provider.Observation{}, provider.InvalidResponse()
	}
	var response struct {
		Instance *instance `json:"instance"`
	}
	if err := provider.GetJSON(ctx, b, c.binding, outbound.Vultr, c.http, base+"/"+id, c.credential.Token, &response); err != nil {
		return provider.Observation{}, err
	}
	if response.Instance == nil || response.Instance.ID != id {
		return provider.Observation{}, provider.InvalidResponse()
	}
	return mapInstance(*response.Instance)
}

func (c *Client) Validate(ctx context.Context, input connection.Credential) (string, []string, error) {
	if input != c.credential {
		return "", nil, provider.ErrBinding
	}
	var response struct {
		Account *struct {
			ACL []string `json:"acl"`
		} `json:"account"`
	}
	if err := provider.GetJSON(ctx, c.binding, c.binding, outbound.Vultr, c.http, "https://api.vultr.com/v2/account", c.credential.Token, &response); err != nil {
		return "", nil, err
	}
	if response.Account == nil {
		return "", nil, provider.InvalidResponse()
	}
	if _, err := c.Page(ctx, c.binding, ""); err != nil {
		return "", nil, err
	}
	// The documented account schema has names/email/ACL but no immutable ID.
	return "", nil, provider.ErrIdentityUnverified
}
