// Package audit owns safe, append-only application audit event persistence.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"sama/backend/internal/identifier"
)

type ActorKind string

const (
	ActorUser         ActorKind = "user"
	ActorSystem       ActorKind = "system"
	ActorInstallation ActorKind = "installation"
)

type Event struct {
	ID        uuid.UUID
	Workspace uuid.UUID
	ActorKind ActorKind
	ActorID   *uuid.UUID
	Type      string
	RequestID string
	Metadata  map[string]string
}

var safeCode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var metadataFields = map[string]map[string]bool{
	"workspace.created":             {},
	"membership.changed":            {"member_id": true, "role": true},
	"connection.created":            {"connection_id": true},
	"connection.credential_rotated": {"connection_id": true},
	"connection.disabled":           {"connection_id": true},
	"operation.accepted":            {"operation_id": true, "resource_id": true},
	"operation.completed":           {"operation_id": true, "resource_id": true},
	"operation.failed":              {"operation_id": true, "resource_id": true, "reason_code": true},
	"operation.reconciled":          {"operation_id": true, "resource_id": true, "reason_code": true},
}

// NewID must be called before opening a multi-row transaction that will append
// this event, so the identifier is available to all rows in that transaction.
func NewID() (uuid.UUID, error) { return identifier.New() }

// Append validates allowlisted fields and writes through the caller's
// transaction. It never stores raw request/provider payloads or returns driver
// errors that could reveal database configuration.
func Append(ctx context.Context, tx pgx.Tx, event Event) error {
	metadata, err := validate(event)
	if err != nil {
		return err
	}
	actorID := ""
	if event.ActorID != nil {
		actorID = event.ActorID.String()
	}
	_, err = tx.Exec(ctx, `INSERT INTO public.audit_events
		(id, workspace_id, actor_kind, actor_id, event_type, request_id, metadata)
		VALUES ($1::uuid, $2::uuid, $3, NULLIF($4, '')::uuid, $5, $6, $7::jsonb)`,
		event.ID.String(), event.Workspace.String(), string(event.ActorKind), actorID,
		event.Type, event.RequestID, string(metadata),
	)
	if err != nil {
		return errors.New("audit event could not be appended")
	}
	return nil
}

func validate(event Event) ([]byte, error) {
	if !isEntityID(event.ID) || !isEntityID(event.Workspace) || len(event.RequestID) > 64 || !safeCode.MatchString(event.RequestID) {
		return nil, errors.New("invalid audit event")
	}
	switch event.ActorKind {
	case ActorUser:
		if event.ActorID == nil || !isEntityID(*event.ActorID) {
			return nil, errors.New("invalid audit event")
		}
	case ActorSystem, ActorInstallation:
		if event.ActorID != nil {
			return nil, errors.New("invalid audit event")
		}
	default:
		return nil, errors.New("invalid audit event")
	}
	allowed, ok := metadataFields[event.Type]
	if !ok || len(event.Metadata) > 8 {
		return nil, errors.New("invalid audit event")
	}
	for key, value := range event.Metadata {
		if !allowed[key] || len(value) == 0 || len(value) > 64 {
			return nil, errors.New("invalid audit event metadata")
		}
		switch key {
		case "member_id", "connection_id", "operation_id", "resource_id":
			if _, err := identifier.Parse(value); err != nil {
				return nil, errors.New("invalid audit event metadata")
			}
		case "role":
			if value != "viewer" && value != "operator" && value != "admin" && value != "owner" {
				return nil, errors.New("invalid audit event metadata")
			}
		case "reason_code":
			if !safeCode.MatchString(value) {
				return nil, errors.New("invalid audit event metadata")
			}
		}
	}
	if event.Metadata == nil {
		event.Metadata = map[string]string{}
	}
	encoded, err := json.Marshal(event.Metadata)
	if err != nil {
		return nil, errors.New("invalid audit event metadata")
	}
	return encoded, nil
}

func isEntityID(value uuid.UUID) bool {
	_, err := identifier.Parse(value.String())
	return err == nil
}
