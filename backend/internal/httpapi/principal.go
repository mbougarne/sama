package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"sama/backend/internal/identifier"
	"sama/backend/internal/identity"
	"sama/backend/internal/workspace"
)

type authKey uint8

const (
	principalKey authKey = iota
	membershipKey
)

func CurrentPrincipal(ctx context.Context) (identity.Principal, bool) {
	p, ok := ctx.Value(principalKey).(identity.Principal)
	return p, ok
}
func CurrentMembership(ctx context.Context) (workspace.Membership, bool) {
	m, ok := ctx.Value(membershipKey).(workspace.Membership)
	return m, ok
}

// Protect supplies current identity and, for tenant routes, current membership.
// The transport maps absent scope to 404 and known-scope role denial to 403.
func (a *Auth) Protect(next http.Handler, minimumRole string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		cookie, err := r.Cookie(a.cookie("session", "", 0).Name)
		if err != nil {
			a.loginFailure(w, r, identity.ErrUnauthenticated)
			return
		}
		p, err := a.Sessions.Resolve(ctx, cookie.Value)
		if err != nil {
			a.loginFailure(w, r, err)
			return
		}
		ctx = context.WithValue(ctx, principalKey, p)
		if path, tenant := strings.CutPrefix(r.URL.Path, "/api/v1/workspaces/"); tenant && path != "" {
			idText, _, _ := strings.Cut(path, "/")
			id, err := identifier.Parse(idText)
			if err != nil {
				writeProblem(w, r, 400, "invalid_request", "Bad Request")
				return
			}
			member, err := workspace.ResolveMembership(ctx, a.Pool, p.UserID, id, minimumRole)
			if err != nil {
				status, code := 503, "identity_unavailable"
				if errors.Is(err, workspace.ErrNotFound) {
					status, code = 404, "not_found"
				}
				if errors.Is(err, workspace.ErrDenied) {
					status, code = 403, "permission_denied"
				}
				writeProblem(w, r, status, code, http.StatusText(status))
				return
			}
			ctx = context.WithValue(ctx, membershipKey, member)
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
