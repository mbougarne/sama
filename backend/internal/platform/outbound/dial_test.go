package outbound

import (
	"context"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

type resolveFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolveFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

func TestUnsafeDNSAndPinnedDial(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.1.1", "172.16.0.1", "192.168.1.1", "169.254.169.254", "0.0.0.0", "224.0.0.1", "100.100.100.200", "::1", "fc00::1", "fe80::1", "ff02::1", "::", "::ffff:127.0.0.1", "64:ff9b::7f00:1", "2002:7f00:1::"} {
		called := false
		resolve := resolveFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr(ip)}, nil
		})
		_, err := safeDial(resolve, func(context.Context, string, string) (net.Conn, error) { called = true; return nil, nil }, "provider.example")(context.Background(), "tcp", "provider.example:443")
		if err != ErrNetwork || called {
			t.Fatal("unsafe mixed DNS", ip)
		}
	}
	lookups := 0
	resolve := resolveFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		lookups++
		if lookups > 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	})
	client, server := net.Pipe()
	defer server.Close()
	dial := safeDial(resolve, func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "8.8.8.8:443" {
			t.Fatal("hostname redialed", address)
		}
		return client, nil
	}, "provider.example")
	conn, err := dial(context.Background(), "tcp", "provider.example:443")
	if err != nil || lookups != 1 {
		t.Fatal("pinned dial", err)
	}
	conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dial(ctx, "tcp", "provider.example:443"); err != ErrNetwork {
		t.Fatal("cancelled call")
	}
	transport := HetznerCloud.Client().Transport.(confinedTransport).transport.(*http.Transport)
	if transport.Proxy != nil || transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.ServerName != "api.hetzner.cloud" {
		t.Fatal("TLS/proxy policy")
	}
}

func TestTLSVerificationAndCancellation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	profile := Profile{"example.com", "/v1"}
	client := profile.Client()
	transport := client.Transport.(confinedTransport).transport.(*http.Transport)
	defer transport.CloseIdleConnections()
	transport.TLSClientConfig.RootCAs = roots
	resolve := resolveFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	})
	transport.DialContext = safeDial(resolve, func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "8.8.8.8:443" {
			t.Fatal(address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}, "example.com")
	response, err := client.Get("https://example.com/v1")
	if err != nil {
		t.Fatal("verified TLS", err)
	}
	response.Body.Close()
	transport.CloseIdleConnections()
	transport.TLSClientConfig.ServerName = "wrong.example"
	if response, err := client.Get("https://example.com/v1"); err == nil {
		response.Body.Close()
		t.Fatal("wrong TLS identity accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", "https://example.com/v1", nil)
	if response, err := client.Do(request); err == nil {
		response.Body.Close()
		t.Fatal("cancelled request sent")
	}
}
