package qualification

import (
	"context"
	"github.com/google/uuid"
	"sama/backend/internal/connection"
	"sama/backend/internal/provider"
	"testing"
)

type reader struct {
	binding provider.Binding
	pages   int
	fail    bool
}

func (r *reader) Page(_ context.Context, b provider.Binding, cursor string) (provider.InventoryPage, error) {
	if b != r.binding {
		return provider.InventoryPage{}, provider.ErrBinding
	}
	r.pages++
	if cursor == "last" {
		if r.fail {
			return provider.InventoryPage{}, provider.InvalidResponse()
		}
		return provider.InventoryPage{}, nil
	}
	return provider.InventoryPage{Data: []provider.Observation{{NativeID: "42"}}, Next: "last"}, nil
}

func (r *reader) Read(_ context.Context, b provider.Binding, id string) (provider.Observation, error) {
	if b != r.binding {
		return provider.Observation{}, provider.ErrBinding
	}
	return provider.Observation{NativeID: id}, nil
}

func (r *reader) Validate(context.Context, connection.Credential) (string, []string, error) {
	return "", nil, provider.ErrIdentityUnverified
}

func TestReadEvidenceNeverInventsIdentityQualification(t *testing.T) {
	w, _ := uuid.NewV7()
	c, _ := uuid.NewV7()
	b, _ := provider.NewBinding(w, c, "synthetic", 1)
	r := &reader{binding: b}
	evidence, err := Probe(context.Background(), b, connection.Credential{Type: "bearer_v1", Token: "synthetic-only"}, r)
	if err != provider.ErrIdentityUnverified || !evidence.ListComplete || !evidence.DetailChecked || !evidence.BindingIsolated || evidence.IdentityVerified || evidence.DenialChecked || evidence.Pages != 2 {
		t.Fatal(evidence, err)
	}
	r = &reader{binding: b, fail: true}
	evidence, err = Probe(context.Background(), b, connection.Credential{Type: "bearer_v1", Token: "synthetic-only"}, r)
	if err == nil || evidence.ListComplete || evidence.Pages != 1 {
		t.Fatal("partial evidence claimed completion", evidence, err)
	}
}
