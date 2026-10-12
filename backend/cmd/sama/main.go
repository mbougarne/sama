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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"sama/backend/internal/connection"
	"sama/backend/internal/httpapi"
	"sama/backend/internal/identity"
	"sama/backend/internal/inventory"
	"sama/backend/internal/job"
	"sama/backend/internal/operation"
	"sama/backend/internal/platform"
	"sama/backend/internal/platform/credentials"
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
	oidcConfig, err := identity.LoadOIDC(config.PublicOrigin.String(), os.LookupEnv, os.ReadFile)
	if err != nil {
		return err
	}
	var login *identity.OIDC
	if oidcConfig.Issuer != "" {
		login, err = identity.NewOIDC(ctx, oidcConfig, time.Now)
		if err != nil {
			return err
		}
		if config.Database.URL == "" {
			return errors.New("OIDC database required")
		}
	}
	var pool *pgxpool.Pool
	if config.Database.URL != "" {
		pool, err = platform.OpenPool(ctx, config.Database)
		if err != nil {
			return err
		}
		defer pool.Close()
	}
	var auth *httpapi.Auth
	if login != nil {
		sessions, err := identity.NewSessions(pool, identity.DefaultSessionPolicy(), time.Now)
		if err != nil {
			return err
		}
		auth = &httpapi.Auth{OIDC: login, Challenges: identity.NewChallenges(pool, time.Now), Sessions: sessions, Pool: pool}
	}
	var assets *httpapi.Assets
	var assetHandler http.Handler
	if config.AssetsDir != "" {
		assets, err = httpapi.OpenAssets(config.AssetsDir)
		if err != nil {
			return err
		}
		defer assets.Close()
		assetHandler = assets
	}
	// Keyring failure closes credential readiness, not process liveness.
	ring, _ := credentials.LoadKeyring(config.KeyringFile)
	if auth != nil {
		auth.Operations = operation.NewService(pool, auth.Sessions, config.Budgets.ProviderCallingProcesses == 1, nil)
		auth.ConnectionValidation = connection.NewValidationService(pool, ring, config.Budgets.ProviderCallingProcesses == 1, nil)
	}
	workerErrors := make(chan error, 1)
	workerCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	if config.WorkersEnabled {
		scanner := inventory.Scanner{Pool: pool}
		scheduler := job.Scheduler{Pool: pool, Now: time.Now}
		runner, err := job.NewRunner(scheduler, config.Budgets.WorkerCount, config.Timeouts.Shutdown, map[job.Key]job.Handler{
			{Type: "connection_refresh", Version: 2}: {Work: scanner.Scan, Finish: scanner.Complete},
		}, func(ctx context.Context, lease job.Lease) error {
			err := scanner.Complete(ctx, lease, job.ErrJob)
			if errors.Is(err, job.ErrLease) {
				return scheduler.Finish(ctx, lease, "failed")
			}
			return err
		})
		if err != nil {
			return err
		}
		go func() { workerErrors <- runner.Run(workerCtx) }()
	}
	server := &http.Server{
		Addr:              config.HTTPAddr,
		Handler:           httpapi.WithClientIP(httpapi.CredentialReadiness(httpapi.NewAppHandler(auth, assetHandler), pool, ring), config.TrustedProxies),
		MaxHeaderBytes:    httpapi.MaxHeaderBytes,
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
	case err := <-workerErrors:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), config.Timeouts.Shutdown)
		defer cancel()
		server.Shutdown(shutdownCtx)
		if err == nil && ctx.Err() != nil {
			return nil
		}
		if err == nil {
			return errors.New("scheduler stopped unexpectedly")
		}
		return err
	case err := <-errorsCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		stopWorkers()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), config.Timeouts.Shutdown)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		if config.WorkersEnabled {
			select {
			case err := <-workerErrors:
				if err != nil {
					return err
				}
			case <-shutdownCtx.Done():
				return job.ErrDrain
			}
		}
		return nil
	}
}
