package httpapi

import (
	"net/http"
	"sama/backend/internal/identifier"
	"sama/backend/internal/workspace"
)

func (a *Auth) transferOwnership(w http.ResponseWriter, r *http.Request, m workspace.Membership) {
	if r.Method != http.MethodPost {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return
	}
	var input struct {
		UserID        string `json:"user_id"`
		ActorVersion  int64  `json:"actor_version"`
		TargetVersion int64  `json:"target_version"`
		Demote        bool   `json:"demote"`
	}
	if !a.decodeMutation(w, r, &input) {
		return
	}
	target, err := identifier.Parse(input.UserID)
	if err != nil {
		a.memberFailure(w, r, workspace.ErrInvalid)
		return
	}
	if err := workspace.Transfer(r.Context(), a.Pool, m.UserID, m.WorkspaceID, target, input.ActorVersion, input.TargetVersion, input.Demote, RequestID(r.Context())); err != nil {
		a.memberFailure(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
