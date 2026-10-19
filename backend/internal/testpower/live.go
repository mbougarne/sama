// Package testpower provides explicit, opt-in live acceptance cases only.
// It is imported by tests, never by the production composition root.
package testpower

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/google/uuid"
	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
	"sama/backend/internal/provider/qualification"
)

type Factory func(provider.Binding, connection.Credential, *outbound.Budget) (qualification.PowerClient, error)

type Input struct {
	Workspace  uuid.UUID               `json:"workspace_id"`
	Connection uuid.UUID               `json:"connection_id"`
	Family     string                  `json:"family"`
	Version    int64                   `json:"credential_version"`
	Credential connection.Credential   `json:"credential"`
	Case       qualification.PowerCase `json:"case"`
}

// Load reads only an explicit regular, private file. It never uses an ambient
// provider identity or prints input/provider errors, even when parsing fails.
func Load(path string) (Input, error) {
	var in Input
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 || before.Size() > 32768 {
		return in, provider.ErrBinding
	}
	f, err := os.Open(path)
	if err != nil {
		return in, provider.ErrBinding
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || after.Mode().Perm()&0077 != 0 {
		return in, provider.ErrBinding
	}
	d := json.NewDecoder(io.LimitReader(f, 32769))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF || in.Credential.Validate() != nil {
		return Input{}, provider.ErrBinding
	}
	return in, nil
}

// Live requires an exact family/action opt-in plus an explicit private input.
// One invocation authorizes only this target/action and its declared restore.
func Live(t *testing.T, family, action string, factory Factory) {
	t.Helper()
	if os.Getenv("SAMA_POWER_PROBE_AUTHORIZATION") != family+":"+action {
		t.Skip("live provider action unverified; exact opt-in absent")
	}
	path := os.Getenv("SAMA_POWER_PROBE_INPUT_FILE")
	if path == "" {
		t.Fatal("explicit private power probe input required")
	}
	in, err := Load(path)
	if err != nil || in.Family != family || in.Case.Action != action {
		t.Fatal("power probe input rejected")
	}
	b, err := provider.NewBinding(in.Workspace, in.Connection, in.Family, in.Version)
	if err != nil {
		t.Fatal("power probe binding rejected")
	}
	client, err := factory(b, in.Credential, outbound.NewBudget())
	if err != nil {
		t.Fatal("power probe adapter unavailable")
	}
	evidence, err := qualification.PowerProbe(context.Background(), b, client, in.Credential, in.Case)
	// Evidence contains allowlisted booleans and the fixed selected action only.
	encoded, _ := json.Marshal(evidence)
	t.Log(string(encoded))
	if err != nil {
		t.Fatal("live power acceptance incomplete; production qualification unchanged")
	}
}
