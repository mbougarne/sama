package credentials

import (
	"bytes"
	"github.com/google/uuid"
	"testing"
)

func TestRewrapPreservesCiphertextAndIdentity(t *testing.T) {
	scope, _ := uuid.NewV7()
	connection, _ := uuid.NewV7()
	binding := Binding{scope, connection, "compute", 1}
	ring := &Keyring{active: "new", keys: map[string][]byte{"old": bytes.Repeat([]byte{1}, 32), "new": bytes.Repeat([]byte{2}, 32)}}
	original, err := Encrypt(binding, []byte("synthetic-credential"), "old", ring.keys["old"])
	if err != nil {
		t.Fatal(err)
	}
	next, err := ring.Rewrap(binding, original, "new")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original.Ciphertext, next.Ciphertext) || !bytes.Equal(original.Nonce, next.Nonce) || next.Format != original.Format || next.KeyID != "new" {
		t.Fatal("credential changed")
	}
	plain, err := Decrypt(binding, next, map[string][]byte{"new": ring.keys["new"]})
	if err != nil || string(plain) != "synthetic-credential" {
		t.Fatal("new envelope unavailable")
	}
	binding.Version++
	if _, err = ring.Rewrap(binding, original, "new"); err != ErrEnvelope {
		t.Fatal("binding tamper accepted")
	}
	if _, err = ring.Rewrap(binding, original, "missing"); err != ErrKeyring {
		t.Fatal("missing key accepted")
	}
}
