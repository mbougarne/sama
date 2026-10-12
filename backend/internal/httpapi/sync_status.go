package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"sama/backend/internal/identifier"
	"sama/backend/internal/inventory"
	"sama/backend/internal/workspace"
)

func (a *Auth) syncStatus(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok {
		return false
	}
	suffix, ok := strings.CutPrefix(r.URL.Path, "/api/v1/workspaces/"+m.WorkspaceID.String()+"/syncs/")
	if !ok || strings.Contains(suffix, "/") {
		return false
	}
	if r.Method != http.MethodGet {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return true
	}
	id, err := identifier.Parse(suffix)
	if err != nil || r.URL.RawQuery != "" {
		a.memberFailure(w, r, workspace.ErrInvalid)
		return true
	}
	result, err := inventory.Status(r.Context(), a.Pool, m.UserID, m.WorkspaceID, id)
	if err != nil {
		a.memberFailure(w, r, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(result)
	return true
}
