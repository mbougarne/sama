package job

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunnerRejectsInvalidBoundsAndRegistrations(t *testing.T) {
	scheduler := Scheduler{Pool: new(pgxpool.Pool), Now: time.Now}
	reject := func(context.Context, Lease) error { return nil }
	for _, workers := range []int{0, 33} {
		if _, err := NewRunner(scheduler, workers, time.Second, nil, reject); err != ErrJob {
			t.Fatal("worker limit", workers)
		}
	}
	if _, err := NewRunner(scheduler, 4, time.Hour, nil, reject); err != ErrJob {
		t.Fatal("drain limit")
	}
	if _, err := NewRunner(scheduler, 4, time.Second, map[Key]Handler{{Type: "unknown", Version: 99}: {}}, reject); err != ErrJob {
		t.Fatal("nil registration")
	}
}
