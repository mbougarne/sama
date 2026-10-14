//go:build integration

package integration

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/job"
	"sama/backend/internal/operation"
	"sama/backend/internal/provider"
)

// A separately owned test process can be killed without interrupting other
// tests, the fake cloud, the database or any application on this machine.
func TestMutationWorkerProcess(t *testing.T) {
	if os.Getenv("SAMA_TEST_WORKER") != "1" {
		return
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("SAMA_TEST_DB"))
	if err != nil {
		t.Fatal("synthetic worker database unavailable")
	}
	defer pool.Close()
	client := mutationClient{os.Getenv("SAMA_TEST_FAKE")}
	s := operation.NewService(pool, nil, true, map[string]operation.ReviewFactory{"fixture/compute": func(context.Context, provider.Binding) (provider.PowerReviewer, error) { return client, nil }})
	d := operation.Dispatcher{Service: s, Factories: map[string]operation.MutationFactory{"fixture/compute": func(context.Context, provider.Binding) (provider.PowerMutation, error) { return client, nil }}}
	lease, err := (job.Scheduler{Pool: pool, Now: time.Now, Operations: true}).Claim(context.Background(), entityID(t))
	if err != nil {
		t.Fatal("synthetic worker claim failed")
	}
	err = (job.Scheduler{Pool: pool, Now: time.Now}).Run(context.Background(), lease, func(ctx context.Context) error { return d.Work(ctx, lease) })
	if err != nil {
		t.Fatal("synthetic worker checkpoint failed")
	}
}

func startMutationWorker(t *testing.T, pool *pgxpool.Pool, f *mutationFixture) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestMutationWorkerProcess$")
	// Only synthetic, task-owned endpoints enter this child; never inherit
	// ambient provider credentials from the user's shell.
	command.Env = []string{"SAMA_TEST_WORKER=1", "SAMA_TEST_DB=" + pool.Config().ConnConfig.ConnString(), "SAMA_TEST_FAKE=" + f.server.URL}
	if err := command.Start(); err != nil {
		t.Fatal("start owned worker", err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			command.Process.Kill()
			command.Wait()
		}
	})
	return command
}

func TestWorkerCrashesNeverDuplicateAmbiguousMutation(t *testing.T) {
	for _, checkpoint := range []string{"/inspect", "/before-send", "/mutation", "/before-receipt"} {
		t.Run(checkpoint, func(t *testing.T) {
			d, pool, lease, _, _, f, _ := dispatchSetup(t)
			ctx := context.Background()
			// Return the initial fixture claim to the queue before the child owns it.
			if _, err := pool.Exec(ctx, `UPDATE jobs SET state='queued',lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL WHERE id=$1`, lease.ID); err != nil {
				t.Fatal(err)
			}
			f.pause(checkpoint)
			worker := startMutationWorker(t, pool, f)
			f.wait(t, checkpoint)
			if err := worker.Process.Kill(); err != nil {
				t.Fatal("kill owned worker", err)
			}
			worker.Wait()
			f.resume(checkpoint)
			state := "dispatching"
			calls := int64(0)
			if checkpoint == "/inspect" {
				state = "queued"
			}
			if checkpoint == "/mutation" || checkpoint == "/before-receipt" {
				calls = 1
			}
			assertOperation(t, pool, lease.Operation, state, true)
			if f.mutations.Load() != calls {
				t.Fatal("checkpoint effect counter")
			}
			f.mu.Lock()
			if checkpoint == "/before-send" {
				f.outcome = "unknown"
			}
			f.mu.Unlock()
			if err := d.Work(ctx, claimSuccessor(t, pool, lease)); err != nil {
				t.Fatal(err)
			}
			if checkpoint == "/inspect" {
				assertOperation(t, pool, lease.Operation, "waiting_provider", true)
				calls = 1
			} else if checkpoint == "/before-send" {
				assertOperation(t, pool, lease.Operation, "unknown", true)
			} else {
				assertOperation(t, pool, lease.Operation, "succeeded", false)
			}
			if f.mutations.Load() != calls {
				t.Fatal("replacement repeated mutation")
			}
		})
	}
}
