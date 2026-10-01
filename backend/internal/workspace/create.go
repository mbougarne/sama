package workspace

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/audit"
	"sama/backend/internal/identifier"
	"sama/backend/internal/platform"
)

var ErrLimit = errors.New("workspace creation limit reached")

const MaxOwnedWorkspaces = 10

func Create(ctx context.Context, pool *pgxpool.Pool, user uuid.UUID, name, requestID string) (View, error) {
	if !ValidName(name) {
		return View{}, ErrInvalid
	}
	id, err := identifier.New()
	if err != nil {
		return View{}, ErrStore
	}
	event, err := audit.NewID()
	if err != nil {
		return View{}, ErrStore
	}
	tx, err := platform.BeginTx(ctx, pool, pgx.TxOptions{})
	if err != nil {
		return View{}, ErrStore
	}
	defer tx.Rollback(ctx)
	var admitted uuid.UUID
	// Lock the actor to serialize the quota check with concurrent creations.
	err = tx.QueryRow(ctx, `SELECT u.id FROM users u WHERE u.id=$1 AND u.disabled_at IS NULL
 AND EXISTS(SELECT 1 FROM memberships m JOIN workspaces w ON w.id=m.workspace_id WHERE m.user_id=u.id AND w.closed_at IS NULL)
 FOR UPDATE OF u`, user).Scan(&admitted)
	if errors.Is(err, pgx.ErrNoRows) {
		return View{}, ErrDenied
	}
	if err != nil {
		return View{}, ErrStore
	}
	var owned int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM memberships WHERE user_id=$1 AND role='owner'`, user).Scan(&owned); err != nil {
		return View{}, ErrStore
	}
	if owned >= MaxOwnedWorkspaces {
		return View{}, ErrLimit
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workspaces(id,name) VALUES($1,$2)`, id, name); err != nil {
		return View{}, ErrStore
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,'owner')`, id, user); err != nil {
		return View{}, ErrStore
	}
	if err := audit.Append(ctx, tx, audit.Event{ID: event, Workspace: id, ActorKind: audit.ActorUser, ActorID: &user, Type: "workspace.created", RequestID: requestID}); err != nil {
		return View{}, ErrStore
	}
	if err := tx.Commit(ctx); err != nil {
		return View{}, ErrStore
	}
	return View{ID: id, Name: name, Role: "owner"}, nil
}
