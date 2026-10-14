//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sama/backend/internal/provider"
)

// This HTTP fixture is compiled only with tests and is never registered by the
// production composition root. All ports are loopback/ephemeral.
type mutationFixture struct {
	server                  *httptest.Server
	mutations, observations atomic.Int64
	mu                      sync.Mutex
	gates                   map[string]chan struct{}
	reached                 chan string
	outcome                 string
	native, lose            bool
}

func newMutationFixture(t *testing.T) *mutationFixture {
	f := &mutationFixture{gates: map[string]chan struct{}{}, reached: make(chan string, 20), outcome: "succeeded", native: true}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/mutation" {
			f.mutations.Add(1)
		}
		if r.Method == "GET" && r.URL.Path == "/observation" {
			f.observations.Add(1)
		}
		f.mu.Lock()
		gate := f.gates[r.URL.Path]
		outcome, native, lose := f.outcome, f.native, f.lose
		f.mu.Unlock()
		if gate != nil {
			f.reached <- r.URL.Path
			<-gate
		}
		switch r.URL.Path {
		case "/inspect":
			json.NewEncoder(w).Encode(provider.Preconditions{Fingerprint: sha256.Sum256([]byte("stopped")), TargetName: "Safe", Allowed: true})
		case "/mutation":
			if lose {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					conn.Close()
				}
				return
			}
			json.NewEncoder(w).Encode(provider.Receipt{Outcome: "accepted", Reference: "action-1"})
		case "/observation":
			json.NewEncoder(w).Encode(provider.ActionObservation{Outcome: outcome, NativeEvidence: native})
		case "/before-send", "/before-receipt":
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		f.mu.Lock()
		for path, gate := range f.gates {
			close(gate)
			delete(f.gates, path)
		}
		f.mu.Unlock()
		f.server.Close()
	})
	return f
}

func (f *mutationFixture) pause(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gates[path] = make(chan struct{})
}
func (f *mutationFixture) resume(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if gate := f.gates[path]; gate != nil {
		close(gate)
		delete(f.gates, path)
	}
}
func (f *mutationFixture) wait(t *testing.T, path string) {
	t.Helper()
	select {
	case got := <-f.reached:
		if got != path {
			t.Fatalf("unexpected checkpoint %s", got)
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("checkpoint %s timed out", path)
	}
}

type mutationClient struct{ url string }

func (c mutationClient) request(ctx context.Context, method, path string, value any) error {
	r, err := http.NewRequestWithContext(ctx, method, c.url+path, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(r)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 && response.StatusCode != 204 {
		return fmt.Errorf("synthetic fixture response")
	}
	if value == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
		return err
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1024)).Decode(value)
}
func (c mutationClient) Inspect(ctx context.Context, _ provider.Binding, _, _ string) (provider.Preconditions, error) {
	var result provider.Preconditions
	err := c.request(ctx, "GET", "/inspect", &result)
	return result, err
}
func (c mutationClient) Submit(ctx context.Context, _ provider.Binding, _, _ string, _ [32]byte) (provider.Receipt, error) {
	var result provider.Receipt
	if err := c.request(ctx, "GET", "/before-send", nil); err != nil {
		return result, err
	}
	if err := c.request(ctx, "POST", "/mutation", &result); err != nil {
		return result, err
	}
	if err := c.request(ctx, "GET", "/before-receipt", nil); err != nil {
		return result, err
	}
	return result, nil
}
func (c mutationClient) Observe(ctx context.Context, _ provider.Binding, _, _ string, _ *string) (provider.ActionObservation, error) {
	var result provider.ActionObservation
	err := c.request(ctx, "GET", "/observation", &result)
	return result, err
}

func TestFakeMutationProviderSeparatesLostResponseAndObservations(t *testing.T) {
	f := newMutationFixture(t)
	f.lose = true
	c := mutationClient{f.server.URL}
	if _, err := c.Submit(context.Background(), provider.Binding{}, "", "", [32]byte{}); err == nil {
		t.Fatal("lost response was acknowledged")
	}
	if _, err := c.Observe(context.Background(), provider.Binding{}, "", "", nil); err != nil {
		t.Fatal(err)
	}
	if f.mutations.Load() != 1 || f.observations.Load() != 1 {
		t.Fatal("fixture counters conflated calls")
	}
}
