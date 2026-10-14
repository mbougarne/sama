package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"sama/backend/internal/identifier"
	"sama/backend/internal/operation"
)

func (a *Auth) operationControls(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok {
		return false
	}
	suffix, ok := strings.CutPrefix(r.URL.Path, "/api/v1/workspaces/"+m.WorkspaceID.String()+"/operations/")
	if !ok {
		return false
	}
	parts := strings.Split(suffix, "/")
	if len(parts) != 2 || (parts[1] != "cancel" && parts[1] != "reconciliations" && parts[1] != "reservation-release") {
		return false
	}
	if r.Method != http.MethodPost {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return true
	}
	id, err := identifier.Parse(parts[0])
	if err != nil || r.URL.RawQuery != "" {
		writeProblem(w, r, 400, "invalid_request", "Bad Request")
		return true
	}
	var input operation.Resolution
	if parts[1] == "reservation-release" {
		if !a.decodeMutation(w, r, &input) {
			return true
		}
	} else {
		var empty struct{}
		if !a.decodeMutation(w, r, &empty) {
			return true
		}
	}
	p, _ := CurrentPrincipal(r.Context())
	var result operation.Accepted
	switch parts[1] {
	case "cancel":
		result, err = a.Operations.Cancel(r.Context(), p, m.WorkspaceID, id, RequestID(r.Context()))
	case "reconciliations":
		result, err = a.Operations.Recheck(r.Context(), p, m.WorkspaceID, id, RequestID(r.Context()))
	case "reservation-release":
		result, err = a.Operations.ReleaseUnknown(r.Context(), p, m.WorkspaceID, id, input, RequestID(r.Context()))
	}

	if err != nil {
		a.operationFailure(w, r, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
	return true
}
