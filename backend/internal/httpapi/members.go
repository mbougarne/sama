package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"sama/backend/internal/identifier"
	"sama/backend/internal/workspace"
)

func (a *Auth) memberRoutes(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok {
		return false
	}
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/workspaces/"+m.WorkspaceID.String())
	if suffix != "/members" {
		return false
	}
	if r.Method != http.MethodGet {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return true
	}
	q := r.URL.Query()
	limit := 50
	cursor := uuid.Nil
	if len(q["limit"]) > 1 || len(q["cursor"]) > 1 {
		a.memberFailure(w, r, workspace.ErrInvalid)
		return true
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			a.memberFailure(w, r, workspace.ErrInvalid)
			return true
		}
		limit = n
	}
	if v := q.Get("cursor"); v != "" {
		id, err := identifier.Parse(v)
		if err != nil {
			a.memberFailure(w, r, workspace.ErrInvalid)
			return true
		}
		cursor = id
	}
	page, err := workspace.ListMembers(r.Context(), a.Pool, m.UserID, m.WorkspaceID, cursor, limit)
	if err != nil {
		a.memberFailure(w, r, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(page)
	return true
}

func (a *Auth) memberFailure(w http.ResponseWriter, r *http.Request, err error) {
	status, code := 503, "identity_unavailable"
	switch {
	case errors.Is(err, workspace.ErrInvalid):
		status, code = 400, "invalid_request"
	case errors.Is(err, workspace.ErrDenied):
		status, code = 403, "permission_denied"
	case errors.Is(err, workspace.ErrNotFound):
		status, code = 404, "not_found"
	}
	writeProblem(w, r, status, code, http.StatusText(status))
}
