package httpapi

import (
	"encoding/json"
	"net/http"
	"sama/backend/internal/connection"
	"sama/backend/internal/identifier"
	"sama/backend/internal/workspace"
	"strings"
)

func (a *Auth) connectionRotation(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok {
		return false
	}
	suffix, ok := strings.CutPrefix(r.URL.Path, "/api/v1/workspaces/"+m.WorkspaceID.String()+"/connections/")
	if !ok {
		return false
	}
	parts := strings.Split(suffix, "/")
	if len(parts) != 2 || parts[1] != "credential-rotations" {
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
	var input struct {
		Credential connection.Credential `json:"credential"`
	}
	if !a.decodeMutation(w, r, &input) {
		return true
	}
	version, err := a.ConnectionValidation.Rotate(r.Context(), m.UserID, m.WorkspaceID, id, input.Credential, RequestID(r.Context()))
	if err != nil {
		a.connectionWriteFailure(w, r, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Version int64 `json:"version"`
	}{version})
	return true
}
