package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"sama/backend/internal/identifier"
	"sama/backend/internal/workspace"
)

func (a *Auth) grantRoutes(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok {
		return false
	}
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/workspaces/"+m.WorkspaceID.String()+"/connections/")
	parts := strings.Split(suffix, "/")
	if len(parts) != 2 || parts[1] != "grants" {
		return false
	}
	connection, err := identifier.Parse(parts[0])
	if err != nil {
		a.memberFailure(w, r, workspace.ErrInvalid)
		return true
	}
	switch r.Method {
	case http.MethodGet:
		actions, err := workspace.ReadGrant(r.Context(), a.Pool, m.UserID, m.WorkspaceID, connection)
		if err != nil {
			a.memberFailure(w, r, err)
			return true
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Actions []string `json:"actions"`
		}{actions})
	case http.MethodPut:
		var input struct {
			UserID  string    `json:"user_id"`
			Actions *[]string `json:"actions"`
		}
		if !a.decodeMutation(w, r, &input) {
			return true
		}
		user, err := identifier.Parse(input.UserID)
		if err != nil || input.Actions == nil {
			a.memberFailure(w, r, workspace.ErrInvalid)
			return true
		}
		if err := workspace.SetGrant(r.Context(), a.Pool, m.UserID, m.WorkspaceID, connection, user, *input.Actions, RequestID(r.Context())); err != nil {
			a.memberFailure(w, r, err)
			return true
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
	}
	return true
}
