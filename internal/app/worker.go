package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"forge-server/internal/config"
	"forge-server/internal/domain"
	otelinfra "forge-server/internal/infra/otel"
	"forge-server/internal/infra/postgres"
	"forge-server/internal/infra/secretbox"
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
	codeSourceRepository := postgres.NewCodeSourceRepository(postgresPool)
	// 构建成功回调：把跟随该分支的提测单条目换成这次构建，失败只记日志。
	buildService := service.NewBuildService(buildRepository).
		WithSucceededHook(newBuildTriggerService(postgresPool, buildRepository, logger))
	// worker 与 API 共用同一套 provider 获取 clone 凭证；worker 不接收 webhook，因此不需要 PushEventHandler。
	codeSourceService, err := newCodeSourceService(cfg, codeSourceRepository, nil)
	if err != nil {
		postgresPool.Close()
		cleanupOTel()
		return nil, err
	}
	// worker 与 API 连同一个库：构建变量通过 service + 仓储读取并用 secretbox 解密，日志分片直接写库。
	buildVariableSecrets, err := secretbox.New(cfg.Security.SecretKey)
	if err != nil {
		postgresPool.Close()
		cleanupOTel()
		return nil, fmt.Errorf("initialize secretbox: %w", err)
	}
	// 按运行环境自动禁用不能处理的构建方式：没有 git 或 Docker 的容器里，worker 只处理 gitlab_ci，不会领到 docker 构建。
	capabilities := worker.DetectBuildModes(ctx, workerBuildModes(cfg.Worker.BuildModes), nil)
	for mode, reason := range capabilities.Disabled {
		logger.Warn("build mode disabled on this worker", "mode", mode, "reason", reason)
	}
	if len(capabilities.Enabled) == 0 {
		postgresPool.Close()
		cleanupOTel()
		return nil, fmt.Errorf("worker 没有可处理的构建方式：FORGE_WORKER_BUILD_MODES=%s，当前环境都不可用", strings.Join(cfg.Worker.BuildModes, ","))
	}
	logger.Info("build modes enabled on this worker", "modes", capabilities.Enabled)

	executor := worker.NewExecutor(cfg.Worker, worker.ExecutorDependencies{
		// 构建适配器：读取适配器，渲染后把版本和 sha256 写回构建记录。
		Adapters: service.NewBuildAdapterService(service.BuildAdapterServiceDependencies{
			Repository: postgres.NewBuildAdapterRepository(postgresPool),
			Usage:      buildRepository,
		}),
		// gitlab_ci 构建：通过代码源的 CI 构建，pipeline 写回构建记录。
		CI:          codeSourceService,
		CIPipelines: buildService,
		Credentials: codeSourceService,
		// 提交详情：构建成功后按 commit SHA 查标题、作者和时间写进构建记录，查不到只记 SHA。
		Commits: codeSourceService,
		// 依赖产物注入：Feature 模块构建时读取所属 Common 模块生效中的默认路由构建，产物按依赖构建自己的存储配置下载。
		Dependencies: service.NewBuildDependencyService(buildRepository),
		Logs:         service.NewBuildLogService(postgres.NewBuildLogRepository(postgresPool)),
		// 包签名：应用有生效中的签名密钥时，签名产物文件列表，见 docs/artifact-signing.md。
		Signer:    service.NewArtifactSigningService(postgres.NewSigningKeyRepository(postgresPool), buildVariableSecrets),
		Variables: service.NewBuildVariableService(postgres.NewBuildVariableRepository(postgresPool), buildVariableSecrets),
	}, logger)
	buildWorker := worker.New(buildService, executor, logger, worker.Options{
		BuildTimeout:  cfg.Worker.BuildTimeout,
		Concurrency:   cfg.Worker.Concurrency,
		ID:            cfg.Worker.ID,
		LeaseDuration: cfg.Worker.LeaseDuration,
		Modes:         capabilities.Enabled,
		PollInterval:  cfg.Worker.PollInterval,
		ShutdownGrace: cfg.Worker.ShutdownGrace,
	})

	return &WorkerApp{
		cfg:      cfg,
		postgres: postgresPool,
		otel:     otelShutdown,
		worker:   buildWorker,
	}, nil
}

// workerBuildModes 把配置里的构建方式转成领域类型；配置已校验过取值。
func workerBuildModes(values []string) []domain.BuildMode {
	modes := make([]domain.BuildMode, 0, len(values))
	for _, value := range values {
		modes = append(modes, domain.BuildMode(value))
	}
	return modes
}

func (a *WorkerApp) Run(ctx context.Context) error {
	// worker 领取所有租户的构建，查询不按租户过滤。
	return a.worker.Run(domain.WithSystemScope(ctx))
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
