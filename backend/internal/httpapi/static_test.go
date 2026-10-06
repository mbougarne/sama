package httpapi

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticRoutesConfinementAndCaching(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{"index.html": "<html>entry</html>", "main.0123456789abcdefabcd.js": "safe asset", "plain.css": "body {}", "secret.txt": "private sentinel"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.js")
	if err := os.WriteFile(outside, []byte("outside sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.js")); err != nil {
		t.Fatal(err)
	}
	assets, err := OpenAssets(root)
	if err != nil {
		t.Fatal(err)
	}
	defer assets.Close()
	handler := NewAppHandler(nil, assets)
	for _, tc := range []struct {
		url            string
		status         int
		cache, content string
	}{
		{"/", 200, "no-store", "entry"}, {"/workspaces/example/resources", 200, "no-store", "entry"},
		{"/main.0123456789abcdefabcd.js", 200, "public, max-age=31536000, immutable", "safe asset"},
		{"/plain.css", 200, "no-cache", "body"},
		{"/missing.js", 404, "no-store", "not_found"}, {"/api/missing", 404, "no-store", "not_found"},
		{"/api", 404, "no-store", "not_found"}, {"/auth/missing", 404, "no-store", "not_found"},
		{"/escape.js", 404, "no-store", "not_found"}, {"/secret.txt", 404, "no-store", "not_found"},
		{"/../outside.js", 404, "no-store", "not_found"}, {"/%2e%2e/outside.js", 404, "no-store", "not_found"},
		{"/.hidden", 404, "no-store", "not_found"},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", tc.url, nil))
		if w.Code != tc.status || w.Header().Get("Cache-Control") != tc.cache || !strings.Contains(w.Body.String(), tc.content) {
			t.Fatalf("%s: %d %s %s", tc.url, w.Code, w.Header(), w.Body.String())
		}
		if strings.Contains(w.Body.String(), "sentinel") {
			t.Fatal("file escaped")
		}
		if w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing CSP")
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
	if a, err := OpenAssets(t.TempDir()); err == nil {
		a.Close()
		t.Fatal("missing index accepted")
	}
}
