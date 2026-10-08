package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"sama/backend/internal/identifier"
	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
)

func (a *Auth) apiRoutes(w http.ResponseWriter, r *http.Request) {
	if a.grantRoutes(w, r) || a.memberRoutes(w, r) {
		return
	}
	if r.URL.Path != "/api/v1/me" && r.URL.Path != "/api/v1/workspaces" {
		route(w, r)
		return
	}
	if r.URL.Path == "/api/v1/workspaces" && r.Method == http.MethodPost {
		a.createWorkspace(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return
	}
	principal, ok := CurrentPrincipal(r.Context())
	if !ok {
		a.loginFailure(w, r, identity.ErrUnauthenticated)
		return
	}
	var response any
	if r.URL.Path == "/api/v1/me" {
		user, err := identity.CurrentUser(r.Context(), a.Pool, principal.UserID)
		if err != nil {
			a.loginFailure(w, r, err)
			return
		}
		response = user
	} else {
		limit := 20
		query := r.URL.Query()
		if len(query["limit"]) > 1 || len(query["cursor"]) > 1 {
			writeProblem(w, r, 400, "invalid_request", "Bad Request")
			return
		}
		if value := query.Get("limit"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 1 || parsed > 100 {
				writeProblem(w, r, 400, "invalid_request", "Bad Request")
				return
			}
			limit = parsed
		}
		cursor := uuid.Nil
		if value := query.Get("cursor"); value != "" {
			parsed, err := identifier.Parse(value)
			if err != nil {
				writeProblem(w, r, 400, "invalid_request", "Bad Request")
				return
			}
			cursor = parsed
		}
		page, err := workspace.List(r.Context(), a.Pool, principal.UserID, cursor, limit)
		if err != nil {
			a.loginFailure(w, r, identity.ErrStore)
			return
		}
		response = page
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
