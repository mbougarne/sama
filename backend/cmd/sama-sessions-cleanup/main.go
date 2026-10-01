package main

import (
	"context"
	"log"
	"os"
	"time"

	"sama/backend/internal/identity"
	"sama/backend/internal/platform"
)

func main() {
	if err := cleanup(); err != nil {
		log.Print("session cleanup failed")
		os.Exit(1)
	}
	log.Print("session cleanup batch completed")
}

func cleanup() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	config, err := platform.Load()
	if err != nil {
		return err
	}
	pool, err := platform.OpenPool(ctx, config.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	sessions, err := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
	if err != nil {
		return err
	}
	_, err = sessions.Cleanup(ctx, 1000)
	return err
}
