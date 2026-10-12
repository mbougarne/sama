// Package qualification supplies explicit read probes, never registry evidence.
package qualification

import (
	"context"
	"errors"
	"time"

	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

type Reader interface {
	provider.InventoryReader
	connection.Validator
}

type Evidence struct {
	Pages            int  `json:"pages"`
	Resources        int  `json:"resources"`
	ListComplete     bool `json:"list_complete"`
	DetailChecked    bool `json:"detail_checked"`
	BindingIsolated  bool `json:"binding_isolated"`
	IdentityVerified bool `json:"identity_verified"`
	DenialChecked    bool `json:"denial_checked"`
}

// Denied requires another explicit credential with read scope denied. It never
// treats an empty collection or network outage as permission-denial evidence.
func Denied(ctx context.Context, b provider.Binding, r Reader) bool {
	if r == nil {
		return false
	}
	_, err := r.Page(ctx, b, "")
	return outbound.IsFailure(err, "provider_permission_denied")
}

// Probe returns partial evidence on failure. It never treats successful reads
// as identity qualification, records native payloads, or performs mutations.
func Probe(ctx context.Context, b provider.Binding, c connection.Credential, r Reader) (Evidence, error) {
	result := Evidence{}
	if r == nil || c.Validate() != nil {
		return result, provider.ErrBinding
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cursor := ""
	seen := map[string]bool{}
	first := ""
	for range 100 {
		page, err := r.Page(ctx, b, cursor)
		if err != nil {
			return result, err
		}
		if len(page.Data) > 200 || len(page.Next) > 512 || result.Resources+len(page.Data) > 10000 {
			return result, provider.InvalidResponse()
		}
		result.Pages++
		result.Resources += len(page.Data)
		if first == "" && len(page.Data) > 0 {
			first = page.Data[0].NativeID
		}
		if page.Next == "" {
			result.ListComplete = true
			break
		}
		if seen[page.Next] {
			return result, provider.InvalidResponse()
		}
		seen[page.Next] = true
		cursor = page.Next
	}
	if !result.ListComplete {
		return result, provider.InvalidResponse()
	}
	if first != "" {
		detail, err := r.Read(ctx, b, first)
		if err != nil {
			return result, err
		}
		if detail.NativeID != first {
			return result, provider.InvalidResponse()
		}
		result.DetailChecked = true
	}
	other, err := provider.NewBinding(b.Workspace(), b.Connection(), b.Family(), b.Version()+1)
	if err != nil {
		return result, err
	}
	if _, err = r.Page(ctx, other, ""); !errors.Is(err, provider.ErrBinding) {
		return result, provider.ErrBinding
	}
	result.BindingIsolated = true
	account, capabilities, err := r.Validate(ctx, c)
	if err != nil {
		return result, err
	}
	if account == "" || c.ContainsSecret(account) {
		return result, provider.ErrIdentityUnverified
	}
	for _, capability := range capabilities {
		if capability == "compute.server.read" {
			result.IdentityVerified = true
		}
	}
	if !result.IdentityVerified {
		return result, provider.ErrIdentityUnverified
	}
	// Denied-scope qualification needs a separately supplied disposable token.
	return result, nil
}
