package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"sama/backend/internal/connection"
	"sama/backend/internal/workspace"
)

func (a *Auth) validationRoutes(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok || strings.TrimPrefix(r.URL.Path, "/api/v1/workspaces/"+m.WorkspaceID.String()) != "/connection-validations" {
		return false
	}
	if r.Method != http.MethodPost {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return true
	}
	var input struct {
		Family     string                `json:"family"`
		Credential connection.Credential `json:"credential"`
	}
	if !a.decodeMutation(w, r, &input) {
		return true
	}
	preview, err := a.ConnectionValidation.Preview(r.Context(), m.UserID, m.WorkspaceID, input.Family, input.Credential)
	if err != nil {
		status, code := 503, "provider_unavailable"
		switch {
		case errors.Is(err, workspace.ErrDenied):
			status, code = 403, "permission_denied"
		case errors.Is(err, connection.ErrUnsupported):
			status, code = 422, "unsupported_capability"
		case errors.Is(err, connection.ErrCredential):
			status, code = 400, "invalid_request"
		case errors.Is(err, connection.ErrValidationLimit):
			status, code = 429, "rate_limited"
		}
		writeProblem(w, r, status, code, http.StatusText(status))
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(preview)
	return true
}
