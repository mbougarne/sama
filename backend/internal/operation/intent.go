package operation

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io"

	"github.com/google/uuid"
)

type PowerParameters struct{}

type Intent struct {
	Action     string           `json:"action"`
	Parameters *PowerParameters `json:"parameters"`
}

func (i Intent) Validate() error {
	if i.Parameters == nil {
		return ErrInvalid
	}
	switch i.Action {
	case "compute.server.power_on", "compute.server.shutdown", "compute.server.power_off", "compute.server.reboot":
		return nil
	default:
		return ErrInvalid
	}
}

func DecodeIntent(data []byte) (Intent, error) {
	if len(data) > 1024 {
		return Intent{}, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var i Intent
	if decoder.Decode(&i) != nil || decoder.Decode(new(any)) != io.EOF || i.Validate() != nil {
		return Intent{}, ErrInvalid
	}
	return i, nil
}

// Digest binds canonical typed parameters to all server-owned scope identities.
// Credential material is neither an intent field nor a hash input.
func Digest(i Intent, workspace, actor, connection, target, review uuid.UUID) ([32]byte, error) {
	if i.Validate() != nil {
		return [32]byte{}, ErrInvalid
	}
	for _, id := range []uuid.UUID{workspace, actor, connection, target, review} {
		if id.Version() != 7 {
			return [32]byte{}, ErrInvalid
		}
	}
	data, err := json.Marshal(struct {
		Workspace  uuid.UUID
		Actor      uuid.UUID
		Connection uuid.UUID
		Target     uuid.UUID
		Review     uuid.UUID
		Intent     Intent
	}{workspace, actor, connection, target, review, i})
	if err != nil {
		return [32]byte{}, ErrInvalid
	}
	return sha256.Sum256(data), nil
}
