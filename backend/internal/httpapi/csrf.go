package httpapi

import (
	"mime"
	"net/http"
	"sama/backend/internal/identity"
	"strings"
)

func singleHeader(r *http.Request, name string) string {
	values := r.Header.Values(name)
	if len(values) != 1 {
		return ""
	}
	return values[0]
}
func (a *Auth) validOriginJSON(r *http.Request) bool {
	media, _, err := mime.ParseMediaType(singleHeader(r, "Content-Type"))
	origin := singleHeader(r, "Origin")
	return a.OIDC != nil && origin != "" && origin == strings.TrimSuffix(a.OIDC.Config.PublicOrigin, "/") && err == nil && media == "application/json"
}
func (a *Auth) validMutation(r *http.Request, p identity.Principal) bool {
	return a.validOriginJSON(r) && identity.ValidCSRF(p, singleHeader(r, "X-CSRF-Token"))
}

// Every authenticated browser mutation is covered, including unknown/new routes.
// OIDC login initiation and callback have their separate challenge boundary.
func (a *Auth) requireMutation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			p, ok := CurrentPrincipal(r.Context())
			if !ok || !a.validMutation(r, p) {
				writeProblem(w, r, 403, "permission_denied", "Forbidden")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
