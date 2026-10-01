package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sama/backend/internal/identity"
	"sama/backend/internal/platform"
	"sama/backend/internal/workspace"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bootstrap(ctx, os.Args[1:]); err != nil {
		log.Print("owner bootstrap failed; verify configuration and installation state")
		os.Exit(1)
	}
	log.Print("installation owner established")
}

func bootstrap(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("sama-bootstrap", flag.ContinueOnError)
	issuer := flags.String("issuer", "", "exact configured OIDC issuer")
	subject := flags.String("subject", "", "exact OIDC subject")
	name := flags.String("workspace-name", "", "initial workspace name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	config, err := platform.Load()
	if err != nil {
		return err
	}
	oidcConfig, err := identity.LoadOIDC(config.PublicOrigin.String(), os.LookupEnv, os.ReadFile)
	if err != nil {
		return err
	}
	if oidcConfig.Issuer == "" || *issuer != oidcConfig.Issuer || *subject == "" || !workspace.ValidName(*name) {
		return workspace.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool, err := platform.OpenPool(bounded, config.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	_, err = workspace.Bootstrap(bounded, pool, oidcConfig.Issuer, *issuer, *subject, *name)
	return err
}
