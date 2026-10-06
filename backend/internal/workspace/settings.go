package workspace

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/audit"
	"sama/backend/internal/platform"
	"strconv"
)

// Settings are workspace policy, not authority to change process-wide budgets.
type Settings struct {
	Name               string `json:"name"`
	PolicyVersion      int64  `json:"policy_version"`
	QueueLimit         int    `json:"queue_limit"`
	AuditRetentionDays int    `json:"audit_retention_days"`
}

var ErrPolicyConflict = errors.New("workspace policy version conflict")

func WorkspaceSettings(ctx context.Context, pool *pgxpool.Pool, actor, scope uuid.UUID, update *Settings, request string) (Settings, error) {
	tx, err := platform.BeginTx(ctx, pool, pgx.TxOptions{})
	if err != nil {
		return Settings{}, ErrStore
	}
	defer tx.Rollback(ctx)
	member, err := LockAuthority(ctx, tx, actor, scope)
	if err != nil {
		return Settings{}, err
	}
	if member.Role != "owner" {
		return Settings{}, ErrDenied
	}
	var current Settings
	if err := tx.QueryRow(ctx, `SELECT name,policy_version,queue_limit,audit_retention_days FROM workspaces WHERE id=$1`, scope).Scan(&current.Name, &current.PolicyVersion, &current.QueueLimit, &current.AuditRetentionDays); err != nil {
		return Settings{}, ErrStore
	}
	if update == nil {
		return current, nil
	}
	if !ValidName(update.Name) || update.QueueLimit < 1 || update.QueueLimit > 1000 || update.AuditRetentionDays < 180 || update.AuditRetentionDays > 3650 {
		return Settings{}, ErrInvalid
	}
	if update.PolicyVersion != current.PolicyVersion {
		return Settings{}, ErrPolicyConflict
	}
	if *update == current {
		return current, nil
	}
	current = *update
	current.PolicyVersion++
	if _, err := tx.Exec(ctx, `UPDATE workspaces SET name=$2,policy_version=$3,queue_limit=$4,audit_retention_days=$5 WHERE id=$1`, scope, current.Name, current.PolicyVersion, current.QueueLimit, current.AuditRetentionDays); err != nil {
		return Settings{}, ErrStore
	}
	event, err := audit.NewID()
	if err != nil {
		return Settings{}, ErrStore
	}
	if err := audit.Append(ctx, tx, audit.Event{ID: event, Workspace: scope, ActorKind: audit.ActorUser, ActorID: &actor, Type: "workspace.settings_changed", RequestID: request, Metadata: map[string]string{"policy_version": strconv.FormatInt(current.PolicyVersion, 10)}}); err != nil {
		return Settings{}, ErrStore
	}
	if tx.Commit(ctx) != nil {
		return Settings{}, ErrStore
	}
	return current, nil
}
