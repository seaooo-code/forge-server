package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"forge-server/internal/config"
	feishuinfra "forge-server/internal/infra/feishu"
	githubinfra "forge-server/internal/infra/github"
	gitlabinfra "forge-server/internal/infra/gitlab"
	otelinfra "forge-server/internal/infra/otel"
	"forge-server/internal/infra/postgres"
	redisinfra "forge-server/internal/infra/redis"
	s3infra "forge-server/internal/infra/s3"
	"forge-server/internal/infra/secretbox"
	httpserver "forge-server/internal/server/http"
	"forge-server/internal/service"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

type App struct {
	cfg            config.Config
	logger         *slog.Logger
	releaseService *service.ReleaseService
	// buildLogService 用于后台巡检清理过期的构建日志。
	buildLogService *service.BuildLogService
	server          *echo.Echo
	postgres        *pgxpool.Pool
	redis           *redis.Client
	otel            otelinfra.ShutdownFunc
}

const releaseExpirationInterval = time.Minute

const buildLogCleanupInterval = time.Hour

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
	authorizationRepository := postgres.NewAuthorizationRepository(postgresPool)
	authorizationService := service.NewAuthorizationService(authorizationRepository)
	codeSourceRepository := postgres.NewCodeSourceRepository(postgresPool)
	buildRepository := postgres.NewBuildRepository(postgresPool)
	buildTriggerService := newBuildTriggerService(postgresPool, buildRepository, logger)
	codeSourceService, err := newCodeSourceService(cfg, codeSourceRepository, buildTriggerService)
	if err != nil {
		_ = redisClient.Close()
		postgresPool.Close()
		cleanupOTel()
		return nil, err
	}
	buildVariableSecrets, err := secretbox.New(cfg.Security.SecretKey)
	if err != nil {
		_ = redisClient.Close()
		postgresPool.Close()
		cleanupOTel()
		return nil, fmt.Errorf("initialize secretbox: %w", err)
	}
	buildVariableService := service.NewBuildVariableService(postgres.NewBuildVariableRepository(postgresPool), buildVariableSecrets)
	buildLogService := service.NewBuildLogService(postgres.NewBuildLogRepository(postgresPool))
	moduleRepository := postgres.NewModuleRepository(postgresPool)
	buildAdapterRepository := postgres.NewBuildAdapterRepository(postgresPool)
	moduleService := service.NewModuleService(moduleRepository, codeSourceRepository, cfg.Worker.Dockerfile).
		WithBuildAdapters(buildAdapterRepository)
	buildAdapterService := service.NewBuildAdapterService(service.BuildAdapterServiceDependencies{
		Modules:    moduleRepository,
		Repository: buildAdapterRepository,
	})
	artifactManifestReader := s3infra.NewManifestReader()
	artifactObjectReader := s3infra.NewObjectReader()
	buildService := service.NewBuildServiceWithManifestReader(buildRepository, artifactManifestReader).WithSucceededHook(buildTriggerService)
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
	dashboardService := service.NewDashboardService(service.DashboardDependencies{
		ApplicationService:         applicationService,
		ModuleService:              moduleService,
		BuildService:               buildService,
		ReleaseService:             releaseService,
		CodeSourceService:          codeSourceService,
		StorageService:             storageService,
		EnvironmentService:         environmentService,
		ArtifactAccessTokenService: artifactAccessTokenService,
	})

	server := httpserver.New(cfg, httpserver.Dependencies{
		ApplicationService:         applicationService,
		ArtifactService:            artifactService,
		ArtifactAccessTokenService: artifactAccessTokenService,
		AuthorizationService:       authorizationService,
		BuildService:               buildService,
		BuildQueueService:          buildService,
		BuildAdapterService:        buildAdapterService,
		BuildLogService:            buildLogService,
		BuildVariableService:       buildVariableService,
		DashboardService:           dashboardService,
		CodeSourceService:          codeSourceService,
		WebhookService:             codeSourceService,
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
		cfg:             cfg,
		logger:          logger,
		releaseService:  releaseService,
		buildLogService: buildLogService,
		server:          server,
		postgres:        postgresPool,
		redis:           redisClient,
		otel:            otelShutdown,
	}, nil
}

// newCodeSourceService 装配代码源 provider 注册表与 secretbox，API 与 worker 共用同一套实现。
func newCodeSourceService(cfg config.Config, store *postgres.CodeSourceRepository, pushHandler service.PushEventHandler) (*service.CodeSourceService, error) {
	secrets, err := secretbox.New(cfg.Security.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("initialize secretbox: %w", err)
	}
	githubProvider := githubinfra.NewProvider(githubinfra.NewClient(cfg.GitHub, cfg.Telemetry.Enabled), store)
	return service.NewCodeSourceService(service.CodeSourceServiceDependencies{
		GitHubApp:   githubProvider,
		PushHandler: pushHandler,
		Registry: service.NewCodeSourceProviderRegistry(
			githubProvider,
			gitlabinfra.NewProvider(cfg.Telemetry.Enabled),
		),
		Secrets: secrets,
		Store:   store,
	}), nil
}

// newBuildTriggerService 装配 push 自动构建与预览通道发布，API 与 worker 共用。
// API 进程用它处理 webhook，worker 进程用它作为构建成功回调创建预览发布。
func newBuildTriggerService(pool *pgxpool.Pool, buildRepository *postgres.BuildRepository, logger *slog.Logger) *service.BuildTriggerService {
	return service.NewBuildTriggerService(service.BuildTriggerServiceDependencies{
		Builds:   buildRepository,
		Logger:   logger,
		Modules:  postgres.NewModuleRepository(pool),
		Releases: service.NewReleaseService(postgres.NewReleaseRepository(pool)),
	})
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	if a.releaseService != nil {
		go a.runReleaseExpirationLoop(ctx)
	}
	if a.buildLogService != nil {
		go a.runBuildLogCleanupLoop(ctx)
	}

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

func (a *App) runReleaseExpirationLoop(ctx context.Context) {
	a.expireTargetedGrayReleases(ctx)

	ticker := time.NewTicker(releaseExpirationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.expireTargetedGrayReleases(ctx)
		}
	}
}

func (a *App) expireTargetedGrayReleases(ctx context.Context) {
	expireCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	expired, err := a.releaseService.ExpireTargetedGrayReleases(expireCtx)
	if err != nil {
		a.logger.Error("expire targeted gray releases failed", "error", err)
		return
	}
	if expired > 0 {
		a.logger.Info("targeted gray releases expired", "count", expired)
	}
}

// runBuildLogCleanupLoop 按 FORGE_BUILD_LOG_RETENTION 定期删除过期的构建日志分片。
func (a *App) runBuildLogCleanupLoop(ctx context.Context) {
	a.cleanupExpiredBuildLogs(ctx)

	ticker := time.NewTicker(buildLogCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.cleanupExpiredBuildLogs(ctx)
		}
	}
}

func (a *App) cleanupExpiredBuildLogs(ctx context.Context) {
	cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	deleted, err := a.buildLogService.DeleteExpiredBuildLogs(cleanupCtx, a.cfg.Worker.LogRetention)
	if err != nil {
		a.logger.Error("delete expired build logs failed", "error", err)
		return
	}
	if deleted > 0 {
		a.logger.Info("expired build logs deleted", "chunks", deleted)
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
