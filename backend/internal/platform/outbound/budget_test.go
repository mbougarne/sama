package outbound

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBudgetConcurrencyCancellationAndScopeCooldown(t *testing.T) {
	budget := NewBudget()
	connection, _ := uuid.NewV7()
	other, _ := uuid.NewV7()
	first, err := budget.acquire(context.Background(), connection, "project")
	if err != nil {
		t.Fatal(err)
	}
	second, err := budget.acquire(context.Background(), connection, "project")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	if _, err := budget.acquire(ctx, connection, "project"); err != ErrNetwork {
		t.Fatal("third slot")
	}
	cancel()
	first()
	first()
	second()
	release, err := budget.acquire(context.Background(), connection, "project")
	if err != nil {
		t.Fatal("cancel leaked slot")
	}
	release()
	held, err := budget.acquire(context.Background(), connection, "project")
	if err != nil {
		t.Fatal(err)
	}
	budget.postpone("project", time.Second)
	held()
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
	if _, err := budget.acquire(ctx, other, "project"); err != ErrNetwork {
		t.Fatal("shared scope cooldown")
	}
	cancel()
	release, err = budget.acquire(context.Background(), other, "unrelated")
	if err != nil {
		t.Fatal("unrelated blocked")
	}
	release()
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if len(budget.connections) != 0 {
		t.Fatal("slot entries retained")
	}
}

func TestRateHeaders(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	for _, value := range []string{"2", now.Add(2 * time.Second).UTC().Format(http.TimeFormat)} {
		if delay := retryAfter(http.Header{"Retry-After": []string{value}}, now); delay != 2*time.Second {
			t.Fatal(delay)
		}
	}
	if delay := retryAfter(http.Header{"Retry-After": []string{"9999999"}}, now); delay != time.Hour {
		t.Fatal("unbounded wait", delay)
	}
}
