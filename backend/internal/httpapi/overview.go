package httpapi

import (
	"encoding/json"
	"net/http"
	"sama/backend/internal/workspace"
)

func (a *Auth) overview(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok || r.URL.Path != "/api/v1/workspaces/"+m.WorkspaceID.String()+"/overview" {
		return false
	}
	if r.Method != http.MethodGet {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return true
	}
	if r.URL.RawQuery != "" {
		a.memberFailure(w, r, workspace.ErrInvalid)
		return true
	}
	value, err := workspace.ReadOverview(r.Context(), a.Pool, m.UserID, m.WorkspaceID)
	if err != nil {
		a.memberFailure(w, r, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
	return true
}
