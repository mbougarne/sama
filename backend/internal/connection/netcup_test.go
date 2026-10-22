package connection

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"sama/backend/internal/platform/credentials"
)

func TestLegacyCredentialCannotCrossVariants(t *testing.T) {
	c := Credential{Type: "netcup_legacy_v1", CustomerNumber: "123456", APIKey: "synthetic-key", APIPassword: "synthetic-password"}
	if c.Validate() != nil || !c.ContainsSecret("synthetic-key") || !c.ContainsSecret("synthetic-password") || c.ContainsSecret("netcup.customer/123456") {
		t.Fatal("typed identity/secret distinction")
	}
	for _, mutate := range []func(*Credential){func(c *Credential) { c.Type = "bearer_v1"; c.Token = "synthetic-token" }, func(c *Credential) { c.Token = "synthetic-token" }, func(c *Credential) { c.CustomerNumber = "../123456" }, func(c *Credential) { c.APIKey = "bad\nkey" }, func(c *Credential) { c.APIPassword = "" }} {
		bad := c
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("mixed or invalid variant")
		}
	}
}

func TestLegacyCredentialUsesSharedEnvelopeBinding(t *testing.T) {
	// Synthetic test-only key; no mounted/application credential is consulted.
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	path := filepath.Join(t.TempDir(), "synthetic-keyring")
	if err := os.WriteFile(path, []byte(`{"active":"fixture","keys":[{"id":"fixture","key":"`+key+`"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	ring, err := credentials.LoadKeyring(path)
	if err != nil {
		t.Fatal(err)
	}
	w, _ := uuid.NewV7()
	id, _ := uuid.NewV7()
	b := credentials.Binding{Workspace: w, Connection: id, Family: "netcup.legacy_dns", Version: 1}
	input := Credential{Type: "netcup_legacy_v1", CustomerNumber: "123456", APIKey: "synthetic-key", APIPassword: "synthetic-password"}
	envelope, err := EncryptCredential(ring, b, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{input.APIKey, input.APIPassword} {
		if bytes.Contains(envelope.Ciphertext, []byte(secret)) {
			t.Fatal("credential persisted as plaintext")
		}
	}
	restored, err := DecryptCredential(ring, b, envelope)
	if err != nil || restored != input {
		t.Fatal("typed credential changed through shared envelope")
	}
	otherWorkspace, _ := uuid.NewV7()
	otherConnection, _ := uuid.NewV7()
	for _, wrong := range []credentials.Binding{
		{Workspace: otherWorkspace, Connection: id, Family: b.Family, Version: 1},
		{Workspace: w, Connection: otherConnection, Family: b.Family, Version: 1},
		{Workspace: w, Connection: id, Family: "netcup.clouddns", Version: 1},
		{Workspace: w, Connection: id, Family: b.Family, Version: 2},
	} {
		if data, err := DecryptCredential(ring, wrong, envelope); err != ErrCredential || data != (Credential{}) {
			t.Fatal("envelope crossed workspace, connection, family or version")
		}
	}
	for _, payload := range []string{
		`{"type":"netcup_legacy_v1","customer_number":"123456","api_key":"synthetic-key","api_password":"synthetic-password","endpoint":"https://outside.test"}`,
		`{"type":"netcup_legacy_v1","customer_number":"123456","api_key":"synthetic-key","api_password":"synthetic-password","token":"mixed-token"}`,
		`{"type":"netcup_legacy_v1","customer_number":"123456","api_key":"synthetic-key","api_password":"synthetic-password"}{}`,
	} {
		bad, err := ring.Encrypt(b, []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if value, err := DecryptCredential(ring, b, bad); err != ErrCredential || value != (Credential{}) {
			t.Fatal("unrestricted encrypted schema became adapter input")
		}
	}
}
