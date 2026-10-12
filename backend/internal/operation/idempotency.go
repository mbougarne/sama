package operation

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Accepted struct {
	ID    uuid.UUID `json:"id"`
	State string    `json:"state"`
}

func ValidKey(key string) bool {
	if len(key) == 0 || len(key) > 128 {
		return false
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r)) {
			return false
		}
	}
	return true
}

// Resolve precedes expiry/consumption rejection. The database's unique
// workspace+actor key is authoritative; current read grants still filter access.
func Resolve(ctx context.Context, tx pgx.Tx, scope, actor uuid.UUID, key string, hash [32]byte) (Accepted, bool, error) {
	if !ValidKey(key) {
		return Accepted{}, false, ErrInvalid
	}
	var result Accepted
	var stored []byte
	err := tx.QueryRow(ctx, `SELECT o.id,o.state,o.intent_hash FROM operations o
 JOIN memberships m ON m.workspace_id=o.workspace_id AND m.user_id=$2
 JOIN users u ON u.id=m.user_id AND u.disabled_at IS NULL
 JOIN workspaces w ON w.id=o.workspace_id AND w.closed_at IS NULL
 JOIN connection_grants g ON (g.workspace_id,g.connection_id,g.user_id)=(o.workspace_id,o.connection_id,m.user_id)
 WHERE o.workspace_id=$1 AND o.actor_id=$2 AND o.idempotency_key=$3 AND 'read'=ANY(g.actions)`, scope, actor, key).Scan(&result.ID, &result.State, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return Accepted{}, false, nil
	}
	if err != nil {
		return Accepted{}, false, ErrStore
	}
	if subtle.ConstantTimeCompare(stored, hash[:]) != 1 {
		return Accepted{}, false, ErrConflict
	}
	return result, true, nil
}
