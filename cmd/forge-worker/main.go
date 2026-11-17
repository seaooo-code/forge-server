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
	// 第一次信号进入排空：不再领取新构建，等进行中的构建完成（最多 FORGE_WORKER_SHUTDOWN_GRACE）。
	// 之后恢复默认信号处理，再发一次信号立即退出，未完成的构建在租约过期后由其他 worker 接管。
	go func() {
		<-ctx.Done()
		stop()
		logger.Info("shutdown signal received, draining builds; send the signal again to exit immediately")
	}()

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
