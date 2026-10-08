package connection

import (
	"encoding/json"
	"errors"
	"strings"

	"sama/backend/internal/platform/credentials"
)

var ErrCredential = errors.New("invalid typed credential")

// Credential is a versioned write-only schema. Add separate typed variants when
// an adapter requires them; never accept arbitrary maps or ambient identities.
type Credential struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

func (c Credential) Validate() error {
	if c.Type != "bearer_v1" || len(c.Token) == 0 || len(c.Token) > 4096 || strings.ContainsAny(c.Token, "\r\n\x00") {
		return ErrCredential
	}
	return nil
}

func EncryptCredential(ring *credentials.Keyring, binding credentials.Binding, input Credential) (credentials.Envelope, error) {
	if err := input.Validate(); err != nil {
		return credentials.Envelope{}, err
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return credentials.Envelope{}, ErrCredential
	}
	defer clear(payload)
	return ring.Encrypt(binding, payload)
}
