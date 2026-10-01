package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
)

func (a *Auth) createWorkspace(w http.ResponseWriter, r *http.Request) {
	principal, ok := CurrentPrincipal(r.Context())
	if !ok {
		a.loginFailure(w, r, identity.ErrUnauthenticated)
		return
	}
	if !a.validMutation(r, principal) {
		writeProblem(w, r, 403, "permission_denied", "Forbidden")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var input struct {
		Name string `json:"name"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, r, 400, "invalid_request", "Bad Request")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeProblem(w, r, 400, "invalid_request", "Bad Request")
		return
	}
	created, err := workspace.Create(r.Context(), a.Pool, principal.UserID, input.Name, RequestID(r.Context()))
	if err != nil {
		status, code := 503, "identity_unavailable"
		switch {
		case errors.Is(err, workspace.ErrInvalid):
			status, code = 422, "invalid_workspace"
		case errors.Is(err, workspace.ErrLimit):
			status, code = 429, "workspace_limit"
		case errors.Is(err, workspace.ErrDenied):
			status, code = 403, "permission_denied"
		}
		writeProblem(w, r, status, code, http.StatusText(status))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", "/api/v1/workspaces/"+created.ID.String())
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}
