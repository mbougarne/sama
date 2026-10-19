package testpower

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRequiresPrivateExplicitTypedInput(t *testing.T) {
	dir := t.TempDir()
	valid := `{"workspace_id":"01994d83-8123-7123-8123-123456789abc","connection_id":"01994d83-8123-7123-8123-123456789abd","family":"hetzner.cloud","credential_version":1,"credential":{"type":"bearer_v1","token":"synthetic-only"},"case":{"action":"compute.server.power_on","native_id":"42","disposable":true,"spend_ceiling":"explicit ceiling","disruption_seconds":60,"restore_action":"compute.server.shutdown"}}`
	path := filepath.Join(dir, "case.json")
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	in, err := Load(path)
	if err != nil || in.Case.NativeID != "42" || in.Family != "hetzner.cloud" {
		t.Fatal("private typed input rejected")
	}
	for name, body := range map[string]string{"unknown.json": `{"unknown":true}`, "trailing.json": valid + `{}`, "large.json": string(make([]byte, 32769))} {
		candidate := filepath.Join(dir, name)
		if os.WriteFile(candidate, []byte(body), 0600) != nil {
			t.Fatal("fixture")
		}
		if _, err := Load(candidate); err == nil {
			t.Fatal("unsafe input accepted", name)
		}
	}
	public := filepath.Join(dir, "public.json")
	if os.WriteFile(public, []byte(valid), 0644) != nil {
		t.Fatal("fixture")
	}
	if _, err := Load(public); err == nil {
		t.Fatal("shared credential input accepted")
	}
	link := filepath.Join(dir, "link.json")
	if os.Symlink(path, link) != nil {
		t.Fatal("fixture")
	}
	if _, err := Load(link); err == nil {
		t.Fatal("symlink input accepted")
	}
}
