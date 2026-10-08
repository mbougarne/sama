package outbound

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

type quotaScope struct {
	until time.Time
	refs  int
}

type connectionSlots struct {
	active int
	refs   int
}

// Budget is shared by every provider caller in the single calling process.
// scope is the adapter-derived credential/project quota identity, not user input.
type Budget struct {
	mu          sync.Mutex
	connections map[uuid.UUID]*connectionSlots
	cooldowns   map[string]*quotaScope
	changed     chan struct{}
}

func NewBudget() *Budget {
	return &Budget{connections: map[uuid.UUID]*connectionSlots{}, cooldowns: map[string]*quotaScope{}, changed: make(chan struct{})}
}

func (b *Budget) signal() { close(b.changed); b.changed = make(chan struct{}) }

func (b *Budget) acquire(ctx context.Context, connection uuid.UUID, scope string) (func(), error) {
	if connection.Version() != 7 || scope == "" || len(scope) > 512 {
		return nil, ErrNetwork
	}
	b.mu.Lock()
	now := time.Now()
	for key, quota := range b.cooldowns {
		if quota.refs == 0 && !quota.until.After(now) {
			delete(b.cooldowns, key)
		}
	}
	if _, exists := b.cooldowns[scope]; !exists && len(b.cooldowns) >= 1000 {
		b.mu.Unlock()
		return nil, ErrNetwork
	}
	slots := b.connections[connection]
	if slots == nil {
		if len(b.connections) >= 1000 {
			b.mu.Unlock()
			return nil, ErrNetwork
		}
		slots = &connectionSlots{}
		b.connections[connection] = slots
	}
	quota := b.cooldowns[scope]
	if quota == nil {
		quota = &quotaScope{}
		b.cooldowns[scope] = quota
	}
	quota.refs++
	slots.refs++
	for {
		delay := time.Until(quota.until)
		if ctx.Err() != nil {
			slots.refs--
			quota.refs--
			if quota.refs == 0 && !quota.until.After(time.Now()) {
				delete(b.cooldowns, scope)
			}
			if slots.refs == 0 {
				delete(b.connections, connection)
			}
			b.mu.Unlock()
			return nil, ErrNetwork
		}
		if slots.active < 2 && delay <= 0 {
			slots.active++
			b.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					b.mu.Lock()
					defer b.mu.Unlock()
					slots.active--
					slots.refs--
					quota.refs--
					if quota.refs == 0 && !quota.until.After(time.Now()) {
						delete(b.cooldowns, scope)
					}
					if slots.refs == 0 {
						delete(b.connections, connection)
					}
					b.signal()
				})
			}, nil
		}
		changed := b.changed
		b.mu.Unlock()
		if delay <= 0 {
			delay = 30 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
		case <-changed:
		case <-timer.C:
		}
		timer.Stop()
		b.mu.Lock()
	}
}

func (b *Budget) postpone(scope string, delay time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	quota := b.cooldowns[scope]
	if quota == nil {
		return
	}
	until := time.Now().Add(delay)
	if until.After(quota.until) {
		quota.until = until
		b.signal()
	}

}

type budgetTransport struct {
	base       http.RoundTripper
	budget     *Budget
	connection uuid.UUID
	scope      string
}

type releaseBody struct {
	io.ReadCloser
	release func()
}

func (b releaseBody) Close() error { defer b.release(); return b.ReadCloser.Close() }

func (t budgetTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	release, err := t.budget.acquire(r.Context(), t.connection, t.scope)
	if err != nil {
		return nil, err
	}
	response, err := t.base.RoundTrip(r)
	if err != nil {
		release()
		return nil, err
	}
	if response.StatusCode == 429 {
		t.budget.postpone(t.scope, retryAfter(response.Header, time.Now()))
	}
	response.Body = releaseBody{response.Body, release}
	return response, nil
}

func (b *Budget) Client(profile Profile, connection uuid.UUID, scope string) *http.Client {
	client := profile.Client()
	client.Transport = budgetTransport{client.Transport, b, connection, scope}
	return client
}
