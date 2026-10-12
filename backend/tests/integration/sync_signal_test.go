//go:build integration

package integration

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"sama/backend/internal/inventory"
	"sama/backend/internal/job"
	"sama/backend/internal/platform"
	"sama/backend/internal/provider"
)

func TestSyncRecoveryProcessHelper(t *testing.T) {
	mode := os.Getenv("SAMA_TEST_SYNC_RECOVERY_MODE")
	if mode == "" {
		return
	}
	raw := os.Getenv("SAMA_TEST_SYNC_RECOVERY_DB")
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "postgres" || parsed.Hostname() != "127.0.0.1" {
		t.Fatal("explicit disposable database required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	pool, err := platform.OpenPool(ctx, platform.DatabaseConfig{URL: raw})
	if err != nil {
		t.Fatal("helper database unavailable")
	}
	defer pool.Close()
	scanner := inventory.Scanner{Pool: pool, Reader: func(context.Context, provider.Binding) (provider.InventoryReader, error) {
		return scanReader{func(ctx context.Context, cursor string) (provider.InventoryPage, error) {
			if mode == "block" {
				if cursor == "" {
					return provider.InventoryPage{Data: []provider.Observation{scanObservation("signal-partial")}, Next: "wait"}, nil
				}
				fmt.Println("staged")
				<-ctx.Done()
				return provider.InventoryPage{}, ctx.Err()
			}
			return provider.InventoryPage{Data: []provider.Observation{scanObservation("signal-complete")}}, nil
		}}, nil
	}}
	scheduler := job.Scheduler{Pool: pool, Now: time.Now}
	runner, err := job.NewRunner(scheduler, 4, 3*time.Second, map[job.Key]job.Handler{{Type: "connection_refresh", Version: 2}: {Work: scanner.Scan, Finish: scanner.Complete}}, func(ctx context.Context, l job.Lease) error { return scheduler.Finish(ctx, l, "failed") })
	if err != nil || runner.Run(ctx) != nil {
		t.Fatal("helper scheduler failed")
	}
}

func TestSIGTERMDrainsAndRestartPublishesOnlyCompleteGeneration(t *testing.T) {
	pool, lease, _, run := inventoryLease(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE jobs SET state='queued',lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL,next_run_at=clock_timestamp()-interval '1 second' WHERE id=$1`, lease.ID); err != nil {
		t.Fatal(err)
	}
	var raw string
	raw = pool.Config().ConnConfig.ConnString()
	// The fixture URL contains only the synthetic loopback database; production
	// configuration/ambient credentials are neither read nor forwarded.
	start := func(mode string) (*exec.Cmd, *bufio.Scanner) {
		t.Helper()
		childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		t.Cleanup(cancel)
		command := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestSyncRecoveryProcessHelper$", "-test.timeout=25s")
		command.Env = append(os.Environ(), "SAMA_TEST_SYNC_RECOVERY_MODE="+mode, "SAMA_TEST_SYNC_RECOVERY_DB="+raw)
		output, err := command.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if command.Start() != nil {
			t.Fatal("start owned worker helper")
		}
		t.Cleanup(func() {
			if command.ProcessState == nil {
				command.Process.Kill()
				command.Wait()
			}
		})
		return command, bufio.NewScanner(output)
	}
	stop := func(command *exec.Cmd) {
		t.Helper()
		if command.Process.Signal(syscall.SIGTERM) != nil {
			t.Fatal("signal owned worker")
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal("worker did not drain cleanly", err)
			}
		case <-time.After(6 * time.Second):
			t.Fatal("SIGTERM drain exceeded bound")
		}
	}
	first, output := start("block")
	staged := make(chan bool, 1)
	go func() {
		for output.Scan() {
			if output.Text() == "staged" {
				staged <- true
				return
			}
		}
		staged <- false
	}()
	select {
	case ok := <-staged:
		if !ok {
			t.Fatal("worker exited before staging")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("worker failed to checkpoint")
	}
	stop(first)
	var state string
	if pool.QueryRow(ctx, `SELECT state FROM jobs WHERE id=$1`, lease.ID).Scan(&state) != nil || state != "running" {
		t.Fatal("SIGTERM discarded recovery state")
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, lease.ID); err != nil {
		t.Fatal(err)
	}
	second, _ := start("complete")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		pool.QueryRow(ctx, `SELECT status FROM sync_runs WHERE id=$1`, run).Scan(&state)
		if state == "succeeded" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop(second)
	if state != "succeeded" {
		t.Fatal("new worker did not recover sync")
	}
	var count int
	if pool.QueryRow(ctx, `SELECT count(*) FROM resources WHERE native_id='signal-complete' AND tombstoned_at IS NULL`).Scan(&count) != nil || count != 1 {
		t.Fatal("complete generation missing")
	}
	if pool.QueryRow(ctx, `SELECT count(*) FROM resources WHERE native_id='signal-partial'`).Scan(&count) != nil || count != 0 {
		t.Fatal("partial generation published")
	}
}
