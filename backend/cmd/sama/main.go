package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"sama/backend/internal/httpapi"
	"sama/backend/internal/platform"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logServerFailure(logger, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	config, err := platform.Load()
	if err != nil {
		return err
	}
	if config.Database.URL != "" {
		pool, err := platform.OpenPool(ctx, config.Database)
		if err != nil {
			return err
		}
		defer pool.Close()
	}
	server := &http.Server{
		Addr:              config.HTTPAddr,
		Handler:           httpapi.NewHandler(),
		ReadHeaderTimeout: config.Timeouts.ReadHeader,
		ReadTimeout:       config.Timeouts.Read,
		WriteTimeout:      config.Timeouts.Write,
		IdleTimeout:       config.Timeouts.Idle,
		ErrorLog:          log.New(serverLogWriter{logger: logger}, "", 0),
	}
	errorsCh := make(chan error, 1)
	go func() {
		logger.Info("starting HTTP server")
		errorsCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errorsCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), config.Timeouts.Shutdown)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}
