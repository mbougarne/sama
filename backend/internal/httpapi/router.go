// Package httpapi owns Sama's browser-facing HTTP transport.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-ID"

type contextKey uint8

const requestIDKey contextKey = iota

type problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
}

// NewHandler creates the current liveness and error boundary. No unknown path
// can fall through to frontend HTML, and error text is never reflected.
func NewHandler() http.Handler {
	return withRequestID(http.HandlerFunc(route))
}

func route(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/health" {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			writeProblem(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "Method Not Allowed")
			return
		}
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Cache-Control", "no-store")
		_, _ = response.Write([]byte("{\"status\":\"ok\"}\n"))
		return
	}
	writeProblem(response, request, http.StatusNotFound, "not_found", "Not Found")
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestID := request.Header.Get(requestIDHeader)
		if !validRequestID(requestID) {
			requestID = newRequestID()
		}
		response.Header().Set(requestIDHeader, requestID)
		request = request.WithContext(context.WithValue(request.Context(), requestIDKey, requestID))
		next.ServeHTTP(response, request)
	})
}

func validRequestID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') &&
			!(character >= 'A' && character <= 'Z') &&
			!(character >= '0' && character <= '9') &&
			!strings.ContainsRune("-._", character) {
			return false
		}
	}
	return true
}

// RequestID returns the validated request correlation value installed by the
// HTTP boundary, or an empty string when called outside a request handler.
func RequestID(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey).(string)
	return requestID
}

func newRequestID() string {
	value, err := uuid.NewRandom()
	if err != nil {
		return "request-id-unavailable"
	}
	return value.String()
}

func writeProblem(response http.ResponseWriter, request *http.Request, status int, code, title string) {
	requestID := RequestID(request.Context())
	response.Header().Set("Content-Type", "application/problem+json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(problem{
		Type:      "about:blank",
		Title:     title,
		Status:    status,
		Code:      code,
		RequestID: requestID,
	})
}
