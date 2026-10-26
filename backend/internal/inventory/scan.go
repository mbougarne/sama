package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/job"
	"sama/backend/internal/platform"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
	"sama/backend/internal/workspace"
)

var ErrScan = errors.New("inventory scan unavailable")

// Scanner executes a claimed job through injected read-only ports. Registering
// and starting workers is a separate scheduler ticket; no worker starts here.
type Scanner struct {
	Pool      *pgxpool.Pool
	Reader    func(context.Context, provider.Binding) (provider.InventoryReader, error)
	Snapshots func(context.Context, provider.Binding) (provider.SnapshotReader, error)
}

// Scan stages only: even the last successful page never changes public resources.
func (s Scanner) Scan(ctx context.Context, lease job.Lease) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var run uuid.UUID
	var actor *uuid.UUID
	var kind string
	err := s.Pool.QueryRow(ctx, `SELECT j.sync_id,s.actor_id,s.resource_type FROM jobs j JOIN sync_runs s
 ON (s.workspace_id,s.connection_id,s.id)=(j.workspace_id,j.connection_id,j.sync_id)
 WHERE j.id=$1 AND j.workspace_id=$2 AND j.connection_id=$3 AND j.payload_version=2`,
		lease.ID, lease.Workspace, lease.Connection).Scan(&run, &actor, &kind)
	if err != nil || actor == nil {
		return ErrScan
	}
	return (job.Scheduler{Pool: s.Pool, Now: time.Now}).Run(ctx, lease, func(ctx context.Context) error {
		binding, err := s.guard(ctx, lease, run, *actor, 0, true)
		if err != nil {
			return err
		}
		readPage, err := s.pageReader(ctx, binding, kind)
		if err != nil {
			return ErrScan
		}
		cursor, count, bytes := "", 0, 0
		seen := map[string]bool{}
		for pageNumber := 1; pageNumber <= 100; pageNumber++ {
			if _, err = s.guard(ctx, lease, run, *actor, binding.Version(), false); err != nil {
				return err
			}
			page, err := readPage(ctx, cursor)
			if err != nil {
				return &ScanFailure{Code: failureCode(err)}
			}
			encoded, err := json.Marshal(page.Data)
			count += len(page.Data)
			bytes += len(encoded)
			if err != nil || len(encoded) > 8<<20 || bytes > 32<<20 || count > 10000 || len(page.Data) > 200 || len(page.Next) > 512 || page.Next != "" && seen[page.Next] {
				return &ScanFailure{Code: "sync_limit_reached"}
			}
			if err = s.stage(ctx, lease, run, *actor, binding, page, pageNumber); err != nil {
				return err
			}
			if page.Next == "" {
				return nil
			}
			seen[page.Next] = true
			cursor = page.Next
		}
		return &ScanFailure{Code: "sync_limit_reached"}
	})
}

type ScanFailure struct{ Code string }

func (e *ScanFailure) Error() string { return e.Code }

func failureCode(err error) string {
	var scan *ScanFailure
	var upstream *outbound.Failure
	code := "provider_unavailable"
	if errors.As(err, &scan) {
		code = scan.Code
	}
	if errors.As(err, &upstream) {
		code = upstream.Code
	}
	switch code {
	case "permission_denied", "provider_permission_denied":
		return "permission_denied"
	case "provider_rate_limited", "provider_response_invalid", "sync_limit_reached":
		return code
	default:
		return "provider_unavailable"
	}
}

func scanAuthority(ctx context.Context, tx pgx.Tx, lease job.Lease, run, actor uuid.UUID, version int64) (provider.Binding, error) {
	// Use the same workspace lock order as grant/membership changes, then read
	// fresh authority. Do not hold a database transaction over provider traffic.
	var locked uuid.UUID
	if tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE id=$1 AND closed_at IS NULL FOR UPDATE`, lease.Workspace).Scan(&locked) != nil {
		return provider.Binding{}, ErrScan
	}
	var family, role string
	var current int64
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT c.api_family,c.active_credential_version,m.role,
 c.status='active' AND v.revoked_at IS NULL AND 'read'=ANY(g.actions) AND 'refresh'=ANY(g.actions)
 FROM connections c JOIN memberships m ON m.workspace_id=c.workspace_id AND m.user_id=$3
 JOIN users u ON u.id=m.user_id AND u.disabled_at IS NULL
 JOIN connection_grants g ON (g.workspace_id,g.connection_id,g.user_id)=(c.workspace_id,c.id,m.user_id)
 JOIN credential_versions v ON (v.workspace_id,v.connection_id,v.version)=(c.workspace_id,c.id,c.active_credential_version)
 WHERE c.workspace_id=$1 AND c.id=$2 FOR UPDATE OF c`, lease.Workspace, lease.Connection, actor).Scan(&family, &current, &role, &allowed)
	if err != nil || !allowed || !workspace.WithinCeiling(role, "refresh") || version != 0 && version != current {
		return provider.Binding{}, &ScanFailure{Code: "permission_denied"}
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `SELECT j.id FROM jobs j JOIN sync_runs s ON (s.workspace_id,s.connection_id,s.id)=(j.workspace_id,j.connection_id,j.sync_id)
 WHERE j.id=$1 AND j.workspace_id=$2 AND j.connection_id=$3 AND j.sync_id=$4 AND s.actor_id=$5
 AND j.lease_owner=$6 AND j.lease_token=$7 AND j.state='running' AND j.lease_expires_at>clock_timestamp()
 AND j.deadline>clock_timestamp() AND s.status IN ('pending','running') FOR UPDATE OF j,s`,
		lease.ID, lease.Workspace, lease.Connection, run, actor, lease.Owner, lease.Token).Scan(&id)
	if err != nil {
		return provider.Binding{}, job.ErrLease
	}
	return provider.NewBinding(lease.Workspace, lease.Connection, family, current)
}

func (s Scanner) guard(ctx context.Context, lease job.Lease, run, actor uuid.UUID, version int64, start bool) (provider.Binding, error) {
	tx, err := platform.BeginTx(ctx, s.Pool, pgx.TxOptions{})
	if err != nil {
		return provider.Binding{}, ErrScan
	}
	defer tx.Rollback(ctx)
	binding, err := scanAuthority(ctx, tx, lease, run, actor, version)
	if err != nil {
		return provider.Binding{}, err
	}
	if start {
		if _, err = tx.Exec(ctx, `DELETE FROM inventory_staging WHERE workspace_id=$1 AND connection_id=$2 AND generation=(SELECT generation FROM sync_runs WHERE id=$3)`, lease.Workspace, lease.Connection, run); err != nil {
			return provider.Binding{}, ErrScan
		}
		generation, genErr := uuid.NewV7()
		if genErr != nil {
			return provider.Binding{}, ErrScan
		}
		_, err = tx.Exec(ctx, `UPDATE sync_runs SET generation=CASE WHEN started_at IS NULL THEN generation ELSE $3 END,status='running',page_count=0,scan_complete=false,started_at=clock_timestamp(),credential_version=$2 WHERE id=$1`, run, binding.Version(), generation)
		if err != nil {
			return provider.Binding{}, ErrScan
		}
	}
	if tx.Commit(ctx) != nil {
		return provider.Binding{}, ErrScan
	}
	return binding, nil
}

func (s Scanner) stage(ctx context.Context, lease job.Lease, run, actor uuid.UUID, binding provider.Binding, page scanPage, number int) error {
	tx, err := platform.BeginTx(ctx, s.Pool, pgx.TxOptions{})
	if err != nil {
		return ErrScan
	}
	defer tx.Rollback(ctx)
	if _, err = scanAuthority(ctx, tx, lease, run, actor, binding.Version()); err != nil {
		return err
	}
	for _, o := range page.Data {
		if o.ObservedAt.IsZero() {
			return &ScanFailure{Code: "provider_response_invalid"}
		}
		id, err := uuid.NewV7()
		if err != nil {
			return ErrScan
		}
		details, err := json.Marshal(o.Details)
		if err != nil {
			return ErrScan
		}
		_, err = tx.Exec(ctx, `INSERT INTO inventory_staging(id,workspace_id,connection_id,api_family,resource_type,scope,native_id,native_status,status,name,details_version,details,observed_at,generation,tombstoned_at,version)
 SELECT $1,$2,$3,$4,s.resource_type,s.scope,$5,$6,$7,$8,1,$9,$10,s.generation,NULL,1 FROM sync_runs s WHERE s.id=$11`,
			id, lease.Workspace, lease.Connection, binding.Family(), o.NativeID, o.NativeStatus, o.Status, o.Name, details, o.ObservedAt, run)
		if err != nil {
			return &ScanFailure{Code: "provider_response_invalid"}
		}
	}
	_, err = tx.Exec(ctx, `UPDATE sync_runs SET page_count=$2,scan_complete=$3 WHERE id=$1`, run, number, page.Next == "")
	if err != nil || tx.Commit(ctx) != nil {
		return ErrScan
	}
	return nil
}
