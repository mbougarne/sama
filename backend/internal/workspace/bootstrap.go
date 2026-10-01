// Package workspace owns workspace lifecycle and membership policy.
package workspace

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/audit"
	"sama/backend/internal/identifier"
	"sama/backend/internal/platform"
)

var ErrStore = errors.New("workspace storage unavailable")
var ErrBootstrapExists = errors.New("installation already initialized or bootstrap busy")
var ErrInvalid = errors.New("invalid workspace input")

func ValidName(name string) bool {
	if name != strings.TrimSpace(name) || !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// Bootstrap is a local-only installation operation. It never replaces an owner
// and creates identity, workspace, membership and audit in one transaction.
func Bootstrap(ctx context.Context, pool *pgxpool.Pool, configuredIssuer, issuer, subject, name string) (uuid.UUID, error) {
	if configuredIssuer == "" || issuer != configuredIssuer || strings.TrimSpace(subject) == "" || len(subject) > 255 || !ValidName(name) {
		return uuid.Nil, ErrInvalid
	}
	user, err := identifier.New()
	if err != nil {
		return uuid.Nil, ErrStore
	}
	workspace, err := identifier.New()
	if err != nil {
		return uuid.Nil, ErrStore
	}
	event, err := audit.NewID()
	if err != nil {
		return uuid.Nil, ErrStore
	}
	tx, err := platform.BeginTx(ctx, pool, pgx.TxOptions{})
	if err != nil {
		return uuid.Nil, ErrStore
	}
	defer tx.Rollback(ctx)
	var locked, initialized bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(891016)`).Scan(&locked); err != nil {
		return uuid.Nil, ErrStore
	}
	if !locked {
		return uuid.Nil, ErrBootstrapExists
	}
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM installation_bootstrap) OR EXISTS(SELECT 1 FROM workspaces)`).Scan(&initialized); err != nil {
		return uuid.Nil, ErrStore
	}
	if initialized {
		return uuid.Nil, ErrBootstrapExists
	}
	if _, err := tx.Exec(ctx, `INSERT INTO users(id,issuer,subject) VALUES($1,$2,$3) ON CONFLICT(issuer,subject) DO NOTHING`, user, issuer, subject); err != nil {
		return uuid.Nil, ErrStore
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE issuer=$1 AND subject=$2 AND disabled_at IS NULL FOR UPDATE`, issuer, subject).Scan(&user); err != nil {
		return uuid.Nil, ErrStore
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workspaces(id,name) VALUES($1,$2)`, workspace, name); err != nil {
		return uuid.Nil, ErrStore
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspace, user); err != nil {
		return uuid.Nil, ErrStore
	}
	if _, err := tx.Exec(ctx, `INSERT INTO installation_bootstrap(singleton,user_id,workspace_id) VALUES(true,$1,$2)`, user, workspace); err != nil {
		return uuid.Nil, ErrStore
	}
	if err := audit.Append(ctx, tx, audit.Event{ID: event, Workspace: workspace, ActorKind: audit.ActorInstallation, Type: "workspace.created", RequestID: "installation-bootstrap"}); err != nil {
		return uuid.Nil, ErrStore
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, ErrStore
	}
	return workspace, nil
}
