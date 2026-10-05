package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/platform"
)

var ErrRateLimited = errors.New("login initiation limited")

type Challenges struct {
	pool *pgxpool.Pool
	now  func() time.Time
}
type Challenge struct {
	State, Browser, Nonce, Verifier string
	InvitationDigest                []byte
	SessionDigest                   []byte
	StartedAt                       time.Time
}

func NewChallenges(pool *pgxpool.Pool, now func() time.Time) *Challenges {
	return &Challenges{pool: pool, now: now}
}

// Start caps installation-wide issuance at 30/minute and 1000 stored challenges.
// A transaction lock keeps the count and insertion atomic across replicas.
func (c *Challenges) Start(ctx context.Context) (Challenge, error) {
	return c.start(ctx, nil, nil)
}

func (c *Challenges) StartInvitation(ctx context.Context, proof string) (Challenge, error) {
	digest, ok := tokenDigest(proof)
	if !ok {
		return Challenge{}, ErrUnauthenticated
	}
	return c.start(ctx, digest, nil)
}
func (c *Challenges) StartReauthentication(ctx context.Context, token string) (Challenge, error) {
	digest, ok := tokenDigest(token)
	if !ok {
		return Challenge{}, ErrUnauthenticated
	}
	return c.start(ctx, nil, digest)
}
func (c *Challenges) start(ctx context.Context, invitation, session []byte) (Challenge, error) {
	var challenge Challenge
	fields := []*string{&challenge.State, &challenge.Browser, &challenge.Nonce, &challenge.Verifier}
	for _, field := range fields {
		value, err := randomToken()
		if err != nil {
			return Challenge{}, err
		}
		*field = value
	}
	tx, err := platform.BeginTx(ctx, c.pool, pgx.TxOptions{})
	if err != nil {
		return Challenge{}, ErrStore
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(891014)`).Scan(&locked); err != nil {
		return Challenge{}, ErrStore
	}
	if !locked {
		return Challenge{}, ErrRateLimited
	}
	now := c.now().UTC()
	if _, err := tx.Exec(ctx, `DELETE FROM login_challenges WHERE state_digest IN (SELECT state_digest FROM login_challenges WHERE expires_at<=$1 ORDER BY expires_at LIMIT 100)`, now); err != nil {
		return Challenge{}, ErrStore
	}
	var issued int
	err = tx.QueryRow(ctx, `INSERT INTO login_initiation_budget VALUES(true,$1,1)
 ON CONFLICT(singleton) DO UPDATE SET
 issued=CASE WHEN login_initiation_budget.window_started_at<=$2 THEN 1 ELSE login_initiation_budget.issued+1 END,
 window_started_at=CASE WHEN login_initiation_budget.window_started_at<=$2 THEN $1 ELSE login_initiation_budget.window_started_at END
 WHERE login_initiation_budget.window_started_at<=$2 OR login_initiation_budget.issued<30
 RETURNING issued`, now, now.Add(-time.Minute)).Scan(&issued)
	if errors.Is(err, pgx.ErrNoRows) {
		return Challenge{}, ErrRateLimited
	}
	if err != nil {
		return Challenge{}, ErrStore
	}
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM login_challenges`).Scan(&total); err != nil {
		return Challenge{}, ErrStore
	}
	if total >= 1000 {
		return Challenge{}, ErrRateLimited
	}
	state, _ := tokenDigest(challenge.State)
	browser, _ := tokenDigest(challenge.Browser)
	if _, err := tx.Exec(ctx, `INSERT INTO login_challenges(state_digest,browser_digest,nonce,verifier,created_at,expires_at,invitation_digest,session_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, state, browser, challenge.Nonce, challenge.Verifier, now, now.Add(5*time.Minute), invitation, session); err != nil {
		return Challenge{}, ErrStore
	}
	if err := tx.Commit(ctx); err != nil {
		return Challenge{}, ErrStore
	}
	return challenge, nil
}

// Consume deletes before the external code exchange. Failed logins cannot replay
// a challenge, and state must be bound to the initiating browser cookie.
func (c *Challenges) Consume(ctx context.Context, state, browser string) (Challenge, error) {
	stateHash, stateOK := tokenDigest(state)
	browserHash, browserOK := tokenDigest(browser)
	if !stateOK || !browserOK {
		return Challenge{}, ErrUnauthenticated
	}
	var result Challenge
	err := c.pool.QueryRow(ctx, `DELETE FROM login_challenges WHERE state_digest=$1 AND browser_digest=$2 AND expires_at>$3 RETURNING nonce,verifier,invitation_digest,session_digest,created_at`, stateHash, browserHash, c.now().UTC()).Scan(&result.Nonce, &result.Verifier, &result.InvitationDigest, &result.SessionDigest, &result.StartedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Challenge{}, ErrUnauthenticated
	}
	if err != nil {
		return Challenge{}, ErrStore
	}
	return result, nil
}
