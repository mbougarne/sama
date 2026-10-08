package connection

import (
	"context"
	"errors"
	"regexp"
	"sama/backend/internal/workspace"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/platform/credentials"
	"sama/backend/internal/provider"
)

var ErrUnsupported = errors.New("provider validation unsupported")
var ErrValidation = errors.New("provider validation unavailable")
var ErrValidationLimit = errors.New("credential validation rate limited")

// Validator is implemented only by a qualified read adapter. It receives the
// exact request-owned typed credential and must use its shared outbound budget.
type Validator interface {
	Validate(context.Context, Credential) (string, []string, error)
}

type Preview struct {
	AccountIdentity string                `json:"account_identity"`
	VerifiedAt      time.Time             `json:"verified_at"`
	Capabilities    []provider.Capability `json:"capabilities"`
}

type ValidationService struct {
	pool    *pgxpool.Pool
	keyring *credentials.Keyring
	enabled bool
	readers map[string]Validator
	mu      sync.Mutex
	windows map[uuid.UUID][]time.Time
	now     func() time.Time
}

// No adapters are registered by default; tests inject synthetic read-only ports.
func NewValidationService(pool *pgxpool.Pool, keyring *credentials.Keyring, enabled bool, readers map[string]Validator) *ValidationService {
	copied := make(map[string]Validator, len(readers))
	for family, reader := range readers {
		copied[family] = reader
	}
	return &ValidationService{pool: pool, keyring: keyring, enabled: enabled, readers: copied, windows: map[uuid.UUID][]time.Time{}, now: time.Now}
}

var safeAccount = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,511}$`)

func (s *ValidationService) permit(actor uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	cutoff := now.Add(-time.Minute)
	for id, values := range s.windows {
		first := 0
		for first < len(values) && !values[first].After(cutoff) {
			first++
		}
		if first == len(values) {
			delete(s.windows, id)
		} else {
			s.windows[id] = values[first:]
		}
	}
	if len(s.windows[actor]) >= 5 || len(s.windows) >= 1000 && s.windows[actor] == nil {
		return false
	}
	s.windows[actor] = append(s.windows[actor], now)
	return true
}

func (s *ValidationService) Preview(ctx context.Context, actor, scope uuid.UUID, family string, input Credential) (Preview, error) {
	if s == nil || !s.enabled || s.keyring.Require(nil) != nil {
		return Preview{}, ErrValidation
	}
	// Current authority is checked before any credential-bearing provider request.
	var allowed bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships m JOIN users u ON u.id=m.user_id
        JOIN workspaces w ON w.id=m.workspace_id WHERE m.workspace_id=$1 AND m.user_id=$2
        AND m.role IN ('admin','owner') AND w.closed_at IS NULL AND u.disabled_at IS NULL)`, scope, actor).Scan(&allowed)
	if err != nil {
		return Preview{}, ErrValidation
	}
	if !allowed {
		return Preview{}, workspace.ErrDenied
	}
	if input.Validate() != nil {
		return Preview{}, ErrCredential
	}
	reader := s.readers[family]
	if reader == nil {
		return Preview{}, ErrUnsupported
	}
	if !s.permit(actor) {
		return Preview{}, ErrValidationLimit
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	account, verified, err := reader.Validate(ctx, input)
	if err != nil || !safeAccount.MatchString(account) || strings.Contains(account, input.Token) {
		return Preview{}, ErrValidation
	}
	// This slice exposes only read qualification, never inferred action authority.
	catalog := []provider.Support{{Action: "compute.server.read", Class: "read"}}
	capabilities := provider.Effective(catalog, []string{"compute.server.read"}, verified, []string{"read"}, []string{"compute.server.read"})
	return Preview{account, s.now().UTC(), capabilities}, nil
}
