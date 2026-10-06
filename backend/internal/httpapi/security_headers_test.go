package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersCoverErrorsAndSuccess(t *testing.T) {
	for _, path := range []string{"/health", "/api/missing", "/auth/missing", "/"} {
		w := httptest.NewRecorder()
		NewHandler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		policy := w.Header().Get("Content-Security-Policy")
		for _, directive := range []string{"default-src 'none'", "script-src 'self'", "style-src 'self'", "object-src 'none'", "frame-src 'none'", "frame-ancestors 'none'", "base-uri 'none'", "form-action 'self'"} {
			if !strings.Contains(policy, directive) {
				t.Fatal("missing directive", directive, path)
			}
		}
		for _, unsafe := range []string{"unsafe-inline", "unsafe-eval", "https:", "*", "data:"} {
			if strings.Contains(policy, unsafe) {
				t.Fatal("unsafe source", unsafe)
			}
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatal("missing protection")
		}
	}
}
