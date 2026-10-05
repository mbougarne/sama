package httpapi

import (
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"net/http"
	"sama/backend/internal/identity"
)

func (a *Auth) reauthenticate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return
	}
	var input struct{}
	if !a.decodeMutation(w, r, &input) {
		return
	}
	if !a.OIDC.ReauthenticationSupported {
		a.loginFailure(w, r, identity.ErrReauthUnsupported)
		return
	}
	cookie, err := r.Cookie(a.cookie("session", "", 0).Name)
	if err != nil {
		a.loginFailure(w, r, identity.ErrUnauthenticated)
		return
	}
	challenge, err := a.Challenges.StartReauthentication(r.Context(), cookie.Value)
	if err != nil {
		a.loginFailure(w, r, err)
		return
	}
	http.SetCookie(w, a.cookie("login", challenge.Browser, 300))
	http.Redirect(w, r, a.OIDC.OAuth.AuthCodeURL(challenge.State, oidc.Nonce(challenge.Nonce), oauth2.S256ChallengeOption(challenge.Verifier), oauth2.SetAuthURLParam("prompt", "login"), oauth2.SetAuthURLParam("max_age", "0"), oauth2.SetAuthURLParam("acr_values", a.OIDC.Config.ReauthACR)), http.StatusFound)
}
