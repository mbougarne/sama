package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"sama/backend/internal/identifier"
	"sama/backend/internal/inventory"
	"sama/backend/internal/workspace"
)

func (a *Auth) resourceList(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok || r.URL.Path != "/api/v1/workspaces/"+m.WorkspaceID.String()+"/resources" {
		return false
	}
	if r.Method != http.MethodGet {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return true
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	invalid := err != nil
	for key, values := range q {
		switch key {
		case "limit", "cursor", "connection_id", "type", "region", "account", "provider":
		default:
			invalid = true
		}
		invalid = invalid || len(values) != 1 || values[0] == ""
	}
	limit := 50
	if value := q.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		invalid = invalid || err != nil || limit < 1 || limit > 200
	}
	filters := inventory.Filters{Type: q.Get("type"), Region: q.Get("region"), Account: q.Get("account"), Provider: q.Get("provider")}
	if value := q.Get("connection_id"); value != "" {
		filters.Connection, err = identifier.Parse(value)
		invalid = invalid || err != nil
	}
	if invalid {
		a.memberFailure(w, r, workspace.ErrInvalid)
		return true
	}
	result, err := inventory.List(r.Context(), a.Pool, m.UserID, m.WorkspaceID, filters, q.Get("cursor"), limit)
	if err != nil {
		a.memberFailure(w, r, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
	return true
}
