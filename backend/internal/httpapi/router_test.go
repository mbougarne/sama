package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealth(t *testing.T) {
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected health headers: %v", response.Header())
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Status != "ok" {
		t.Fatalf("invalid health response: %q (%v)", response.Body.String(), err)
	}
	if response.Header().Get(requestIDHeader) == "" {
		t.Fatal("health response omitted its request ID")
	}
}

func TestHealthSupportsHead(t *testing.T) {
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, httptest.NewRequest(http.MethodHead, "/health", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("HEAD /health status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected HEAD health headers: %v", response.Header())
	}
}

func TestRouteBoundaries(t *testing.T) {
	for _, tc := range []struct {
		method string
		path   string
		status int
		code   string
	}{
		{http.MethodPost, "/health", http.StatusMethodNotAllowed, "method_not_allowed"},
		{http.MethodGet, "/health/extra", http.StatusNotFound, "not_found"},
		{http.MethodGet, "/api/v1/connections", http.StatusNotFound, "not_found"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			NewHandler().ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d", response.Code, tc.status)
			}
			if response.Header().Get("Content-Type") != "application/problem+json" {
				t.Fatal("HTTP error did not use the problem media type")
			}
			var body problem
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Code != tc.code || body.Status != tc.status {
				t.Fatal("HTTP error did not use its stable problem status and code")
			}
		})
	}
}

func TestProblemEnvelopeUsesValidatedBoundedRequestID(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil)
	request.Header.Set(requestIDHeader, "review-42.a")
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, request)

	var body problem
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal("problem response was not JSON")
	}
	if response.Code != http.StatusNotFound || body.Status != http.StatusNotFound || body.Code != "not_found" || body.RequestID != "review-42.a" {
		t.Fatal("problem response fields did not match the stable not-found contract")
	}
}

func TestInvalidRequestIDIsReplacedAndUnknownPathIsNotReflected(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/credential-sentinel", nil)
	request.Header.Set(requestIDHeader, "bad\r\nid")
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, request)

	var body problem
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal("unknown API route did not return a JSON problem")
	}
	if !validRequestID(body.RequestID) || body.RequestID == "bad\r\nid" {
		t.Fatal("invalid request ID was not replaced with a bounded ID")
	}
	if strings.Contains(response.Body.String(), "credential-sentinel") {
		t.Fatal("problem response reflected the unknown request path")
	}
}
