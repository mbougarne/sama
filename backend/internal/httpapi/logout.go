package httpapi

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"strings"
	"time"

	"sama/backend/internal/identity"
)

// Mutation requests require the configured origin and session-bound CSRF.
func (a *Auth) validMutation(r *http.Request, p identity.Principal) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && media == "application/json" && r.Header.Get("Origin") == strings.TrimSuffix(a.OIDC.Config.PublicOrigin, "/") && identity.ValidCSRF(p, r.Header.Get("X-CSRF-Token"))
}

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if r.Method != http.MethodPost {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return
	}
	if r.Header.Get("Origin") == "" || r.Header.Get("Origin") != strings.TrimSuffix(a.OIDC.Config.PublicOrigin, "/") {
		writeProblem(w, r, 403, "permission_denied", "Forbidden")
		return
	}
	cookie, err := r.Cookie(a.cookie("session", "", 0).Name)
	if err == nil {
		principal, err := a.Sessions.Resolve(r.Context(), cookie.Value)
		if err != nil && !errors.Is(err, identity.ErrUnauthenticated) {
			a.loginFailure(w, r, err)
			return
		}
		if err == nil {
			if !a.validMutation(r, principal) {
				writeProblem(w, r, 403, "permission_denied", "Forbidden")
				return
			}
			if err := a.Sessions.Logout(r.Context(), cookie.Value, RequestID(r.Context())); err != nil {
				a.loginFailure(w, r, err)
				return
			}
		}
	}
	http.SetCookie(w, a.cookie("session", "", -1))
	http.SetCookie(w, a.csrfCookie("", -1))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (a *Auth) csrfCookie(value string, maxAge int) *http.Cookie {
	c := a.cookie("csrf", value, maxAge)
	c.HttpOnly = false
	return c
}
