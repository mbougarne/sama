package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"sama/backend/internal/identifier"
	"sama/backend/internal/identity"
	"sama/backend/internal/operation"
)

func (a *Auth) operationFailure(w http.ResponseWriter, r *http.Request, err error) {
	status, code := 503, "provider_unavailable"
	switch {
	case errors.Is(err, operation.ErrInvalid):
		status, code = 422, "invalid_request"
	case errors.Is(err, operation.ErrUnsupported):
		status, code = 422, "unsupported_capability"
	case errors.Is(err, operation.ErrDenied):
		status, code = 403, "permission_denied"
	case errors.Is(err, identity.ErrRecentRequired):
		status, code = 403, "recent_authentication_required"
	case errors.Is(err, operation.ErrStale):
		status, code = 409, "stale_review"
	case errors.Is(err, operation.ErrConflict):
		status, code = 409, "idempotency_conflict"
	case errors.Is(err, operation.ErrQuota):
		status, code = 429, "queue_quota_reached"
	case errors.Is(err, operation.ErrProvider), errors.Is(err, operation.ErrStore):
		status, code = 503, "provider_unavailable"
	case errors.Is(err, operation.ErrBusy):
		status, code = 409, "resource_busy"
	default:
		a.memberFailure(w, r, err)
		return
	}
	writeProblem(w, r, status, code, http.StatusText(status))
}

func (a *Auth) actionReview(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok {
		return false
	}
	suffix, ok := strings.CutPrefix(r.URL.Path, "/api/v1/workspaces/"+m.WorkspaceID.String()+"/resources/")
	if !ok {
		return false
	}
	parts := strings.Split(suffix, "/")
	if len(parts) != 2 || parts[1] != "action-reviews" {
		return false
	}
	if r.Method != http.MethodPost {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return true
	}
	id, err := identifier.Parse(parts[0])
	if err != nil || r.URL.RawQuery != "" {
		writeProblem(w, r, 400, "invalid_request", "Bad Request")
		return true
	}
	var intent operation.Intent
	if !a.decodeMutation(w, r, &intent) {
		return true
	}
	p, _ := CurrentPrincipal(r.Context())
	result, err := a.Operations.Prepare(r.Context(), p, m.WorkspaceID, id, intent)
	if err != nil {
		a.operationFailure(w, r, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
	return true
}
