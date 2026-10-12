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
	Type         string `json:"type"`
	Token        string `json:"token,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
}

func validSecret(value string) bool {
	return len(value) > 0 && len(value) <= 4096 && !strings.ContainsAny(value, "\r\n\x00")
}

func (c Credential) Validate() error {
	switch c.Type {
	case "bearer_v1":
		if !validSecret(c.Token) || c.ClientID != "" || c.ClientSecret != "" || c.Username != "" || c.Password != "" {
			return ErrCredential
		}
	case "contabo_v1":
		if c.Token != "" || !validSecret(c.ClientID) || !validSecret(c.ClientSecret) || !validSecret(c.Username) || !validSecret(c.Password) {
			return ErrCredential
		}
	default:
		return ErrCredential
	}
	return nil
}

func (c Credential) ContainsSecret(text string) bool {
	for _, value := range []string{c.Token, c.ClientID, c.ClientSecret, c.Username, c.Password} {
		if value != "" && strings.Contains(text, value) {
			return true
		}
	}
	return false
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
