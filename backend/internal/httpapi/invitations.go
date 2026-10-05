package httpapi

import (
	"encoding/json"
	"net/http"
	"sama/backend/internal/workspace"
	"time"
)

func (a *Auth) issueInvitation(w http.ResponseWriter, r *http.Request, m workspace.Membership) {
	if r.Method != http.MethodPost {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return
	}
	var input struct {
		Subject string `json:"subject"`
		Role    string `json:"role"`
	}
	if !a.decodeMutation(w, r, &input) {
		return
	}
	proof, err := workspace.IssueInvitation(r.Context(), a.Pool, m.UserID, m.WorkspaceID, a.OIDC.Config.Issuer, input.Subject, input.Role, RequestID(r.Context()), time.Now())
	if err != nil {
		a.memberFailure(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(struct {
		Token string `json:"token"`
	}{proof})
}
