// Package contabo confines Compute Management API v1 reads and OAuth acquisition.
package contabo

import (
	"context"
	"net/http"
	"regexp"

	"github.com/google/uuid"
	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

const Family = "contabo.compute"
const base = "https://api.contabo.com/v1/compute/instances"

var identityPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

type Client struct {
	binding    provider.Binding
	credential connection.Credential
	http       *http.Client
	tokens     *tokens
}

func New(b provider.Binding, c connection.Credential, budget *outbound.Budget) (*Client, error) {
	if b.Family() != Family || c.Type != "contabo_v1" || c.Validate() != nil || budget == nil {
		return nil, provider.ErrBinding
	}
	scope := b.Workspace().String() + "/" + b.Connection().String()
	return &Client{b, c, budget.Client(outbound.Contabo, b.Connection(), scope), newTokens(c, budget.Client(outbound.ContaboAuth, b.Connection(), scope))}, nil
}

func (c *Client) get(ctx context.Context, b provider.Binding, raw string, out any) error {
	if b != c.binding {
		return provider.ErrBinding
	}
	u, err := outbound.Contabo.URL(raw)
	if err != nil {
		return err
	}
	token, err := c.tokens.get(ctx, nil)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return provider.ErrBinding
		}
		req.Header.Set("Authorization", "Bearer "+token.value)
		req.Header.Set("Accept", "application/json")
		// UUID4 identifies this request for support; it is never an account identity.
		requestID, err := uuid.NewRandom()
		if err != nil {
			return provider.ErrBinding
		}
		req.Header.Set("x-request-id", requestID.String())
		denied := false
		client := *c.http
		client.Transport = unauthorizedTransport{c.http.Transport, &denied}
		err = outbound.RequestJSON(ctx, &client, req, out)
		if err == nil || !denied || attempt == 1 {
			return err
		}
		token, err = c.tokens.get(ctx, &token)
		if err != nil {
			return err
		}
	}
	return provider.ErrBinding
}

// DefaultAccountIdentity reads provider-owned identifiers; it does not switch
// account context or infer that a default account is the token's active account.
func (c *Client) DefaultAccountIdentity(ctx context.Context, b provider.Binding) (string, error) {
	var response struct {
		Data *[]struct {
			Tenant   string `json:"tenantId"`
			Customer string `json:"customerId"`
			Default  bool   `json:"default"`
		} `json:"data"`
		Pagination *struct {
			Page  int `json:"page"`
			Total int `json:"totalElements"`
			Pages int `json:"totalPages"`
		} `json:"_pagination"`
	}
	if err := c.get(ctx, b, "https://api.contabo.com/v1/me/accounts?default=true&page=1&size=50", &response); err != nil {
		return "", err
	}
	if response.Data == nil || response.Pagination == nil || response.Pagination.Page != 1 || response.Pagination.Total != 1 || response.Pagination.Pages != 1 || len(*response.Data) != 1 {
		return "", provider.ErrIdentityUnverified
	}
	account := (*response.Data)[0]
	if !account.Default || !identityPart.MatchString(account.Tenant) || !identityPart.MatchString(account.Customer) {
		return "", provider.ErrIdentityUnverified
	}
	return account.Tenant + "/" + account.Customer, nil
}

func (c *Client) Validate(ctx context.Context, input connection.Credential) (string, []string, error) {
	if input != c.credential {
		return "", nil, provider.ErrBinding
	}
	if _, err := c.DefaultAccountIdentity(ctx, c.binding); err != nil {
		return "", nil, err
	}
	if _, err := c.Page(ctx, c.binding, ""); err != nil {
		return "", nil, err
	}
	// Default account selection versus token-active account and rotation still
	// needs live contract evidence. Production registration remains closed.
	return "", nil, provider.ErrIdentityUnverified
}
