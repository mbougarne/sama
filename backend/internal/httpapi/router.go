// Package httpapi owns Sama's browser-facing HTTP transport.
package httpapi

import "net/http"

// NewHandler creates the initial transport. Provider and authenticated routes are
// added only after their identity and authorization boundaries are implemented.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(res http.ResponseWriter, _ *http.Request) {
		res.Header().Set("Content-Type", "application/json")
		res.Header().Set("Cache-Control", "no-store")
		_, _ = res.Write([]byte("{\"status\":\"ok\"}\n"))
	})
	return mux
}
