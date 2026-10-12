// sama-metadata-cleanup runs bounded database-only metadata retention batches.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"sama/backend/internal/inventory"
	"sama/backend/internal/job"
	"sama/backend/internal/operation"
	"sama/backend/internal/platform"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "metadata cleanup failed")
		os.Exit(1)
	}
}

func run() error {
	config, err := platform.Load()
	if err != nil {
		return err
	}
	if config.Database.URL == "" {
		return errors.New("cleanup database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := platform.OpenPool(ctx, config.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	reviews, err := operation.PruneReviews(ctx, pool)
	if err != nil {
		return err
	}
	staging, err := inventory.PruneStaging(ctx, pool)
	if err != nil {
		return err
	}
	jobs, err := job.PruneTerminal(ctx, pool)
	if err != nil {
		return err
	}
	fmt.Printf("reviews=%d staging=%d jobs=%d\n", reviews, staging, jobs)
	return nil
}
