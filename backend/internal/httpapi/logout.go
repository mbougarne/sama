package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"sama/backend/internal/identity"
)

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if r.Method != http.MethodPost {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return
	}
	if !a.validOriginJSON(r) {
		writeProblem(w, r, 403, "permission_denied", "Forbidden")
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	var input *struct{}
	// Keep empty-body logout compatibility, but never ignore supplied fields.
	err := decoder.Decode(&input)
	if err != io.EOF && (err != nil || input == nil || decoder.Decode(new(any)) != io.EOF) {
		writeProblem(w, r, 400, "invalid_request", "Bad Request")
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
