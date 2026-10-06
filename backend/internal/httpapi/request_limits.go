package httpapi

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const MaxJSONBody = 1 << 20
const MaxHeaderBytes = 32 << 10

type headerSizeKey struct{}

func headerSize(r *http.Request) int {
	size := len(r.Host) + len("Host: \r\n")
	for key, values := range r.Header {
		for _, value := range values {
			size += len(key) + len(value) + 4
		}
	}
	return size
}

// BoundRequests caps bodies before handlers or database work. Endpoint decoders
// retain smaller limits and reject unknown fields and trailing JSON values.
func BoundRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		size := headerSize(r)
		if original, ok := r.Context().Value(headerSizeKey{}).(int); ok && original > size {
			size = original
		}
		if size > MaxHeaderBytes {
			writeProblem(w, r, 431, "headers_too_large", "Request Header Fields Too Large")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Body != nil {
			if r.ContentLength > MaxJSONBody {
				writeProblem(w, r, 413, "body_too_large", "Content Too Large")
				return
			}
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxJSONBody))
			if err != nil {
				if r.Context().Err() != nil {
					writeProblem(w, r, 408, "request_cancelled", "Request Timeout")
				} else if _, ok := err.(*http.MaxBytesError); ok {
					writeProblem(w, r, 413, "body_too_large", "Content Too Large")
				} else {
					writeProblem(w, r, 400, "invalid_request", "Bad Request")
				}
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		next.ServeHTTP(w, r)
	})
}

type clientIPKey struct{}

func ClientIP(ctx context.Context) netip.Addr {
	ip, _ := ctx.Value(clientIPKey{}).(netip.Addr)
	return ip
}

// Forwarding is only an IP hint from explicitly trusted peers. It never changes
// Host, scheme, configured Origin or OIDC callback policy. Walk from the right
// so an untrusted client cannot prepend an authoritative address.
func WithClientIP(next http.Handler, trusted []netip.Prefix) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originalSize := headerSize(r)
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		ip, _ := netip.ParseAddr(host)
		isTrusted := func(ip netip.Addr) bool {
			for _, prefix := range trusted {
				if prefix.Contains(ip.Unmap()) {
					return true
				}
			}
			return false
		}
		if isTrusted(ip) {
			chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
			if len(r.Header.Values("X-Forwarded-For")) == 1 && len(chain) <= 32 {
				candidate := ip
				for i := len(chain) - 1; i >= 0 && isTrusted(candidate); i-- {
					parsed, err := netip.ParseAddr(strings.TrimSpace(chain[i]))
					if err != nil {
						candidate = ip
						break
					}
					candidate = parsed
				}
				ip = candidate
			}
		}
		for _, key := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP"} {
			r.Header.Del(key)
		}
		ctx := context.WithValue(r.Context(), headerSizeKey{}, originalSize)
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, clientIPKey{}, ip)))
	})
}
