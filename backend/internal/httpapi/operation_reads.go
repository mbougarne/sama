package httpapi

import (
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"net/url"
	"sama/backend/internal/identifier"
	"sama/backend/internal/operation"
	"sama/backend/internal/workspace"
	"strconv"
	"strings"
)

func (a *Auth) operationReads(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok {
		return false
	}
	suffix, ok := strings.CutPrefix(r.URL.Path, "/api/v1/workspaces/"+m.WorkspaceID.String()+"/operations")
	if !ok || suffix != "" && (!strings.HasPrefix(suffix, "/") || strings.Contains(suffix[1:], "/")) {
		return false
	}
	if suffix == "" && r.Method == http.MethodPost {
		return false
	}
	if r.Method != http.MethodGet {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return true
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	invalid := err != nil
	for k, v := range q {
		invalid = invalid || (k != "limit" && k != "cursor") || len(v) != 1 || v[0] == ""
	}
	limit := 50
	target := uuid.Nil
	if q.Get("limit") != "" {
		limit, err = strconv.Atoi(q.Get("limit"))
		invalid = invalid || err != nil || limit < 1 || limit > 200
	}
	if suffix != "" {
		target, err = identifier.Parse(suffix[1:])
		invalid = invalid || err != nil || len(q) > 0
		limit = 1
	}
	if invalid {
		a.memberFailure(w, r, workspace.ErrInvalid)
		return true
	}
	result, err := operation.Read(r.Context(), a.Pool, m.UserID, m.WorkspaceID, target, q.Get("cursor"), limit)
	if err != nil {
		a.memberFailure(w, r, err)
		return true
	}
	var value any = result
	if target != uuid.Nil {
		value = result.Data[0]
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
	return true
}
