// Package outbound confines credential-bearing provider HTTP traffic.
package outbound

import (
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

var ErrEndpoint = errors.New("provider endpoint rejected")

// Profile is selected by trusted compiled adapter code, never browser input.
// Constructing a profile or client does not qualify or enable an adapter.
type Profile struct {
	host   string
	prefix string
}

var HetznerCloud = Profile{host: "api.hetzner.cloud", prefix: "/v1"}

func (p Profile) validate(u *url.URL) error {
	if u == nil || p.host == "" || p.prefix == "" || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Fragment != "" || u.Hostname() != p.host || u.Port() != "" && u.Port() != "443" {
		return ErrEndpoint
	}
	if u.RawPath != "" || strings.ContainsAny(u.Path, "\\\x00") || path.Clean(u.Path) != u.Path || u.Path != p.prefix && !strings.HasPrefix(u.Path, p.prefix+"/") {
		return ErrEndpoint
	}
	return nil
}

func (p Profile) URL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || p.validate(u) != nil {
		return nil, ErrEndpoint
	}
	return u, nil
}

type confinedTransport struct {
	profile   Profile
	transport http.RoundTripper
}

func (t confinedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.profile.validate(r.URL) != nil || r.Host != "" && r.Host != r.URL.Host {
		return nil, ErrEndpoint
	}
	return t.transport.RoundTrip(r)
}

func (p Profile) client(transport http.RoundTripper) *http.Client {
	return &http.Client{Transport: confinedTransport{p, transport}, Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrEndpoint }}
}
