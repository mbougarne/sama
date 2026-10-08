package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/platform/credentials"
)

// CredentialReadiness checks all retained envelopes, including historical versions.
// Missing material closes readiness while the existing liveness handler stays live.
func CredentialReadiness(next http.Handler, pool *pgxpool.Pool, ring *credentials.Keyring) http.Handler {
	return SecurityHeaders(withRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
			return
		}
		ready := pool != nil && ring.Require(nil) == nil
		if ready {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			rows, err := pool.Query(ctx, `SELECT DISTINCT master_key_id FROM credential_versions LIMIT 65`)
			if err != nil {
				ready = false
			} else {
				defer rows.Close()
				var ids []string
				for rows.Next() {
					var id string
					if rows.Scan(&id) != nil {
						ready = false
						break
					}
					ids = append(ids, id)
				}
				ready = ready && rows.Err() == nil && ring.Require(ids) == nil
			}
		}
		if !ready {
			writeProblem(w, r, 503, "credentials_unavailable", "Credential readiness unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte("{\"status\":\"ok\"}\n"))
	})))
}
