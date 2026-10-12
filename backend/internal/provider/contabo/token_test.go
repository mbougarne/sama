package contabo

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

type transport func(*http.Request) (*http.Response, error)

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }

func reply(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func fixture(t *testing.T, api func(*http.Request) *http.Response, auth func(*http.Request) *http.Response) (*Client, provider.Binding) {
	t.Helper()
	w, _ := uuid.NewV7()
	id, _ := uuid.NewV7()
	b, _ := provider.NewBinding(w, id, Family, 1)
	c, err := New(b, connection.Credential{Type: "contabo_v1", ClientID: "synthetic-client", ClientSecret: "synthetic-secret", Username: "synthetic-user", Password: "synthetic-password"}, outbound.NewBudget())
	if err != nil {
		t.Fatal(err)
	}
	c.http = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Host != "api.contabo.com" {
			t.Fatal("unapproved API call")
		}
		id, err := uuid.Parse(r.Header.Get("x-request-id"))
		if err != nil || id.Version() != 4 {
			t.Fatal("request correlation")
		}
		return api(r), nil
	})}
	c.tokens.http = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.String() != tokenURL || r.ParseForm() != nil || r.Form.Get("grant_type") != "password" || r.Form.Get("client_secret") != "synthetic-secret" || r.Form.Get("password") != "synthetic-password" {
			t.Fatal("OAuth contract")
		}
		return auth(r), nil
	})}
	return c, b
}

func tokenReply() *http.Response {
	return reply(200, `{"access_token":"synthetic-access","expires_in":300,"token_type":"Bearer","refresh_token":"never-retained"}`)
}

func TestParallelAcquisitionAndRenewalAreVersionScoped(t *testing.T) {
	var calls atomic.Int32
	c, _ := fixture(t, func(*http.Request) *http.Response { return reply(200, `{}`) }, func(*http.Request) *http.Response { calls.Add(1); return tokenReply() })
	parallel := func(rejected *accessToken) {
		t.Helper()
		var wg sync.WaitGroup
		for range 24 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := c.tokens.get(context.Background(), rejected); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
	}
	parallel(nil)
	if calls.Load() != 1 {
		t.Fatal("one acquisition per caller")
	}
	old, err := c.tokens.get(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	parallel(&old)
	if calls.Load() != 2 {
		t.Fatal("one refresh per caller")
	}
	c.tokens.now = func() time.Time { return time.Now().Add(time.Hour) }
	parallel(nil)
	// The fake clock keeps newly acquired tokens valid relative to that same clock.
	if calls.Load() != 3 {
		t.Fatal("expiry renewal was not serialized", calls.Load())
	}
	other, _ := fixture(t, func(*http.Request) *http.Response { return reply(200, `{}`) }, func(*http.Request) *http.Response { calls.Add(1); return tokenReply() })
	if _, err = other.tokens.get(context.Background(), nil); err != nil || calls.Load() != 4 {
		t.Fatal("cross-connection token reuse")
	}
}

func TestCancellationWhileWaitingForToken(t *testing.T) {
	c, _ := fixture(t, func(*http.Request) *http.Response { return reply(200, `{}`) }, func(*http.Request) *http.Response { return tokenReply() })
	c.tokens.gate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.tokens.get(ctx, nil); err == nil {
		t.Fatal("canceled waiter acquired token")
	}
	<-c.tokens.gate
}

func TestSingle401RefreshThenStopAnd403NeverRefreshes(t *testing.T) {
	for _, code := range []int{401, 403} {
		var apiCalls, authCalls atomic.Int32
		c, b := fixture(t, func(*http.Request) *http.Response {
			apiCalls.Add(1)
			return reply(code, `{"message":"synthetic-secret"}`)
		}, func(*http.Request) *http.Response { authCalls.Add(1); return tokenReply() })
		var out any
		err := c.get(context.Background(), b, base, &out)
		want := int32(1)
		if code == 401 {
			want = 2
		}
		if err == nil || strings.Contains(err.Error(), "synthetic-secret") || apiCalls.Load() != want || authCalls.Load() != want {
			t.Fatal("unbounded refresh/denial", err, apiCalls.Load(), authCalls.Load())
		}
	}
}

func TestInvalidOAuthResponseIsSafeAndNeverFallsBack(t *testing.T) {
	for _, body := range []string{`{}`, `{"access_token":"token","expires_in":0,"token_type":"Bearer"}`, `{"access_token":"token","expires_in":300,"token_type":"Basic"}`, `invalid`} {
		c, _ := fixture(t, func(*http.Request) *http.Response { t.Fatal("API after failed OAuth"); return nil }, func(*http.Request) *http.Response { return reply(200, body) })
		if _, err := c.tokens.get(context.Background(), nil); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
}
