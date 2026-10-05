package identity

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCConfig struct {
	Issuer, ClientID, ClientSecret, PublicOrigin string
	Development                                  bool
	ReauthACR                                    string
}

type OIDC struct {
	Config                    OIDCConfig
	OAuth                     oauth2.Config
	verifier                  *oidc.IDTokenVerifier
	client                    *http.Client
	now                       func() time.Time
	ReauthenticationSupported bool
}

func LoadOIDC(origin string, lookup func(string) (string, bool), read func(string) ([]byte, error)) (OIDCConfig, error) {
	value := func(key string) string { v, _ := lookup(key); return v }
	c := OIDCConfig{Issuer: value("SAMA_OIDC_ISSUER"), ClientID: value("SAMA_OIDC_CLIENT_ID"), PublicOrigin: origin}
	c.ReauthACR = value("SAMA_OIDC_REAUTH_ACR")
	mode := value("SAMA_OIDC_DEVELOPMENT")
	if mode != "" && mode != "true" && mode != "false" {
		return c, errors.New("invalid OIDC configuration")
	}
	c.Development = mode == "true"
	if path := value("SAMA_OIDC_CLIENT_SECRET_FILE"); path != "" {
		contents, err := read(path)
		if err != nil {
			return c, errors.New("OIDC secret file unavailable")
		}
		c.ClientSecret = strings.TrimSpace(string(contents))
	}
	if c.Issuer == "" && c.ClientID == "" && c.ClientSecret == "" && !c.Development && c.ReauthACR == "" {
		return c, nil
	}
	if err := c.validate(); err != nil {
		return c, err
	}
	return c, nil
}

func (c OIDCConfig) validate() error {
	if len(c.ReauthACR) > 255 || strings.ContainsAny(c.ReauthACR, " \t\r\n") {
		return errors.New("invalid OIDC reauthentication policy")
	}
	origin, err := url.Parse(c.PublicOrigin)
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		return errors.New("invalid OIDC configuration")
	}
	if len(c.ClientID) == 0 || len(c.ClientID) > 255 || len(c.Issuer) > 2048 || !oidcURL(c.Issuer, c.Development) || !oidcURL(c.PublicOrigin, c.Development) {
		return errors.New("invalid OIDC configuration")
	}
	if c.Development && (!loopback(origin.Hostname()) || !loopbackURL(c.Issuer)) {
		return errors.New("development OIDC requires loopback endpoints")
	}
	return nil
}

func loopback(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}
func loopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && loopback(u.Hostname())
}
func oidcURL(raw string, development bool) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && u.User == nil && u.Fragment == "" && (!development || loopback(u.Hostname())) && (u.Scheme == "https" || (development && u.Scheme == "http" && loopback(u.Hostname())))
}

type boundedOIDCTransport struct {
	base        http.RoundTripper
	development bool
}

func (t boundedOIDCTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !oidcURL(r.URL.String(), t.development) {
		return nil, errors.New("invalid OIDC endpoint")
	}
	response, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	response.Body = struct {
		io.Reader
		io.Closer
	}{io.LimitReader(response.Body, 1<<20), response.Body}
	return response, nil
}

func NewOIDC(ctx context.Context, c OIDCConfig, now func() time.Time) (*OIDC, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	if now == nil {
		return nil, errors.New("invalid OIDC clock")
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: boundedOIDCTransport{http.DefaultTransport, c.Development}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	discovery, cancel := context.WithTimeout(oidc.ClientContext(ctx, client), 10*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(discovery, c.Issuer)
	if err != nil {
		return nil, errors.New("OIDC discovery unavailable")
	}
	var metadata struct {
		Claims []string `json:"claims_supported"`
		ACRs   []string `json:"acr_values_supported"`
	}
	if err := provider.Claims(&metadata); err != nil {
		return nil, errors.New("invalid OIDC discovery")
	}
	supported := c.ReauthACR != "" && slices.Contains(metadata.Claims, "auth_time") && slices.Contains(metadata.ACRs, c.ReauthACR)
	endpoints := provider.Endpoint()
	if !oidcURL(endpoints.AuthURL, c.Development) || !oidcURL(endpoints.TokenURL, c.Development) {
		return nil, errors.New("invalid OIDC endpoint")
	}
	origin, _ := url.Parse(c.PublicOrigin)
	origin.Path = "/auth/callback"
	return &OIDC{now: now, ReauthenticationSupported: supported, Config: c, OAuth: oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURL: origin.String(), Endpoint: endpoints, Scopes: []string{oidc.ScopeOpenID}}, client: client,
		verifier: provider.VerifierContext(oidc.ClientContext(ctx, client), &oidc.Config{ClientID: c.ClientID, SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256}, Now: now})}, nil
}

func (o *OIDC) Verify(ctx context.Context, raw string) (*oidc.IDToken, error) {
	token, err := o.verifier.Verify(oidc.ClientContext(ctx, o.client), raw)
	if err != nil || token.Subject == "" || len(token.Subject) > 255 {
		return nil, errors.New("OIDC token rejected")
	}
	return token, nil
}

// Exchange binds the code to PKCE and nonce. No access/refresh/ID token is persisted.
func (o *OIDC) Exchange(ctx context.Context, code, verifier, nonce string) (*oidc.IDToken, error) {
	bounded, cancel := context.WithTimeout(oidc.ClientContext(ctx, o.client), 10*time.Second)
	defer cancel()
	response, err := o.OAuth.Exchange(bounded, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, ErrUnauthenticated
	}
	raw, ok := response.Extra("id_token").(string)
	if !ok {
		return nil, ErrUnauthenticated
	}
	token, err := o.Verify(bounded, raw)
	if err != nil || subtle.ConstantTimeCompare([]byte(token.Nonce), []byte(nonce)) != 1 {
		return nil, ErrUnauthenticated
	}
	return token, nil
}
