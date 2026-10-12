package operation

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sama/backend/internal/audit"
	"sama/backend/internal/identity"
	"sama/backend/internal/job"
	"sama/backend/internal/platform"
	"sama/backend/internal/workspace"
)

var ErrQuota = errors.New("operation queue quota reached")

type Confirmation struct {
	ReviewID    uuid.UUID
	Confirmed   bool
	TypedTarget string
}

type stored struct {
	target, connection           uuid.UUID
	intent                       Intent
	encoded, hash, preconditions []byte
	credential, policy           int64
	high                         bool
	name                         string
	expiry                       time.Time
	consumed                     *uuid.UUID
}

func (s *Service) load(ctx context.Context, tx pgx.Tx, actor, scope, id uuid.UUID) (stored, error) {
	var r stored
	err := tx.QueryRow(ctx, `SELECT resource_id,connection_id,intent,intent_hash,precondition_hash,credential_version,policy_version,high_impact,target_name,expires_at,consumed_by
 FROM action_reviews WHERE workspace_id=$1 AND actor_id=$2 AND id=$3 FOR UPDATE`, scope, actor, id).Scan(&r.target, &r.connection, &r.encoded, &r.hash, &r.preconditions, &r.credential, &r.policy, &r.high, &r.name, &r.expiry, &r.consumed)
	if errors.Is(err, pgx.ErrNoRows) {
		return stored{}, workspace.ErrNotFound
	}
	if err != nil {
		return stored{}, ErrStore
	}
	r.intent, err = DecodeIntent(r.encoded)
	if err != nil {
		return stored{}, ErrStore
	}
	return r, nil
}

func (s *Service) check(p identity.Principal, a authority, r stored, c Confirmation) error {
	if r.consumed != nil || !r.expiry.After(s.now()) || r.connection != a.binding.Connection() || r.credential != a.binding.Version() || r.policy != a.policy {
		return ErrStale
	}
	if !authorized(a, r.intent.Action, r.high) {
		return ErrDenied
	}
	if r.high {
		if c.TypedTarget != r.name {
			return ErrInvalid
		}
		if s.sessions == nil || s.sessions.RequireRecent(p) != nil {
			return identity.ErrRecentRequired
		}
	}
	return nil
}

// Confirm reads fresh preconditions outside transactions, then commits review,
// reservation, operation/job/event/audit together. Idempotent recovery comes first.
func (s *Service) Confirm(ctx context.Context, p identity.Principal, scope uuid.UUID, key string, c Confirmation, request string) (Accepted, error) {
	if s == nil || s.pool == nil {
		return Accepted{}, ErrUnsupported
	}
	if !ValidKey(key) || c.ReviewID.Version() != 7 || !c.Confirmed {
		return Accepted{}, ErrInvalid
	}
	tx, err := platform.BeginTx(ctx, s.pool, pgx.TxOptions{})
	if err != nil {
		return Accepted{}, ErrStore
	}
	defer tx.Rollback(ctx)
	// Use the same lock order as membership, grants, review preparation and other
	// acceptances. This also serializes concurrent requests without an in-memory map.
	var locked uuid.UUID
	if tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE id=$1 AND closed_at IS NULL FOR UPDATE`, scope).Scan(&locked) != nil {
		return Accepted{}, workspace.ErrNotFound
	}
	r, err := s.load(ctx, tx, p.UserID, scope, c.ReviewID)
	if err != nil {
		return Accepted{}, err
	}
	hash, err := Digest(r.intent, scope, p.UserID, r.connection, r.target, c.ReviewID)
	if err != nil || subtle.ConstantTimeCompare(hash[:], r.hash) != 1 {
		return Accepted{}, ErrStale
	}
	if result, found, err := Resolve(ctx, tx, scope, p.UserID, key, hash); err != nil || found {
		return result, err
	}
	a, err := s.authority(ctx, tx, p.UserID, scope, r.target)
	if err != nil {
		return Accepted{}, err
	}
	if err = s.check(p, a, r, c); err != nil {
		return Accepted{}, err
	}
	tx.Rollback(ctx)
	pre, err := s.inspect(ctx, a, r.intent)
	if err != nil {
		return Accepted{}, err
	}
	if subtle.ConstantTimeCompare(pre.Fingerprint[:], r.preconditions) != 1 || pre.HighImpact != r.high || pre.TargetName != r.name {
		return Accepted{}, ErrStale
	}
	tx, err = platform.BeginTx(ctx, s.pool, pgx.TxOptions{})
	if err != nil {
		return Accepted{}, ErrStore
	}
	defer tx.Rollback(ctx)
	if tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE id=$1 AND closed_at IS NULL FOR UPDATE`, scope).Scan(&locked) != nil {
		return Accepted{}, workspace.ErrNotFound
	}
	if result, found, err := Resolve(ctx, tx, scope, p.UserID, key, hash); err != nil || found {
		return result, err
	}
	r, err = s.load(ctx, tx, p.UserID, scope, c.ReviewID)
	if err != nil {
		return Accepted{}, err
	}
	current, err := s.authority(ctx, tx, p.UserID, scope, r.target)
	if err != nil {
		return Accepted{}, err
	}
	if err = s.check(p, current, r, c); err != nil {
		return Accepted{}, err
	}
	if subtle.ConstantTimeCompare(hash[:], r.hash) != 1 || subtle.ConstantTimeCompare(pre.Fingerprint[:], r.preconditions) != 1 {
		return Accepted{}, ErrStale
	}
	var limit, count int
	if tx.QueryRow(ctx, `SELECT queue_limit,(SELECT count(*) FROM jobs WHERE workspace_id=$1 AND state IN ('queued','running')) FROM workspaces WHERE id=$1`, scope).Scan(&limit, &count) != nil {
		return Accepted{}, ErrStore
	}
	if count >= limit {
		return Accepted{}, ErrQuota
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Accepted{}, ErrStore
	}
	jobID, err := uuid.NewV7()
	if err != nil {
		return Accepted{}, ErrStore
	}
	deadline := s.now().Add(30 * time.Minute)
	_, err = tx.Exec(ctx, `INSERT INTO operations(id,workspace_id,actor_id,connection_id,resource_id,action,intent_version,intent,intent_hash,idempotency_key,review_id,credential_version,policy_version,state,event_sequence,deadline)
 VALUES($1,$2,$3,$4,$5,$6,1,$7,$8,$9,$10,$11,$12,'queued',1,$13)`, id, scope, p.UserID, r.connection, r.target, r.intent.Action, r.encoded, hash[:], key, c.ReviewID, r.credential, r.policy, deadline)
	if err != nil {
		return Accepted{}, ErrStore
	}
	if _, err = Reserve(ctx, tx, scope, r.connection, id, []Target{{Kind: "resource", Scope: current.scope, NativeID: current.native}}); err != nil {
		return Accepted{}, err
	}
	if job.InsertOperation(ctx, tx, job.Record{ID: jobID, Workspace: scope, Connection: r.connection, Operation: id, Type: "operation_dispatch", PayloadVersion: 3, Deadline: deadline}) != nil {
		return Accepted{}, ErrStore
	}
	if _, err = tx.Exec(ctx, `UPDATE action_reviews SET consumed_by=$2 WHERE id=$1`, c.ReviewID, id); err != nil {
		return Accepted{}, ErrStore
	}
	if AppendEvent(ctx, tx, scope, id, 1, "", "queued") != nil {
		return Accepted{}, ErrStore
	}
	event, err := audit.NewID()
	if err != nil || audit.Append(ctx, tx, audit.Event{ID: event, Workspace: scope, ActorKind: audit.ActorUser, ActorID: &p.UserID, Type: "operation.accepted", RequestID: request, Metadata: map[string]string{"operation_id": id.String(), "resource_id": r.target.String()}}) != nil {
		return Accepted{}, ErrStore
	}
	if tx.Commit(ctx) != nil {
		return Accepted{}, ErrStore
	}
	return Accepted{id, "queued"}, nil
}
