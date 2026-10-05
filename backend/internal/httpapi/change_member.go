package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"sama/backend/internal/identifier"
	"sama/backend/internal/workspace"
)

// decodeMutation applies the existing origin/CSRF policy and bounded strict JSON.
func (a *Auth) decodeMutation(w http.ResponseWriter, r *http.Request, input any) bool {
	p, ok := CurrentPrincipal(r.Context())
	if !ok || !a.validMutation(r, p) {
		writeProblem(w, r, 403, "permission_denied", "Forbidden")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(input) != nil || decoder.Decode(new(any)) != io.EOF {
		a.memberFailure(w, r, workspace.ErrInvalid)
		return false
	}
	return true
}
func (a *Auth) changeMember(w http.ResponseWriter, r *http.Request, m workspace.Membership, target string) {
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return
	}
	user, err := identifier.Parse(target)
	if err != nil {
		a.memberFailure(w, r, workspace.ErrInvalid)
		return
	}
	var input struct {
		Role    string `json:"role"`
		Version *int64 `json:"version"`
	}
	if !a.decodeMutation(w, r, &input) {
		return
	}
	if input.Version == nil || r.Method == http.MethodPut && input.Role == "" || r.Method == http.MethodDelete && input.Role != "" {
		a.memberFailure(w, r, workspace.ErrInvalid)
		return
	}
	if err := workspace.ChangeMember(r.Context(), a.Pool, m.UserID, m.WorkspaceID, user, input.Role, *input.Version, RequestID(r.Context())); err != nil {
		a.memberFailure(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
