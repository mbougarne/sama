package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sama/backend/internal/audit"
	"sama/backend/internal/identifier"
	"sama/backend/internal/workspace"
	"strconv"
)

func (a *Auth) auditHistory(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok || r.URL.Path != "/api/v1/workspaces/"+m.WorkspaceID.String()+"/audit-events" {
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
		case "actor_id", "action", "target_id", "cursor", "limit":
		default:
			invalid = true
		}
		invalid = invalid || len(values) != 1 || values[0] == ""
	}
	limit := 50
	if q.Get("limit") != "" {
		limit, err = strconv.Atoi(q.Get("limit"))
		invalid = invalid || err != nil || limit < 1 || limit > 200
	}
	f := audit.Filters{Action: q.Get("action")}
	if q.Get("actor_id") != "" {
		f.Actor, err = identifier.Parse(q.Get("actor_id"))
		invalid = invalid || err != nil
	}
	if q.Get("target_id") != "" {
		f.Target, err = identifier.Parse(q.Get("target_id"))
		invalid = invalid || err != nil
	}
	if invalid {
		a.memberFailure(w, r, workspace.ErrInvalid)
		return true
	}
	result, err := audit.List(r.Context(), a.Pool, m.UserID, m.WorkspaceID, f, q.Get("cursor"), limit)
	if errors.Is(err, audit.ErrReadInvalid) {
		err = workspace.ErrInvalid
	}
	if err != nil {
		a.memberFailure(w, r, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
	return true
}
