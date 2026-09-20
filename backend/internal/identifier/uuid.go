// Package identifier owns public Sama entity identifiers. These values are
// identifiers, never session, CSRF, invitation, or other authentication secrets.
package identifier

import (
	"errors"

	"github.com/google/uuid"
)

var ErrInvalid = errors.New("invalid identifier")

// New creates an RFC 9562 UUIDv7 suitable for a Sama entity identifier.
func New() (uuid.UUID, error) {
	value, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, errors.New("identifier generation failed")
	}
	return value, nil
}

// Parse accepts only the canonical lowercase, hyphenated public UUID form.
func Parse(value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value || parsed.Version() != 7 || parsed.Variant() != uuid.RFC4122 {
		return uuid.Nil, ErrInvalid
	}
	return parsed, nil
}
