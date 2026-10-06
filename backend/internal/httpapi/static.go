package httpapi

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
)

var hashedAsset = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[0-9a-f]{20}\.(js|css)$`)

type Assets struct{ root *os.Root }

// OpenAssets validates an independently built directory at startup. os.Root
// confines all subsequent opens, including symlink resolution, to that tree.
func OpenAssets(directory string) (*Assets, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, errors.New("frontend assets unavailable")
	}
	file, err := root.Open("index.html")
	if err != nil {
		root.Close()
		return nil, errors.New("frontend index unavailable")
	}
	info, err := file.Stat()
	file.Close()
	if err != nil || !info.Mode().IsRegular() {
		root.Close()
		return nil, errors.New("frontend index unavailable")
	}
	return &Assets{root: root}, nil
}
func (a *Assets) Close() error { return a.root.Close() }
func (a *Assets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeProblem(w, r, 405, "method_not_allowed", "Method Not Allowed")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if strings.Contains(name, "\\") || strings.ContainsRune(name, 0) || (name != "" && path.Clean(name) != name) {
		route(w, r)
		return
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			route(w, r)
			return
		}
	}
	ext := path.Ext(name)
	if name == "" || ext == "" {
		name = "index.html"
		ext = ".html"
	}
	switch ext {
	case ".html", ".js", ".css", ".svg", ".png", ".ico", ".woff2":
	default:
		route(w, r)
		return
	}
	// Only the entry document is an HTML asset; do not expose incidental files.
	if ext == ".html" && name != "index.html" {
		route(w, r)
		return
	}
	file, err := a.root.Open(name)
	if err != nil {
		route(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		route(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-store")
	} else if hashedAsset.MatchString(name) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	w.Header().Set("Content-Type", mime.TypeByExtension(ext))
	http.ServeContent(w, r, name, info.ModTime(), file)
}
