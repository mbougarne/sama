// Explicit operator-run read-only qualification candidate. It is not a startup
// path and cannot enable an adapter, save credentials, or provision resources.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
	"sama/backend/internal/provider/contabo"
	"sama/backend/internal/provider/digitalocean"
	"sama/backend/internal/provider/hetzner"
	"sama/backend/internal/provider/qualification"
	"sama/backend/internal/provider/vultr"
)

type input struct {
	Workspace        uuid.UUID             `json:"workspace_id"`
	Connection       uuid.UUID             `json:"connection_id"`
	Family           string                `json:"family"`
	Version          int64                 `json:"credential_version"`
	Credential       connection.Credential `json:"credential"`
	DeniedCredential connection.Credential `json:"denied_credential"`
}

func load(path string) (input, error) {
	var value input
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 32768 {
		return value, provider.ErrBinding
	}
	file, err := os.Open(path)
	if err != nil {
		return value, provider.ErrBinding
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 32769))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.Credential.Validate() != nil || value.DeniedCredential.Validate() != nil {
		return input{}, provider.ErrBinding
	}
	return value, nil
}

func makeReader(b provider.Binding, c connection.Credential, budget *outbound.Budget) (qualification.Reader, error) {
	switch b.Family() {
	case hetzner.Family:
		return hetzner.New(b, c, budget)
	case digitalocean.Family:
		return digitalocean.New(b, c, budget)
	case vultr.Family:
		return vultr.New(b, c, budget)
	case contabo.Family:
		return contabo.New(b, c, budget)
	default:
		return nil, provider.ErrBinding
	}
}

func run(ctx context.Context, value input) (qualification.Evidence, error) {
	b, err := provider.NewBinding(value.Workspace, value.Connection, value.Family, value.Version)
	if err != nil {
		return qualification.Evidence{}, err
	}
	budget := outbound.NewBudget()
	reader, err := makeReader(b, value.Credential, budget)
	if err != nil {
		return qualification.Evidence{}, err
	}
	evidence, probeErr := qualification.Probe(ctx, b, value.Credential, reader)
	deniedReader, err := makeReader(b, value.DeniedCredential, budget)
	if err != nil {
		return evidence, err
	}
	evidence.DenialChecked = qualification.Denied(ctx, b, deniedReader)
	if !evidence.DenialChecked {
		return evidence, provider.ErrIdentityUnverified
	}
	return evidence, probeErr
}

func main() {
	live := flag.Bool("live-read-only", false, "explicitly authorize read-only requests to the selected provider")
	path := flag.String("input-file", "", "private explicit JSON credential/scope file; no ambient credentials")
	flag.Parse()
	if !*live || *path == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "explicit private input and --live-read-only required; live qualification unverified")
		os.Exit(2)
	}
	value, err := load(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "provider probe input rejected")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	evidence, err := run(ctx, value)
	// Only aggregate evidence is emitted, never input, provider bodies or errors.
	if json.NewEncoder(os.Stdout).Encode(evidence) != nil {
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "provider qualification incomplete; production registration remains closed")
		os.Exit(1)
	}
}
