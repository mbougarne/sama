package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"sama/backend/internal/connection"
	"sama/backend/internal/workspace"
)

func (a *Auth) connectionWrites(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok || r.URL.Path != "/api/v1/workspaces/"+m.WorkspaceID.String()+"/connections" || r.Method != http.MethodPost {
		return false
	}
	var input connection.SaveInput
	if !a.decodeMutation(w, r, &input) {
		return true
	}
	saved, err := a.ConnectionValidation.Save(r.Context(), m.UserID, m.WorkspaceID, input, RequestID(r.Context()))
	if err != nil {
		a.connectionWriteFailure(w, r, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(saved)
	return true
}

func (a *Auth) connectionWriteFailure(w http.ResponseWriter, r *http.Request, err error) {
	status, code := 503, "provider_unavailable"
	switch {
	case errors.Is(err, connection.ErrUnsupported):
		status, code = 422, "unsupported_capability"
	case errors.Is(err, connection.ErrCredential), errors.Is(err, workspace.ErrInvalid):
		status, code = 400, "invalid_request"
	case errors.Is(err, connection.ErrValidationLimit):
		status, code = 429, "rate_limited"
	case errors.Is(err, connection.ErrVersion):
		status, code = 409, "credential_version_changed"
	case errors.Is(err, workspace.ErrDenied):
		status, code = 403, "permission_denied"
	case errors.Is(err, workspace.ErrNotFound):
		status, code = 404, "not_found"
	}
	writeProblem(w, r, status, code, http.StatusText(status))
}
