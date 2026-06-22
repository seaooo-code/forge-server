package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"forge-server/internal/config"
	"forge-server/internal/domain"
	feishuinfra "forge-server/internal/infra/feishu"
	githubinfra "forge-server/internal/infra/github"
	otelinfra "forge-server/internal/infra/otel"
	"forge-server/internal/infra/postgres"
	redisinfra "forge-server/internal/infra/redis"
	s3infra "forge-server/internal/infra/s3"
	httpserver "forge-server/internal/server/http"
	"forge-server/internal/service"

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
	otel     otelinfra.ShutdownFunc
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
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

	redisClient, err := redisinfra.NewClient(ctx, cfg.Redis)
	if err != nil {
		postgresPool.Close()
		cleanupOTel()
		return nil, err
	}

	environmentRepository := postgres.NewEnvironmentRepository(postgresPool)
	environmentService := service.NewEnvironmentService(environmentRepository)
	applicationRepository := postgres.NewApplicationRepository(postgresPool)
	applicationService := service.NewApplicationService(applicationRepository)
	codeSourceRepository := postgres.NewCodeSourceRepository(postgresPool)
	githubClient := githubinfra.NewClient(cfg.GitHub, cfg.Telemetry.Enabled)
	codeSourceService := service.NewCodeSourceService(codeSourceRepository, githubClient, domainCodeSourceAppConfig(cfg.GitHub))
	moduleRepository := postgres.NewModuleRepository(postgresPool)
	moduleService := service.NewModuleService(moduleRepository)
	buildRepository := postgres.NewBuildRepository(postgresPool)
	artifactManifestReader := s3infra.NewManifestReader()
	artifactObjectReader := s3infra.NewObjectReader()
	buildService := service.NewBuildServiceWithManifestReader(buildRepository, artifactManifestReader)
	artifactRepository := postgres.NewArtifactRepository(postgresPool)
	artifactService := service.NewArtifactService(artifactRepository, buildRepository, artifactManifestReader, artifactObjectReader)
	artifactAccessTokenRepository := postgres.NewArtifactAccessTokenRepository(postgresPool)
	artifactAccessTokenService := service.NewArtifactAccessTokenService(artifactAccessTokenRepository)
	releaseRepository := postgres.NewReleaseRepository(postgresPool)
	releaseService := service.NewReleaseService(releaseRepository)
	storageRepository := postgres.NewStorageRepository(postgresPool)
	storageService := service.NewStorageService(storageRepository, s3infra.NewTester())
	feishuUserRepository := postgres.NewFeishuUserRepository(postgresPool)
	feishuClient := feishuinfra.NewClient(cfg.Feishu, cfg.Telemetry.Enabled)
	feishuAuthService := service.NewFeishuAuthService(feishuClient, feishuUserRepository)
	feishuContactService := service.NewFeishuContactService(feishuClient)

	server := httpserver.New(cfg, httpserver.Dependencies{
		ApplicationService:         applicationService,
		ArtifactService:            artifactService,
		ArtifactAccessTokenService: artifactAccessTokenService,
		BuildService:               buildService,
		CodeSourceService:          codeSourceService,
		EnvironmentService:         environmentService,
		FeishuAuthService:          feishuAuthService,
		FeishuContactService:       feishuContactService,
		ModuleService:              moduleService,
		ReleaseService:             releaseService,
		StorageService:             storageService,
		Postgres:                   postgresPool,
		Redis:                      redisClient,
	})

	return &App{
		cfg:      cfg,
		logger:   logger,
		server:   server,
		postgres: postgresPool,
		redis:    redisClient,
		otel:     otelShutdown,
	}, nil
}

func domainCodeSourceAppConfig(cfg config.GitHubConfig) domain.CodeSourceAppConfig {
	return domain.CodeSourceAppConfig{
		AppID:      cfg.AppID,
		AppSlug:    cfg.AppSlug,
		PrivateKey: cfg.PrivateKey,
		Provider:   domain.CodeSourceProviderGitHub,
	}
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

	if a.otel != nil {
		if err := a.otel(shutdownCtx); err != nil {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("shutdown opentelemetry: %w", err))
		}
	}

	return shutdownErr
}
