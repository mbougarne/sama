package httpapi

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sama/backend/internal/identity"
	"strings"
	"testing"
	"time"
)

func TestRequestLimits(t *testing.T) {
	handler := withRequestID(BoundRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })))
	for _, tc := range []struct {
		size   int
		length int64
		want   int
	}{{MaxJSONBody, MaxJSONBody, 204}, {MaxJSONBody + 1, MaxJSONBody + 1, 413}, {MaxJSONBody + 1, -1, 413}} {
		r := httptest.NewRequest("POST", "/api/example", strings.NewReader(strings.Repeat("x", tc.size)))
		r.ContentLength = tc.length
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("size %d length %d: %d", tc.size, tc.length, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/health", nil)
	r.Header.Set("Large", strings.Repeat("x", MaxHeaderBytes))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 431 {
		t.Fatal(w.Code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = httptest.NewRequest("POST", "/api/example", nil).WithContext(ctx)
	r.Body = failedBody{}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 408 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal(w.Body.String())
	}
}

type failedBody struct{}

func (failedBody) Read([]byte) (int, error) { return 0, errors.New("secret read failure") }
func (failedBody) Close() error             { return nil }

func TestTrustedForwardingNeverChangesOrigin(t *testing.T) {
	for _, tc := range []struct{ peer, forward, want string }{
		{"198.51.100.2:123", "192.0.2.1", "198.51.100.2"},
		{"127.0.0.1:123", "192.0.2.1, 127.0.0.2", "192.0.2.1"},
		{"127.0.0.1:123", "192.0.2.1, 198.51.100.3", "198.51.100.3"},
		{"127.0.0.1:123", "malformed", "127.0.0.1"},
	} {
		h := WithClientIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ClientIP(r.Context()).String() != tc.want {
				t.Fatal(ClientIP(r.Context()), tc.want)
			}
			if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Forwarded-Host") != "" || r.Host != "sama.example" || r.Header.Get("Origin") != "https://evil.example" {
				t.Fatal("forwarded authority leaked")
			}
		}), []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
		r := httptest.NewRequest("GET", "https://sama.example/health", nil)
		r.RemoteAddr = tc.peer
		r.Header.Set("X-Forwarded-For", tc.forward)
		r.Header.Set("X-Forwarded-Host", "trusted.example")
		r.Header.Set("Origin", "https://evil.example")
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
}

func TestNetworkHeaderAndSlowClientLimits(t *testing.T) {
	server := httptest.NewUnstartedServer(NewHandler())
	server.Config.MaxHeaderBytes = MaxHeaderBytes
	server.Config.ReadHeaderTimeout = 50 * time.Millisecond
	server.Start()
	defer server.Close()
	for _, oversized := range []bool{false, true} {
		conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		if oversized {
			fmt.Fprintf(conn, "GET /health HTTP/1.1\r\nHost: localhost\r\nLarge: %s\r\n\r\n", strings.Repeat("x", MaxHeaderBytes+8192))
			response, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 431 {
				t.Fatal(response.StatusCode)
			}
		} else {
			fmt.Fprint(conn, "GET /health HTTP/1.1\r\nHost:")
			_, err := conn.Read(make([]byte, 1))
			if err != io.EOF {
				t.Fatalf("slow client was not closed: %v", err)
			}
		}
		conn.Close()
	}
}

func TestLogoutRejectsUnknownAndTrailingFields(t *testing.T) {
	auth := &Auth{OIDC: &identity.OIDC{Config: identity.OIDCConfig{PublicOrigin: "https://sama.example"}}}
	for _, tc := range []struct {
		body string
		want int
	}{{"", 204}, {"{}", 204}, {`{"unknown":"secret"}`, 400}, {"{} {}", 400}, {"null", 400}} {
		r := httptest.NewRequest("POST", "/auth/logout", strings.NewReader(tc.body))
		r.Header.Set("Origin", "https://sama.example")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		NewHandler(auth).ServeHTTP(w, r)
		if w.Code != tc.want || strings.Contains(w.Body.String(), "secret") {
			t.Fatal(tc.body, w.Code)
		}
	}
}
