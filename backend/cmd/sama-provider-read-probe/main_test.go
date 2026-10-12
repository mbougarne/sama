package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
	"sama/backend/internal/provider/hetzner"
)

func TestProbeInputRequiresExplicitPrivateTypedCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	valid := `{"workspace_id":"01994d83-8123-7123-8123-123456789abc","connection_id":"01994d83-8123-7123-8123-123456789abd","family":"hetzner.cloud","credential_version":1,"credential":{"type":"bearer_v1","token":"synthetic-only"},"denied_credential":{"type":"bearer_v1","token":"synthetic-denied"}}`
	if os.WriteFile(path, []byte(valid), 0600) != nil {
		t.Fatal("fixture write")
	}
	if _, err := load(path); err != nil {
		t.Fatal("valid explicit input", err)
	}
	if os.Chmod(path, 0644) != nil {
		t.Fatal("fixture mode")
	}
	if _, err := load(path); err == nil {
		t.Fatal("shared credential file accepted")
	}
	if os.Chmod(path, 0600) != nil {
		t.Fatal("fixture mode")
	}
	for _, body := range []string{`{}`, valid + `{}`, `{"credential":{"type":"ambient"}}`} {
		if os.WriteFile(path, []byte(body), 0600) != nil {
			t.Fatal("fixture write")
		}
		if _, err := load(path); err == nil {
			t.Fatal("missing/ambient/trailing input accepted")
		}
	}
	link := path + "-symlink"
	if os.Symlink(path, link) != nil {
		t.Fatal(errText())
	}
	if _, err := load(link); err == nil {
		t.Fatal("symlink credential file accepted")
	}
}

func errText() string { return "fixture symlink creation failed" }

func TestFactoryRejectsWrongFamilyCredentialAndNoAmbientFallback(t *testing.T) {
	w, _ := uuid.NewV7()
	id, _ := uuid.NewV7()
	b, _ := provider.NewBinding(w, id, hetzner.Family, 1)
	if _, err := makeReader(b, connection.Credential{Type: "contabo_v1", ClientID: "client", ClientSecret: "secret", Username: "user", Password: "password"}, outbound.NewBudget()); err == nil {
		t.Fatal("cross-family credential accepted")
	}
	b, _ = provider.NewBinding(w, id, "ambient", 1)
	if _, err := makeReader(b, connection.Credential{Type: "bearer_v1", Token: "synthetic"}, outbound.NewBudget()); err == nil {
		t.Fatal("ambient registry fallback")
	}
}
