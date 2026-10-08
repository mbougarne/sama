package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"sama/backend/internal/connection"
	"sama/backend/internal/identifier"
	"sama/backend/internal/workspace"
)

func (a *Auth) connectionDisable(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok {
		return false
	}
	prefix := "/api/v1/workspaces/" + m.WorkspaceID.String() + "/connections/"
	suffix, ok := strings.CutPrefix(r.URL.Path, prefix)
	if !ok {
		return false
	}
	parts := strings.Split(suffix, "/")
	if len(parts) != 2 || parts[1] != "disable" {
		return false
	}
	if r.Method != http.MethodPost {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return true
	}
	id, err := identifier.Parse(parts[0])
	if err != nil {
		a.memberFailure(w, r, workspace.ErrInvalid)
		return true
	}
	var input struct{}
	if !a.decodeMutation(w, r, &input) {
		return true
	}
	if err := connection.Disable(r.Context(), a.Pool, m.UserID, m.WorkspaceID, id, RequestID(r.Context())); err != nil {
		a.memberFailure(w, r, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}{"disabled", "New work is disabled. Provider credentials are not revoked; revoke them at the provider if needed."})
	return true
}
