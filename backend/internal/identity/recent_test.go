package identity

import (
	"context"
	"testing"
	"time"
)

func TestSignedRecentAuthenticationPolicy(t *testing.T) {
	server, key := syntheticIssuer(t)
	now := time.Now().Truncate(time.Second)
	config := OIDCConfig{Issuer: server.URL, ClientID: "fixture", PublicOrigin: "http://127.0.0.1:8080", Development: true, ReauthACR: "fixture-mfa"}
	o, err := NewOIDC(context.Background(), config, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, acr string
		auth      int64
		want      bool
	}{
		{"fresh", "fixture-mfa", now.Unix(), true},
		{"missing", "fixture-mfa", 0, false},
		{"stale", "fixture-mfa", now.Add(-5 * time.Minute).Unix(), false},
		{"predates challenge", "fixture-mfa", now.Add(-time.Second).Unix(), false},
		{"future", "fixture-mfa", now.Add(time.Second).Unix(), false},
		{"wrong policy", "other", now.Unix(), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := map[string]any{"iss": server.URL, "sub": "fixture", "aud": "fixture", "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "auth_time": test.auth, "acr": test.acr}
			token, err := o.Verify(context.Background(), signedToken(t, key, claims))
			if err != nil {
				t.Fatal(err)
			}
			_, err = o.AuthenticationTime(token, now)
			if (err == nil) != test.want {
				t.Fatal("authentication policy", err)
			}
		})
	}
	config.ReauthACR = "unsupported"
	unsupported, err := NewOIDC(context.Background(), config, time.Now)
	if err != nil || unsupported.ReauthenticationSupported {
		t.Fatal("unsupported issuer policy enabled")
	}
	if _, err := unsupported.AuthenticationTime(nil, now); err != ErrReauthUnsupported {
		t.Fatal("missing policy explanation")
	}
}
