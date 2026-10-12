package job

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"sama/backend/internal/platform"
)

var ErrActive = errors.New("another scheduler is active")
var ErrDrain = errors.New("scheduler drain deadline exceeded")

type Key struct {
	Type    string
	Version int
}

type Handler struct {
	Work   func(context.Context, Lease) error
	Finish func(context.Context, Lease, error) error
}

type Runner struct {
	scheduler Scheduler
	workers   int
	drain     time.Duration
	handlers  map[Key]Handler
	reject    func(context.Context, Lease) error
}

func NewRunner(s Scheduler, workers int, drain time.Duration, handlers map[Key]Handler, reject func(context.Context, Lease) error) (*Runner, error) {
	if s.Pool == nil || s.Now == nil || workers < 1 || workers > 32 || drain <= 0 || drain > time.Minute || len(handlers) > 16 || reject == nil {
		return nil, ErrJob
	}
	copied := map[Key]Handler{}
	for key, handler := range handlers {
		if key.Type == "" || key.Version < 1 || handler.Work == nil || handler.Finish == nil {
			return nil, ErrJob
		}
		copied[key] = handler
	}
	return &Runner{s, workers, drain, copied, reject}, nil
}

// Run holds a dedicated PostgreSQL advisory-lock session for one active
// scheduler. Registered work is lease-renewed; cancelled work stays recoverable.
func (r *Runner) Run(ctx context.Context) error {
	conn, err := platform.Acquire(ctx, r.scheduler.Pool)
	if err != nil {
		return ErrLease
	}
	defer conn.Release()
	var active bool
	if conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(1396789848)`).Scan(&active) != nil {
		return ErrLease
	}
	if !active {
		return ErrActive
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Failed unlock must close this exact session, never return a held lock to pool.
		var unlocked bool
		if conn.QueryRow(release, `SELECT pg_advisory_unlock(1396789848)`).Scan(&unlocked) != nil || !unlocked {
			conn.Conn().Close(release)
		}
	}()
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errorsCh := make(chan error, r.workers)
	var wg sync.WaitGroup
	for i := 0; i < r.workers; i++ {
		owner, err := uuid.NewRandom()
		if err != nil {
			cancel()
			wg.Wait()
			return ErrJob
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.worker(workCtx, owner); err != nil && workCtx.Err() == nil {
				errorsCh <- err
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	heartbeat := time.NewTicker(time.Second)
	defer heartbeat.Stop()
	var result error
	for workCtx.Err() == nil {
		select {
		case <-ctx.Done():
			cancel()
		case result = <-errorsCh:
			cancel()
		case <-heartbeat.C:
			ping, pingCancel := context.WithTimeout(workCtx, time.Second)
			err := conn.Conn().Ping(ping)
			pingCancel()
			if err != nil {
				result = ErrLease
				cancel()
			}
		}
	}
	timer := time.NewTimer(r.drain)
	defer timer.Stop()
	select {
	case <-done:
		return result
	case <-timer.C:
		return ErrDrain
	}
}

func (r *Runner) worker(ctx context.Context, owner uuid.UUID) error {
	for ctx.Err() == nil {
		lease, err := r.scheduler.Claim(ctx, owner)
		if errors.Is(err, ErrNoJob) {
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
				continue
			}
		}
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		handler, registered := r.handlers[Key{lease.Type, lease.PayloadVersion}]
		if !registered {
			if err := r.reject(ctx, lease); err != nil {
				return err
			}
			continue
		}
		err = r.scheduler.Run(ctx, lease, func(workCtx context.Context) error { return handler.Work(workCtx, lease) })
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, ErrLease) {
			continue
		}
		finish, stop := context.WithTimeout(ctx, 5*time.Second)
		err = handler.Finish(finish, lease, err)
		stop()
		if err != nil {
			return err
		}
	}
	return nil
}
