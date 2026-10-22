package connection

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	"sama/backend/internal/platform/credentials"
)

var ErrCredential = errors.New("invalid typed credential")

// Credential is a versioned write-only schema. Add separate typed variants when
// an adapter requires them; never accept arbitrary maps or ambient identities.
type Credential struct {
	Type           string `json:"type"`
	Token          string `json:"token,omitempty"`
	ClientID       string `json:"client_id,omitempty"`
	ClientSecret   string `json:"client_secret,omitempty"`
	Username       string `json:"username,omitempty"`
	Password       string `json:"password,omitempty"`
	CustomerNumber string `json:"customer_number,omitempty"`
	APIKey         string `json:"api_key,omitempty"`
	APIPassword    string `json:"api_password,omitempty"`
}

var customerNumber = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

func validSecret(value string) bool {
	return len(value) > 0 && len(value) <= 4096 && !strings.ContainsAny(value, "\r\n\x00")
}

func (c Credential) Validate() error {
	legacyFields := c.CustomerNumber != "" || c.APIKey != "" || c.APIPassword != ""
	switch c.Type {
	case "bearer_v1":
		if legacyFields || !validSecret(c.Token) || c.ClientID != "" || c.ClientSecret != "" || c.Username != "" || c.Password != "" {
			return ErrCredential
		}
	case "contabo_v1":
		if legacyFields || c.Token != "" || !validSecret(c.ClientID) || !validSecret(c.ClientSecret) || !validSecret(c.Username) || !validSecret(c.Password) {
			return ErrCredential
		}
	case "netcup_legacy_v1":
		if !customerNumber.MatchString(c.CustomerNumber) || !validSecret(c.APIKey) || !validSecret(c.APIPassword) || c.Token != "" || c.ClientID != "" || c.ClientSecret != "" || c.Username != "" || c.Password != "" {
			return ErrCredential
		}
	default:
		return ErrCredential
	}
	return nil
}

func (c Credential) ContainsSecret(text string) bool {
	for _, value := range []string{c.Token, c.ClientID, c.ClientSecret, c.Username, c.Password, c.APIKey, c.APIPassword} {
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

// DecryptCredential authenticates the exact immutable envelope binding before
// decoding its typed payload. Unknown/mixed schemas never become adapter input.
func DecryptCredential(ring *credentials.Keyring, binding credentials.Binding, envelope credentials.Envelope) (Credential, error) {
	payload, err := ring.Decrypt(binding, envelope)
	if err != nil {
		return Credential{}, ErrCredential
	}
	defer clear(payload)
	var result Credential
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.Validate() != nil {
		return Credential{}, ErrCredential
	}
	return result, nil
}
