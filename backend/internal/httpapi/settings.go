package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"sama/backend/internal/workspace"
)

func (a *Auth) workspaceSettings(w http.ResponseWriter, r *http.Request, m workspace.Membership) {
	var update *workspace.Settings
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		update = new(workspace.Settings)
		if !a.decodeMutation(w, r, update) {
			return
		}
	default:
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return
	}
	settings, err := workspace.WorkspaceSettings(r.Context(), a.Pool, m.UserID, m.WorkspaceID, update, RequestID(r.Context()))
	if errors.Is(err, workspace.ErrPolicyConflict) {
		writeProblem(w, r, 409, "policy_conflict", "Conflict")
		return
	}
	if err != nil {
		a.memberFailure(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}
