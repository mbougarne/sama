package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"sama/backend/internal/identifier"
	"sama/backend/internal/inventory"
	"sama/backend/internal/workspace"
)

func (a *Auth) connectionSyncs(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok {
		return false
	}
	suffix, ok := strings.CutPrefix(r.URL.Path, "/api/v1/workspaces/"+m.WorkspaceID.String()+"/connections/")
	if !ok {
		return false
	}
	parts := strings.Split(suffix, "/")
	if len(parts) != 2 || parts[1] != "syncs" {
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
	syncID, err := inventory.Refresh(r.Context(), a.Pool, m.UserID, m.WorkspaceID, id)
	if errors.Is(err, inventory.ErrQuota) {
		writeProblem(w, r, 429, "queue_quota_reached", "Too Many Requests")
		return true
	}
	if err != nil {
		a.memberFailure(w, r, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(struct {
		ID string `json:"sync_id"`
	}{syncID.String()})
	return true
}
