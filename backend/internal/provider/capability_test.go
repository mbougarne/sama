package provider

import (
	"github.com/google/uuid"
	"testing"
)

func TestCapabilitiesAndImmutableBinding(t *testing.T) {
	workspace, _ := uuid.NewV7()
	connection, _ := uuid.NewV7()
	binding, err := NewBinding(workspace, connection, "fixture/compute", 3)
	if err != nil || binding.Workspace() != workspace || binding.Connection() != connection || binding.Version() != 3 {
		t.Fatal("binding", err)
	}
	if _, err := NewBinding(uuid.Nil, connection, "", 0); err != ErrBinding {
		t.Fatal("fallback binding")
	}
	catalog := []Support{{"read", "read"}}
	for _, test := range []struct {
		implemented, verified, granted, state []string
		reason                                string
	}{
		{nil, nil, nil, nil, "unsupported"},
		{[]string{"read"}, nil, nil, nil, "unverified"},
		{[]string{"read"}, []string{"read"}, nil, nil, "denied"},
		{[]string{"read"}, []string{"read"}, []string{"read"}, nil, "state_restricted"},
		{[]string{"read"}, []string{"read"}, []string{"read"}, []string{"read"}, "available"},
	} {
		got := Effective(catalog, test.implemented, test.verified, test.granted, test.state)
		if len(got) != 1 || got[0].Reason != test.reason || got[0].Available != (test.reason == "available") {
			t.Fatal(got)
		}
	}
	if NormalizeStatus("surprise", map[string]string{"known": "running"}) != "unknown" || NormalizeStatus("known", map[string]string{"known": "success"}) != "unknown" {
		t.Fatal("unknown inferred as success")
	}
}
