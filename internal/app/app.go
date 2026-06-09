package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"forge-server/internal/config"
	"forge-server/internal/infra/postgres"
	redisinfra "forge-server/internal/infra/redis"
	httpserver "forge-server/internal/server/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

type App struct {
	cfg      config.Config
	logger   *slog.Logger
	server   *echo.Echo
	postgres *pgxpool.Pool
	redis    *redis.Client
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	postgresPool, err := postgres.NewPool(ctx, cfg.Postgres)
	if err != nil {
		return nil, err
	}

	redisClient, err := redisinfra.NewClient(ctx, cfg.Redis)
	if err != nil {
		postgresPool.Close()
		return nil, err
	}

	server := httpserver.New(cfg, httpserver.Dependencies{
		Postgres: postgresPool,
		Redis:    redisClient,
	})

	return &App{
		cfg:      cfg,
		logger:   logger,
		server:   server,
		postgres: postgresPool,
		redis:    redisClient,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		a.logger.Info("http server starting", "addr", a.cfg.HTTP.Addr)
		errCh <- a.server.Start(a.cfg.HTTP.Addr)
	}()

	select {
	case <-ctx.Done():
		return a.Shutdown(context.Background())
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (a *App) Shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, a.cfg.App.ShutdownTimeout)
	defer cancel()

	var shutdownErr error

	if a.server != nil {
		if err := a.server.Shutdown(shutdownCtx); err != nil {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("shutdown http server: %w", err))
		}
	}

	if a.redis != nil {
		if err := a.redis.Close(); err != nil {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("close redis: %w", err))
		}
	}

	if a.postgres != nil {
		a.postgres.Close()
	}

	return shutdownErr
}
