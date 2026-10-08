package job

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

func TestRunRejectsExpiredDeadline(t *testing.T) {
	now := time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC)
	s := Scheduler{Now: func() time.Time { return now }}
	if err := s.Run(context.Background(), Lease{Record: Record{Deadline: now}}, func(context.Context) error {
		t.Fatal("expired lease started work")
		return nil
	}); err != ErrLease {
		t.Fatal(err)
	}
}

func TestRunValidatesOwnershipBeforeWork(t *testing.T) {
	for _, stale := range []bool{false, true} {
		err := runRenewing(context.Background(), nil, func(context.Context) (time.Time, error) {
			if stale {
				return time.Time{}, ErrLease
			}
			return time.Now().Add(-time.Second), nil
		}, func(context.Context) error {
			t.Fatal("unavailable lease started work")
			return nil
		})
		if err != ErrLease {
			t.Fatal(err)
		}
	}
}

func TestLeaseWatchdogCancelsWork(t *testing.T) {
	for _, tc := range []struct {
		name        string
		lease, stop time.Duration
		block, late bool
	}{
		{"failed renewal", 60 * time.Second, 15 * time.Second, false, false},
		{"blocked renewal", 60 * time.Second, 60 * time.Second, true, false},
		{"deadline before renewal", 10 * time.Second, 10 * time.Second, false, false},
		{"late renewal acknowledgement", 60 * time.Second, 60 * time.Second, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				ticker := time.NewTicker(15 * time.Second)
				defer ticker.Stop()
				calls := 0
				err := runRenewing(context.Background(), ticker.C, func(ctx context.Context) (time.Time, error) {
					calls++
					if calls == 1 {
						return start.Add(tc.lease), nil
					}
					if tc.late {
						time.Sleep(50 * time.Second)
						return time.Now().Add(60 * time.Second), nil
					}
					if tc.block {
						<-ctx.Done()
					}
					return time.Time{}, ErrLease
				}, func(ctx context.Context) error {
					<-ctx.Done()
					if elapsed := time.Since(start); elapsed != tc.stop {
						t.Errorf("work cancelled after %s, want %s", elapsed, tc.stop)
					}
					return nil // A callback cannot turn lost ownership into success.
				})
				if err != ErrLease {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestAcknowledgedRenewalExtendsLease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		err := runRenewing(context.Background(), ticker.C, func(context.Context) (time.Time, error) {
			return time.Now().Add(60 * time.Second), nil
		}, func(ctx context.Context) error {
			time.Sleep(75 * time.Second)
			return ctx.Err()
		})
		if err != nil {
			t.Fatal("renewed work stopped at the old expiry", err)
		}
	})
}

func TestJobDeadlineBoundsBlockedRenewal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		calls := 0
		err := runRenewing(ctx, ticker.C, func(ctx context.Context) (time.Time, error) {
			calls++
			if calls == 1 {
				return time.Now().Add(60 * time.Second), nil
			}
			<-ctx.Done()
			return time.Time{}, ErrLease
		}, func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
		if err != context.DeadlineExceeded {
			t.Fatal(err)
		}
	})
}

func TestInitialRenewalIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		err := runRenewing(context.Background(), nil, func(ctx context.Context) (time.Time, error) {
			<-ctx.Done()
			return time.Time{}, ctx.Err()
		}, func(context.Context) error {
			t.Error("unvalidated lease started work")
			return nil
		})
		if err != ErrLease || time.Since(start) != 60*time.Second {
			t.Fatal("initial renewal exceeded its budget", err)
		}
	})
}
