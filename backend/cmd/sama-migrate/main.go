package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"sama/backend/internal/platform"
	"sama/backend/migrations"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := migrate(ctx); err != nil {
		log.Print("migration failed")
		os.Exit(1)
	}
}

func migrate(ctx context.Context) error {
	config, err := platform.Load()
	if err != nil {
		return err
	}
	if config.Database.MigrationURL == "" {
		return errors.New("migration database URL is not configured")
	}
	pool, err := platform.OpenPool(ctx, platform.DatabaseConfig{URL: config.Database.MigrationURL})
	if err != nil {
		return err
	}
	defer pool.Close()
	return platform.ApplyMigrations(ctx, pool, migrations.Files)
}
