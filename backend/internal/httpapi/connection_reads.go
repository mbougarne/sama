package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"sama/backend/internal/connection"
	"sama/backend/internal/identifier"
	"sama/backend/internal/workspace"
)

func (a *Auth) connectionReads(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok {
		return false
	}
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/workspaces/"+m.WorkspaceID.String())
	if suffix != "/connections" && !strings.HasPrefix(suffix, "/connections/") {
		return false
	}
	target := uuid.Nil
	capabilities := false
	if suffix != "/connections" {
		parts := strings.Split(strings.TrimPrefix(suffix, "/connections/"), "/")
		if len(parts) > 2 || len(parts) == 2 && parts[1] != "capabilities" {
			return false
		}
		id, err := identifier.Parse(parts[0])
		if err != nil {
			a.memberFailure(w, r, workspace.ErrInvalid)
			return true
		}
		target = id
		capabilities = len(parts) == 2
	}
	if r.Method != http.MethodGet {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return true
	}
	limit := 50
	cursor := uuid.Nil
	q := r.URL.Query()
	if len(q["limit"]) > 1 || len(q["cursor"]) > 1 {
		a.memberFailure(w, r, workspace.ErrInvalid)
		return true
	}
	if value := q.Get("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 200 {
			a.memberFailure(w, r, workspace.ErrInvalid)
			return true
		}
		limit = n
	}
	if value := q.Get("cursor"); value != "" {
		id, err := identifier.Parse(value)
		if err != nil {
			a.memberFailure(w, r, workspace.ErrInvalid)
			return true
		}
		cursor = id
	}
	if target != uuid.Nil && len(q) > 0 {
		a.memberFailure(w, r, workspace.ErrInvalid)
		return true
	}
	page, err := connection.ReadAuthorized(r.Context(), a.Pool, m.UserID, m.WorkspaceID, cursor, target, limit, a.ConnectionSupport)
	if err != nil {
		a.memberFailure(w, r, err)
		return true
	}
	var result any = page
	if target != uuid.Nil {
		result = page.Data[0]
		if capabilities {
			result = page.Data[0].Capabilities
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
	return true
}
