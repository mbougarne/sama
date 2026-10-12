package contabo

import (
	"context"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

const tokenURL = "https://auth.contabo.com/auth/realms/contabo/protocol/openid-connect/token"

type accessToken struct {
	value   string
	epoch   uint64
	renewAt time.Time
}

// tokens belongs to exactly one Client/binding/credential version. A gate lets
// canceled waiters exit without spawning goroutines or a generic token cache.
type tokens struct {
	gate       chan struct{}
	current    accessToken
	credential connection.Credential
	http       *http.Client
	now        func() time.Time
}

func newTokens(c connection.Credential, httpClient *http.Client) *tokens {
	return &tokens{gate: make(chan struct{}, 1), credential: c, http: httpClient, now: time.Now}
}

func (t *tokens) get(ctx context.Context, rejected *accessToken) (accessToken, error) {
	select {
	case t.gate <- struct{}{}:
	case <-ctx.Done():
		return accessToken{}, provider.ErrBinding
	}
	defer func() { <-t.gate }()
	if rejected == nil && t.current.value != "" && t.now().Before(t.current.renewAt) {
		return t.current, nil
	}
	if rejected != nil && t.current.value != "" && t.current.epoch != rejected.epoch {
		return t.current, nil
	}
	nextEpoch := t.current.epoch + 1
	t.current = accessToken{epoch: nextEpoch}
	form := url.Values{"grant_type": {"password"}, "client_id": {t.credential.ClientID}, "client_secret": {t.credential.ClientSecret}, "username": {t.credential.Username}, "password": {t.credential.Password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return accessToken{}, provider.ErrBinding
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var result struct {
		Access  string `json:"access_token"`
		Expires int64  `json:"expires_in"`
		Type    string `json:"token_type"`
	}
	if err = outbound.RequestJSON(ctx, t.http, req, &result); err != nil {
		return accessToken{}, err
	}
	if result.Type != "Bearer" && result.Type != "bearer" || len(result.Access) == 0 || len(result.Access) > 16384 || strings.ContainsAny(result.Access, "\r\n\x00") || result.Expires < 30 || result.Expires > 86400 {
		return accessToken{}, provider.InvalidResponse()
	}
	lifetime := time.Duration(result.Expires) * time.Second
	margin := min(30*time.Second, lifetime/10)
	jitter := time.Duration(rand.Int64N(int64(margin/2) + 1))
	t.current = accessToken{value: result.Access, epoch: nextEpoch, renewAt: t.now().Add(lifetime - margin - jitter)}
	return t.current, nil
}

type unauthorizedTransport struct {
	base   http.RoundTripper
	denied *bool
}

func (t unauthorizedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err == nil && response.StatusCode == http.StatusUnauthorized {
		*t.denied = true
	}
	return response, err
}
