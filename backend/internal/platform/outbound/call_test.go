package outbound

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type callTransport func(*http.Request) (*http.Response, error)

func (f callTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCallReusesNestedSlotAndCancelsWhenCapacityIsFull(t *testing.T) {
	b := NewBudget()
	id, _ := uuid.NewV7()
	held, err := b.acquire(context.Background(), id, "scope")
	if err != nil {
		t.Fatal(err)
	}
	defer held()
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	err = b.Call(ctx, id, "scope", func(ctx context.Context) error {
		third, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()
		if e := b.Call(third, id, "scope", func(context.Context) error { t.Fatal("third caller admitted"); return nil }); e != ErrNetwork {
			t.Fatal("capacity not shared", e)
		}
		r, _ := http.NewRequestWithContext(ctx, "GET", "https://example.invalid", nil)
		transport := budgetTransport{base: callTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		}), budget: b, connection: id, scope: "scope"}
		response, e := transport.RoundTrip(r)
		if e != nil {
			return e
		}
		return response.Body.Close()
	})
	if err != nil {
		t.Fatal("nested transport acquired another slot", err)
	}
}
