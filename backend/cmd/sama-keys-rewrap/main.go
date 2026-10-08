package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sama/backend/internal/connection"
	"sama/backend/internal/platform"
	"sama/backend/internal/platform/credentials"
)

func main() {
	oldID := flag.String("old-key", "", "retained master key ID")
	newID := flag.String("new-key", "", "replacement master key ID")
	batch := flag.Int("batch-size", 100, "versions per independently committed batch (1–200)")
	flag.Parse()
	if *oldID == "" || *newID == "" || *oldID == *newID || *batch < 1 || *batch > 200 || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "invalid rewrap arguments")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := rewrap(ctx, *oldID, *newID, *batch); err != nil {
		fmt.Fprintln(os.Stderr, "rewrap stopped; committed batches are retained; rerun to resume")
		os.Exit(1)
	}
}

func rewrap(ctx context.Context, oldID, newID string, batch int) error {
	config, err := platform.Load()
	if err != nil {
		return err
	}
	ring, err := credentials.LoadKeyring(config.KeyringFile)
	if err != nil {
		return err
	}
	if err = ring.Require([]string{oldID, newID}); err != nil {
		return err
	}
	pool, err := platform.OpenPool(ctx, config.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	for {
		batchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		count, err := connection.RewrapBatch(batchCtx, pool, ring, oldID, newID, batch)
		cancel()
		if err != nil {
			return err
		}
		fmt.Printf("Committed %d credential envelopes.\n", count)
		if count == 0 {
			break
		}
	}
	count, err := connection.KeyReferences(ctx, pool, oldID)
	if err != nil {
		return err
	}
	fmt.Printf("Retained database references to the old key: %d.\n", count)
	fmt.Println("Keep old keys until all installations and retained backups no longer require them. No keys were deleted.")
	return nil
}
