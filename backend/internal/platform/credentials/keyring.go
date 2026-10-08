package credentials

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
)

var ErrKeyring = errors.New("credential keyring unavailable")

// Keyring owns a private immutable copy of mounted key material.
type Keyring struct {
	active string
	keys   map[string][]byte
}

// LoadKeyring requires a regular 0600 mounted file, bounded before decoding.
// Array entries make duplicate key IDs detectable without map overwrite semantics.
func LoadKeyring(path string) (*Keyring, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, ErrKeyring
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrKeyring
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Mode().Perm() != 0600 {
		return nil, ErrKeyring
	}
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(data) > 65536 {
		return nil, ErrKeyring
	}
	defer clear(data)
	var input struct {
		Active string `json:"active"`
		Keys   []struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		} `json:"keys"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || len(input.Keys) == 0 || len(input.Keys) > 64 {
		return nil, ErrKeyring
	}
	// Reject duplicate object properties as well as duplicate key IDs.
	if !uniqueJSONProperties(data) {
		return nil, ErrKeyring
	}
	ring := &Keyring{active: input.Active, keys: map[string][]byte{}}
	for _, entry := range input.Keys {
		key, err := base64.StdEncoding.Strict().DecodeString(entry.Key)
		if err != nil || len(key) != 32 || !validKeyID(entry.ID) || ring.keys[entry.ID] != nil {
			return nil, ErrKeyring
		}
		ring.keys[entry.ID] = key
	}
	if ring.keys[ring.active] == nil {
		return nil, ErrKeyring
	}
	return ring, nil
}

func uniqueJSONProperties(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value func() bool
	value = func() bool {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !value() {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim('}')
		case json.Delim('['):
			for decoder.More() {
				if !value() {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim(']')
		}
		return true
	}
	return value()
}

func (k *Keyring) Require(ids []string) error {
	if k == nil || k.keys[k.active] == nil {
		return ErrKeyring
	}
	for _, id := range ids {
		if k.keys[id] == nil {
			return ErrKeyring
		}
	}
	return nil
}

func (k *Keyring) Encrypt(binding Binding, payload []byte) (Envelope, error) {
	if k.Require(nil) != nil {
		return Envelope{}, ErrKeyring
	}
	return Encrypt(binding, payload, k.active, k.keys[k.active])
}

func (k *Keyring) Decrypt(binding Binding, envelope Envelope) ([]byte, error) {
	if k.Require([]string{envelope.KeyID}) != nil {
		return nil, ErrKeyring
	}
	return Decrypt(binding, envelope, k.keys)
}
