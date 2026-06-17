package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"forge-server/internal/app"
	"forge-server/internal/config"
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

	workerApp, err := app.NewWorker(ctx, cfg, logger)
	if err != nil {
		logger.Error("initialize worker failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := workerApp.Shutdown(context.Background()); err != nil {
			logger.Error("shutdown worker failed", "error", err)
		}
	}()

	if err := workerApp.Run(ctx); err != nil {
		logger.Error("run worker failed", "error", err)
		os.Exit(1)
	}
}
