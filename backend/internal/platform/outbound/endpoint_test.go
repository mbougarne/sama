package outbound

import (
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEndpointConfinementAndRedirects(t *testing.T) {
	profile := Profile{"provider.example", "/v1"}
	for _, raw := range []string{"http://provider.example/v1", "https://user:sentinel@provider.example/v1", "https://provider.example:444/v1", "https://evil.example/v1", "https://provider.example/v10", "https://provider.example/v1/../billing", "https://provider.example/v1/%2e%2e/billing", "https://provider.example/v1//servers", "https://provider.example/v1#fragment", "https://provider.example/v1/%2fservers"} {
		if _, err := profile.URL(raw); err != ErrEndpoint {
			t.Fatalf("allowed %s", raw)
		}
	}
	calls := 0
	client := profile.client(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.example/token"}}, Body: http.NoBody, Request: r}, nil
	}))
	req, _ := http.NewRequest("GET", "https://provider.example/v1/servers?page=opaque", nil)
	req.Header.Set("Authorization", "Bearer synthetic-secret")
	response, err := client.Do(req)
	if response != nil {
		response.Body.Close()
	}
	if err == nil || calls != 1 {
		t.Fatal("redirect followed")
	}
	req, _ = http.NewRequest("GET", "https://evil.example/v1", nil)
	if _, err := client.Do(req); err == nil || calls != 1 {
		t.Fatal("transport bypass")
	}
	req, _ = http.NewRequest("GET", "https://provider.example/v1", nil)
	req.Host = "evil.example"
	if _, err := client.Do(req); err == nil || calls != 1 {
		t.Fatal("Host override")
	}
	if _, err := profile.URL("https://provider.example:443/v1/servers?cursor=" + strings.Repeat("a", 10)); err != nil {
		t.Fatal("approved pagination", err)
	}
}
