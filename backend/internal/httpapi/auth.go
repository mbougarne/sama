package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"sama/backend/internal/identity"
)

// Auth is installation configuration injected by the composition root.
type Auth struct {
	Pool       *pgxpool.Pool
	Sessions   *identity.Sessions
	OIDC       *identity.OIDC
	Challenges *identity.Challenges
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return
	}
	if r.URL.RawQuery != "" {
		writeProblem(w, r, 400, "invalid_request", "Bad Request")
		return
	}
	var challenge identity.Challenge
	var err error
	if r.Method == http.MethodPost {
		media, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mediaErr != nil || media != "application/json" || r.Header.Get("Origin") != strings.TrimSuffix(a.OIDC.Config.PublicOrigin, "/") {
			writeProblem(w, r, 403, "permission_denied", "Forbidden")
			return
		}
		var input struct {
			Invitation string `json:"invitation"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			writeProblem(w, r, 400, "invalid_request", "Bad Request")
			return
		}
		challenge, err = a.Challenges.StartInvitation(r.Context(), input.Invitation)
	} else {
		challenge, err = a.Challenges.Start(r.Context())
	}
	if err != nil {
		status, code := 503, "identity_unavailable"
		if errors.Is(err, identity.ErrUnauthenticated) {
			status, code = 400, "invalid_request"
		}
		if errors.Is(err, identity.ErrRateLimited) {
			status, code = 429, "rate_limited"
			w.Header().Set("Retry-After", "60")
		}
		writeProblem(w, r, status, code, http.StatusText(status))
		return
	}
	http.SetCookie(w, a.cookie("login", challenge.Browser, 300))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, a.OIDC.OAuth.AuthCodeURL(challenge.State, oidc.Nonce(challenge.Nonce), oauth2.S256ChallengeOption(challenge.Verifier)), http.StatusFound)
}

func (a *Auth) cookie(kind, value string, maxAge int) *http.Cookie {
	prefix := "__Host-sama_"
	if a.OIDC.Config.Development {
		prefix = "sama_"
	}
	return &http.Cookie{Name: prefix + kind, Value: value, Path: "/", Secure: !a.OIDC.Config.Development, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge}
}
