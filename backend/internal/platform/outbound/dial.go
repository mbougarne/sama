package outbound

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"
)

var ErrNetwork = errors.New("provider network unavailable")

type resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}
type dialer func(context.Context, string, string) (net.Conn, error)

// Only globally routable public destinations are permitted. Special-use IPv4
// and IPv6 ranges are denied, including mapped, translation and transition forms.
var deniedRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range deniedRanges {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func safeDial(resolve resolver, dial dialer, host string) dialer {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		name, port, err := net.SplitHostPort(address)
		if err != nil || name != host || port != "443" || network != "tcp" {
			return nil, ErrNetwork
		}
		addresses, err := resolve.LookupNetIP(ctx, "ip", name)
		if err != nil || len(addresses) == 0 || len(addresses) > 32 {
			return nil, ErrNetwork
		}
		for _, ip := range addresses {
			if !publicAddress(ip) {
				return nil, ErrNetwork
			}
		}
		for _, ip := range addresses {
			if ctx.Err() != nil {
				return nil, ErrNetwork
			}
			conn, err := dial(ctx, "tcp", net.JoinHostPort(ip.Unmap().String(), port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, ErrNetwork
	}
}

// Client never uses environment proxies, alternate TLS dialing or ambient auth.
// TLS verifies the original hostname while DialContext uses a validated IP literal.
func (p Profile) Client() *http.Client {
	dial := (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport := &http.Transport{DialContext: safeDial(net.DefaultResolver, dial, p.host),
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12, ServerName: p.host},
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second,
		MaxResponseHeaderBytes: 32 << 10, MaxIdleConns: 16, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}
	return p.client(transport)
}
