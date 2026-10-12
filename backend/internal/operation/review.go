package operation

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/identity"
	"sama/backend/internal/platform"
	"sama/backend/internal/provider"
	"sama/backend/internal/workspace"
)

var ErrUnsupported = errors.New("action review capability unavailable")
var ErrProvider = errors.New("provider review unavailable")

type ReviewFactory func(context.Context, provider.Binding) (provider.PowerReviewer, error)

type Service struct {
	pool      *pgxpool.Pool
	sessions  *identity.Sessions
	enabled   bool
	factories map[string]ReviewFactory
	now       func() time.Time
}

func NewService(pool *pgxpool.Pool, sessions *identity.Sessions, enabled bool, factories map[string]ReviewFactory, clocks ...func() time.Time) *Service {
	copied := map[string]ReviewFactory{}
	for family, factory := range factories {
		copied[family] = factory
	}
	now := time.Now
	if len(clocks) > 0 && clocks[0] != nil {
		now = clocks[0]
	}
	return &Service{pool, sessions, enabled, copied, now}
}

type Review struct {
	ID         uuid.UUID `json:"id"`
	ResourceID uuid.UUID `json:"resource_id"`
	Action     string    `json:"action"`
	TargetName string    `json:"target_name"`
	HighImpact bool      `json:"high_impact"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type authority struct {
	binding                             provider.Binding
	provider, scope, native, name, role string
	grants, verified                    []string
	policy                              int64
}

func (s *Service) authority(ctx context.Context, tx pgx.Tx, actor, scope, target uuid.UUID) (authority, error) {
	var locked uuid.UUID
	if tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE id=$1 AND closed_at IS NULL FOR UPDATE`, scope).Scan(&locked) != nil {
		return authority{}, workspace.ErrNotFound
	}
	var a authority
	var conn uuid.UUID
	var family string
	var version int64
	err := tx.QueryRow(ctx, `SELECT c.id,c.provider,c.api_family,c.active_credential_version,r.scope,r.native_id,r.name,m.role,g.actions,
 CASE WHEN c.permissions_verified_at IS NULL THEN '{}'::text[] ELSE c.verified_capabilities END,w.policy_version
 FROM resources r JOIN connections c ON (c.workspace_id,c.id)=(r.workspace_id,r.connection_id)
 JOIN workspaces w ON w.id=r.workspace_id JOIN memberships m ON m.workspace_id=w.id AND m.user_id=$2
 JOIN users u ON u.id=m.user_id AND u.disabled_at IS NULL
 JOIN connection_grants g ON (g.workspace_id,g.connection_id,g.user_id)=(w.id,c.id,m.user_id)
 JOIN credential_versions v ON (v.workspace_id,v.connection_id,v.version)=(w.id,c.id,c.active_credential_version)
 JOIN sync_runs published ON (published.workspace_id,published.connection_id,published.scope,published.generation)=(r.workspace_id,r.connection_id,r.scope,r.generation)
 WHERE r.workspace_id=$1 AND r.id=$3 AND r.tombstoned_at IS NULL AND published.status='succeeded'
 AND 'read'=ANY(g.actions) AND c.status='active' AND v.revoked_at IS NULL FOR UPDATE OF c`, scope, actor, target).Scan(&conn, &a.provider, &family, &version, &a.scope, &a.native, &a.name, &a.role, &a.grants, &a.verified, &a.policy)
	if errors.Is(err, pgx.ErrNoRows) {
		return authority{}, workspace.ErrNotFound
	}
	if err != nil {
		return authority{}, ErrStore
	}
	a.binding, err = provider.NewBinding(scope, conn, family, version)
	if err != nil {
		return authority{}, ErrStore
	}
	return a, nil
}

func authorized(a authority, action string, high bool) bool {
	class := "operate"
	if high {
		class = "high_impact"
	}
	return workspace.WithinCeiling(a.role, class) && slices.Contains(a.grants, class) && slices.Contains(a.verified, action)
}

func (s *Service) inspect(ctx context.Context, a authority, i Intent) (provider.Preconditions, error) {
	if s == nil || !s.enabled {
		return provider.Preconditions{}, ErrUnsupported
	}
	factory := s.factories[a.provider+"/"+a.binding.Family()]
	if factory == nil {
		return provider.Preconditions{}, ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	reader, err := factory(ctx, a.binding)
	if err != nil || reader == nil {
		return provider.Preconditions{}, ErrProvider
	}
	p, err := reader.Inspect(ctx, a.binding, i.Action, a.native)
	if err != nil {
		return provider.Preconditions{}, ErrProvider
	}
	if !p.Allowed || p.Fingerprint == ([32]byte{}) {
		return provider.Preconditions{}, ErrStale
	}
	if p.TargetName == "" || !utf8.ValidString(p.TargetName) || utf8.RuneCountInString(p.TargetName) > 200 || stringsHaveControl(p.TargetName) {
		return provider.Preconditions{}, ErrProvider
	}
	return p, nil
}

func stringsHaveControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func (s *Service) Prepare(ctx context.Context, p identity.Principal, scope, target uuid.UUID, i Intent) (Review, error) {
	if i.Validate() != nil {
		return Review{}, ErrInvalid
	}
	if s == nil || s.pool == nil {
		return Review{}, ErrUnsupported
	}
	tx, err := platform.BeginTx(ctx, s.pool, pgx.TxOptions{})
	if err != nil {
		return Review{}, ErrStore
	}
	a, err := s.authority(ctx, tx, p.UserID, scope, target)
	tx.Rollback(ctx)
	if err != nil {
		return Review{}, err
	}
	if !authorized(a, i.Action, false) && !authorized(a, i.Action, true) {
		return Review{}, ErrDenied
	}
	pre, err := s.inspect(ctx, a, i)
	if err != nil {
		return Review{}, err
	}
	if pre.HighImpact && (s.sessions == nil || s.sessions.RequireRecent(p) != nil) {
		return Review{}, identity.ErrRecentRequired
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Review{}, ErrStore
	}
	hash, err := Digest(i, scope, p.UserID, a.binding.Connection(), target, id)
	if err != nil {
		return Review{}, err
	}
	encoded, _ := json.Marshal(i)
	review := Review{id, target, i.Action, pre.TargetName, pre.HighImpact, s.now().UTC().Add(5 * time.Minute)}
	tx, err = platform.BeginTx(ctx, s.pool, pgx.TxOptions{})
	if err != nil {
		return Review{}, ErrStore
	}
	defer tx.Rollback(ctx)
	current, err := s.authority(ctx, tx, p.UserID, scope, target)
	if err != nil {
		return Review{}, err
	}
	if current.binding.Version() != a.binding.Version() || current.policy != a.policy || !authorized(current, i.Action, pre.HighImpact) {
		return Review{}, ErrStale
	}
	var count int
	if tx.QueryRow(ctx, `SELECT count(*) FROM action_reviews WHERE workspace_id=$1 AND consumed_by IS NULL AND expires_at>clock_timestamp()`, scope).Scan(&count) != nil {
		return Review{}, ErrStore
	}
	if count >= 100 {
		return Review{}, ErrConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO action_reviews(id,workspace_id,actor_id,connection_id,resource_id,action,intent_version,intent,intent_hash,precondition_hash,credential_version,policy_version,high_impact,target_name,expires_at)
 VALUES($1,$2,$3,$4,$5,$6,1,$7,$8,$9,$10,$11,$12,$13,$14)`, id, scope, p.UserID, a.binding.Connection(), target, i.Action, encoded, hash[:], pre.Fingerprint[:], a.binding.Version(), a.policy, pre.HighImpact, pre.TargetName, review.ExpiresAt)
	if err != nil || tx.Commit(ctx) != nil {
		return Review{}, ErrStore
	}
	return review, nil
}
