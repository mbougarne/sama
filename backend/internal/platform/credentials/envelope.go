// Package credentials supplies authenticated envelopes; it never logs material.
package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

var ErrEnvelope = errors.New("credential envelope unavailable")

// Binding is authenticated for both the payload and the wrapped data key.
type Binding struct {
	Workspace  uuid.UUID
	Connection uuid.UUID
	Family     string
	Version    int64
}

type Envelope struct {
	Ciphertext    []byte
	Nonce         []byte
	WrappedKey    []byte
	WrappingNonce []byte
	KeyID         string
	Format        int
}

func (b Binding) aad(purpose string) ([]byte, error) {
	if b.Workspace.Version() != 7 || b.Connection.Version() != 7 || b.Version < 1 || len(b.Family) == 0 || len(b.Family) > 128 {
		return nil, ErrEnvelope
	}
	return json.Marshal(struct {
		Format  int
		Purpose string
		Binding Binding
	}{1, purpose, b})
}

func gcm(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrEnvelope
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrEnvelope
	}
	return cipher.NewGCM(block)
}

func seal(key, plaintext, aad []byte) ([]byte, []byte, error) {
	aead, err := gcm(key)
	if err != nil {
		return nil, nil, ErrEnvelope
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, ErrEnvelope
	}
	return aead.Seal(nil, nonce, plaintext, aad), nonce, nil
}

func open(key, ciphertext, nonce, aad []byte) ([]byte, error) {
	aead, err := gcm(key)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, ErrEnvelope
	}
	plain, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrEnvelope
	}
	return plain, nil
}

// Encrypt accepts the owning adapter's serialized typed credential, never an
// unrestricted provider response. The caller retains responsibility for schema validation.
func Encrypt(binding Binding, payload []byte, keyID string, master []byte) (Envelope, error) {
	aad, err := binding.aad("payload")
	if err != nil || len(payload) == 0 || len(payload) > 65520 || !validKeyID(keyID) {
		return Envelope{}, ErrEnvelope
	}
	dataKey := make([]byte, 32)
	if _, err := rand.Read(dataKey); err != nil {
		return Envelope{}, ErrEnvelope
	}
	defer clear(dataKey)
	ciphertext, nonce, err := seal(dataKey, payload, aad)
	if err != nil {
		return Envelope{}, err
	}
	wrapAAD, _ := binding.aad("data-key")
	wrapped, wrappingNonce, err := seal(master, dataKey, wrapAAD)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{ciphertext, nonce, wrapped, wrappingNonce, keyID, 1}, nil
}

func Decrypt(binding Binding, envelope Envelope, keys map[string][]byte) ([]byte, error) {
	aad, err := binding.aad("payload")
	if err != nil || envelope.Format != 1 || len(envelope.Ciphertext) > 65536 || len(envelope.WrappedKey) != 48 {
		return nil, ErrEnvelope
	}
	wrapAAD, _ := binding.aad("data-key")
	dataKey, err := open(keys[envelope.KeyID], envelope.WrappedKey, envelope.WrappingNonce, wrapAAD)
	if err != nil {
		return nil, err
	}
	defer clear(dataKey)
	return open(dataKey, envelope.Ciphertext, envelope.Nonce, aad)
}

func validKeyID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
