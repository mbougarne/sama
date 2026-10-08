package credentials

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
)

func TestEnvelopeBindingTamperingAndFreshness(t *testing.T) {
	workspace, _ := uuid.NewV7()
	connection, _ := uuid.NewV7()
	binding := Binding{workspace, connection, "fixture/compute", 1}
	master := bytes.Repeat([]byte{3}, 32)
	payload := []byte(`{"token":"synthetic-secret-sentinel"}`)
	first, err := Encrypt(binding, payload, "key-1", master)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encrypt(binding, payload, "key-1", master)
	if err != nil || bytes.Equal(first.Ciphertext, second.Ciphertext) || bytes.Equal(first.Nonce, second.Nonce) || bytes.Equal(first.WrappedKey, second.WrappedKey) || bytes.Equal(first.WrappingNonce, second.WrappingNonce) || bytes.Equal(first.Nonce, first.WrappingNonce) {
		t.Fatal("randomness reused", err)
	}
	plain, err := Decrypt(binding, first, map[string][]byte{"key-1": master})
	if err != nil || !bytes.Equal(plain, payload) {
		t.Fatal("round trip", err)
	}
	changed := []Binding{binding, binding, binding, binding}
	changed[0].Workspace, _ = uuid.NewV7()
	changed[1].Connection, _ = uuid.NewV7()
	changed[2].Family = "other"
	changed[3].Version++
	for _, b := range changed {
		if _, err := Decrypt(b, first, map[string][]byte{"key-1": master}); err != ErrEnvelope {
			t.Fatal("binding substitution")
		}
	}
	for _, field := range []string{"ciphertext", "nonce", "wrapped", "wrapping", "format"} {
		e := first
		e.Ciphertext = bytes.Clone(first.Ciphertext)
		e.Nonce = bytes.Clone(first.Nonce)
		e.WrappedKey = bytes.Clone(first.WrappedKey)
		e.WrappingNonce = bytes.Clone(first.WrappingNonce)
		switch field {
		case "ciphertext":
			e.Ciphertext[0] ^= 1
		case "nonce":
			e.Nonce[0] ^= 1
		case "wrapped":
			e.WrappedKey[0] ^= 1
		case "wrapping":
			e.WrappingNonce[0] ^= 1
		case "format":
			e.Format++
		}
		if _, err := Decrypt(binding, e, map[string][]byte{"key-1": master}); err != ErrEnvelope {
			t.Fatal(field)
		}
	}
	for _, keys := range []map[string][]byte{nil, {"key-1": bytes.Repeat([]byte{9}, 32)}, {"key-1": {1}}} {
		if _, err := Decrypt(binding, first, keys); err != ErrEnvelope {
			t.Fatal("missing/wrong key")
		}
	}
	for _, e := range []Envelope{{}, {Format: 1, WrappedKey: make([]byte, 48)}} {
		if _, err := Decrypt(binding, e, nil); err != ErrEnvelope {
			t.Fatal("malformed envelope")
		}
	}
}
