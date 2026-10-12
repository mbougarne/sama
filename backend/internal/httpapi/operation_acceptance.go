package httpapi

import (
	"encoding/json"
	"net/http"

	"sama/backend/internal/identifier"
	"sama/backend/internal/operation"
)

func (a *Auth) operationAcceptance(w http.ResponseWriter, r *http.Request) bool {
	m, ok := CurrentMembership(r.Context())
	if !ok || r.URL.Path != "/api/v1/workspaces/"+m.WorkspaceID.String()+"/operations" {
		return false
	}
	if r.Method != http.MethodPost {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return true
	}
	if r.URL.RawQuery != "" || len(r.Header.Values("Idempotency-Key")) != 1 || !operation.ValidKey(r.Header.Get("Idempotency-Key")) {
		writeProblem(w, r, 400, "invalid_request", "Bad Request")
		return true
	}
	var input struct {
		ReviewID    string `json:"review_id"`
		Confirmed   bool   `json:"confirmed"`
		TypedTarget string `json:"typed_target,omitempty"`
	}
	if !a.decodeMutation(w, r, &input) {
		return true
	}
	id, err := identifier.Parse(input.ReviewID)
	if err != nil {
		writeProblem(w, r, 400, "invalid_request", "Bad Request")
		return true
	}
	p, _ := CurrentPrincipal(r.Context())
	result, err := a.Operations.Confirm(r.Context(), p, m.WorkspaceID, r.Header.Get("Idempotency-Key"), operation.Confirmation{ReviewID: id, Confirmed: input.Confirmed, TypedTarget: input.TypedTarget}, RequestID(r.Context()))
	if err != nil {
		a.operationFailure(w, r, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", "/api/v1/workspaces/"+m.WorkspaceID.String()+"/operations/"+result.ID.String())
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(result)
	return true
}
