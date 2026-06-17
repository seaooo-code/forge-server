package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"forge-server/internal/config"
	githubinfra "forge-server/internal/infra/github"
	otelinfra "forge-server/internal/infra/otel"
	"forge-server/internal/infra/postgres"
	"forge-server/internal/service"
	"forge-server/internal/worker"

	"github.com/jackc/pgx/v5/pgxpool"
)

type WorkerApp struct {
	cfg      config.Config
	postgres *pgxpool.Pool
	otel     otelinfra.ShutdownFunc
	worker   *worker.Worker
}

func NewWorker(ctx context.Context, cfg config.Config, logger *slog.Logger) (*WorkerApp, error) {
	otelShutdown, err := otelinfra.NewTracerProvider(ctx, cfg.Telemetry)
	if err != nil {
		return nil, err
	}
	cleanupOTel := func() {
		_ = otelShutdown(context.Background())
	}

	postgresPool, err := postgres.NewPool(ctx, cfg.Postgres)
	if err != nil {
		cleanupOTel()
		return nil, err
	}

	if err := postgres.RunMigrations(ctx, postgresPool, "migrations"); err != nil {
		postgresPool.Close()
		cleanupOTel()
		return nil, fmt.Errorf("run postgres migrations: %w", err)
	}

	buildRepository := postgres.NewBuildRepository(postgresPool)
	buildService := service.NewBuildService(buildRepository)
	codeSourceRepository := postgres.NewCodeSourceRepository(postgresPool)
	githubClient := githubinfra.NewClient(cfg.GitHub, cfg.Telemetry.Enabled)
	executor := worker.NewExecutor(cfg.Worker, codeSourceRepository, githubClient, domainCodeSourceAppConfig(cfg.GitHub), logger)
	buildWorker := worker.New(buildService, executor, logger, worker.Options{
		BuildTimeout:  cfg.Worker.BuildTimeout,
		ID:            cfg.Worker.ID,
		LeaseDuration: cfg.Worker.LeaseDuration,
		PollInterval:  cfg.Worker.PollInterval,
	})

	return &WorkerApp{
		cfg:      cfg,
		postgres: postgresPool,
		otel:     otelShutdown,
		worker:   buildWorker,
	}, nil
}

func (a *WorkerApp) Run(ctx context.Context) error {
	return a.worker.Run(ctx)
}

func (a *WorkerApp) Shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, a.cfg.App.ShutdownTimeout)
	defer cancel()

	var shutdownErr error
	if a.postgres != nil {
		a.postgres.Close()
	}
	if a.otel != nil {
		if err := a.otel(shutdownCtx); err != nil {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("shutdown opentelemetry: %w", err))
		}
	}
	return shutdownErr
}
