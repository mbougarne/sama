package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sama/backend/internal/audit"
	"sama/backend/internal/identity"
	"sama/backend/internal/platform"
	"sama/backend/internal/workspace"
)

func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return
	}
	cookie, err := r.Cookie(a.cookie("login", "", 0).Name)
	http.SetCookie(w, a.cookie("login", "", -1))
	query := r.URL.Query()
	if err != nil || len(query["state"]) != 1 || len(query["code"]) != 1 || query.Get("error") != "" || len(query.Get("code")) == 0 || len(query.Get("code")) > 4096 {
		writeProblem(w, r, 401, "unauthenticated", "Unauthorized")
		return
	}
	challenge, err := a.Challenges.Consume(r.Context(), query.Get("state"), cookie.Value)
	if err != nil {
		a.loginFailure(w, r, err)
		return
	}
	token, err := a.OIDC.Exchange(r.Context(), query.Get("code"), challenge.Verifier, challenge.Nonce)
	if err != nil {
		a.loginFailure(w, r, err)
		return
	}
	tx, err := platform.BeginTx(r.Context(), a.Pool, pgx.TxOptions{})
	if err != nil {
		a.loginFailure(w, r, identity.ErrStore)
		return
	}
	defer tx.Rollback(r.Context())
	if len(challenge.InvitationDigest) > 0 {
		err := workspace.RedeemInvitation(r.Context(), tx, challenge.InvitationDigest, token.Issuer, token.Subject, RequestID(r.Context()), time.Now())
		if err != nil {
			if errors.Is(err, workspace.ErrStore) {
				err = identity.ErrStore
			}
			a.loginFailure(w, r, err)
			return
		}
	}
	var user, workspace uuid.UUID
	err = tx.QueryRow(r.Context(), `SELECT u.id,m.workspace_id FROM users u
 JOIN memberships m ON m.user_id=u.id JOIN workspaces w ON w.id=m.workspace_id
 WHERE u.issuer=$1 AND u.subject=$2 AND u.disabled_at IS NULL AND w.closed_at IS NULL
 ORDER BY m.workspace_id LIMIT 1 FOR SHARE OF u,m,w`, token.Issuer, token.Subject).Scan(&user, &workspace)
	if errors.Is(err, pgx.ErrNoRows) {
		a.loginFailure(w, r, identity.ErrUnauthenticated)
		return
	}
	if err != nil {
		a.loginFailure(w, r, identity.ErrStore)
		return
	}
	authenticated := token.IssuedAt
	var claims struct {
		AuthTime int64 `json:"auth_time"`
	}
	if err := token.Claims(&claims); err != nil {
		a.loginFailure(w, r, identity.ErrUnauthenticated)
		return
	}
	if claims.AuthTime > 0 {
		authenticated = time.Unix(claims.AuthTime, 0)
	}
	previous := ""
	if old, err := r.Cookie(a.cookie("session", "", 0).Name); err == nil {
		previous = old.Value
	}
	var credential identity.Credential
	if len(challenge.SessionDigest) > 0 {
		authenticated, err = a.OIDC.AuthenticationTime(token, challenge.StartedAt)
		if err != nil {
			a.loginFailure(w, r, err)
			return
		}
		credential, err = a.Sessions.RotateRecent(r.Context(), tx, user, authenticated, previous, challenge.SessionDigest)
	} else {
		credential, err = a.Sessions.Create(r.Context(), tx, user, authenticated, previous)
	}
	if err != nil {
		a.loginFailure(w, r, err)
		return
	}
	eventID, err := audit.NewID()
	if err != nil {
		a.loginFailure(w, r, identity.ErrStore)
		return
	}
	if err := audit.Append(r.Context(), tx, audit.Event{ID: eventID, Workspace: workspace, ActorKind: audit.ActorUser, ActorID: &user, Type: "auth.login", RequestID: RequestID(r.Context())}); err != nil {
		a.loginFailure(w, r, identity.ErrStore)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		a.loginFailure(w, r, identity.ErrStore)
		return
	}
	http.SetCookie(w, a.cookie("session", credential.Token, a.Sessions.CookieMaxAge()))
	http.SetCookie(w, a.csrfCookie(credential.CSRF, a.Sessions.CookieMaxAge()))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *Auth) loginFailure(w http.ResponseWriter, r *http.Request, err error) {
	status, code := 401, "unauthenticated"
	if errors.Is(err, identity.ErrReauthUnsupported) {
		writeProblem(w, r, 403, "reauthentication_unsupported", "Issuer reauthentication policy is not configured or supported")
		return
	}
	if errors.Is(err, identity.ErrRecentRequired) {
		writeProblem(w, r, 403, "recent_authentication_required", "Recent authentication required")
		return
	}
	if errors.Is(err, identity.ErrStore) {
		status, code = 503, "identity_unavailable"
	}
	writeProblem(w, r, status, code, http.StatusText(status))
}
