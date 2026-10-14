// sama-dispatch-recovery is an installation-only, database-only recovery gate.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"sama/backend/internal/operation"
	"sama/backend/internal/platform"
)

func main() {
	if run() != nil {
		fmt.Fprintln(os.Stderr, "dispatch recovery command failed")
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		return errors.New("expected status,close,or open version")
	}
	mode := os.Args[1]
	if mode != "status" && mode != "close" && mode != "open" {
		return errors.New("invalid recovery mode")
	}
	var version int64
	if mode == "open" {
		if len(os.Args) != 3 {
			return errors.New("inspected version required")
		}
		var err error
		version, err = strconv.ParseInt(os.Args[2], 10, 64)
		if err != nil || version < 1 {
			return errors.New("invalid version")
		}
	} else if len(os.Args) != 2 {
		return errors.New("unexpected argument")
	}
	config, err := platform.Load()
	if err != nil {
		return err
	}
	if config.Database.MigrationURL == "" {
		return errors.New("installation database identity required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := platform.OpenPool(ctx, platform.DatabaseConfig{URL: config.Database.MigrationURL})
	if err != nil {
		return err
	}
	defer pool.Close()
	if mode != "status" {
		if err = operation.SetRecovery(ctx, pool, mode == "open", version); err != nil {
			return err
		}
	}
	s, err := operation.Recovery(ctx, pool)
	if err != nil {
		return err
	}
	fmt.Printf("open=%t version=%d unresolved=%d\n", s.Open, s.Version, s.Unresolved)
	return nil
}
