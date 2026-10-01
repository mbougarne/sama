package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

func syntheticIssuer(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize", "token_endpoint": server.URL + "/token", "jwks_uri": server.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "synthetic", Algorithm: "RS256", Use: "sig"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, key
}

func signedToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "synthetic"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.Sign(body)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestOIDCVerificationAndConfiguration(t *testing.T) {
	server, key := syntheticIssuer(t)
	now := time.Now().UTC()
	c := OIDCConfig{Issuer: server.URL, ClientID: "sama-fixture", PublicOrigin: "http://127.0.0.1:8080", Development: true}
	o, err := NewOIDC(context.Background(), c, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if o.OAuth.RedirectURL != "http://127.0.0.1:8080/auth/callback" {
		t.Fatal("callback was not derived from configured origin")
	}
	claims := func() map[string]any {
		return map[string]any{"iss": server.URL, "sub": "fixture-user", "aud": "sama-fixture", "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "nonce": "fixture-nonce"}
	}
	if _, err := o.Verify(context.Background(), signedToken(t, key, claims())); err != nil {
		t.Fatal(err)
	}
	for field, bad := range map[string]any{"iss": "https://other.example", "aud": "another-client", "exp": now.Add(-time.Minute).Unix(), "sub": ""} {
		t.Run(field, func(t *testing.T) {
			c := claims()
			c[field] = bad
			if _, err := o.Verify(context.Background(), signedToken(t, key, c)); err == nil {
				t.Fatal("invalid claim accepted")
			}
		})
	}
	_, wrongKey := syntheticIssuer(t)
	if _, err := o.Verify(context.Background(), signedToken(t, wrongKey, claims())); err == nil {
		t.Fatal("wrong signature accepted")
	}
	for _, bad := range []OIDCConfig{
		{Issuer: server.URL, ClientID: "client", PublicOrigin: "https://sama.example"},
		{Issuer: "https://issuer.example", ClientID: "client", PublicOrigin: "http://127.0.0.1:8080", Development: true},
		{Issuer: "https://issuer.example", ClientID: "client", PublicOrigin: "https://sama.example/untrusted"},
	} {
		if bad.validate() == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	empty, err := LoadOIDC("http://127.0.0.1:8080", func(string) (string, bool) { return "", false }, nil)
	if err != nil || empty.Issuer != "" {
		t.Fatal("optional OIDC disabled state")
	}
}
