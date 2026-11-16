// Package main provides volkeep, a label-driven Docker volume backup daemon.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/deadnews/volkeep/internal/dockerx"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("Fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	dx, err := dockerx.New()
	if err != nil {
		return fmt.Errorf("docker client: %w", err)
	}
	defer func() {
		if err := dx.Close(); err != nil {
			slog.Error("Failed to close Docker client", "error", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	trigger := make(chan os.Signal, 1)
	signal.Notify(trigger, syscall.SIGUSR1)
	defer signal.Stop(trigger)

	return NewDaemon(cfg, dx).Run(ctx, trigger)
}
