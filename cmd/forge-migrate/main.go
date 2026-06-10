package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"forge-server/internal/config"
	"forge-server/internal/infra/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.Postgres)
	if err != nil {
		logger.Error("initialize postgres failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := postgres.RunMigrations(ctx, pool, "migrations"); err != nil {
		logger.Error("run postgres migrations failed", "error", err)
		os.Exit(1)
	}

	logger.Info("postgres migrations applied")
}
