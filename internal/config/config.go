package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	App       AppConfig
	Feishu    FeishuConfig
	GitHub    GitHubConfig
	HTTP      HTTPConfig
	Postgres  PostgresConfig
	Security  SecurityConfig
	Telemetry TelemetryConfig
	Worker    WorkerConfig
}

type AppConfig struct {
	Env             string
	ShutdownTimeout time.Duration
}

type HTTPConfig struct {
	Addr                 string
	CORSAllowOrigins     []string
	CORSAllowHeaders     []string
	CORSAllowCredentials bool
	// MetricsToken 非空时，GET /metrics 要求 Authorization: Bearer <MetricsToken>，来自 FORGE_METRICS_TOKEN。
	MetricsToken string
}

type TelemetryConfig struct {
	Enabled         bool
	ServiceName     string
	ServiceVersion  string
	Environment     string
	OTLPEndpoint    string
	OTLPHeaders     map[string]string
	OTLPLogEndpoint string
	OTLPLogHeaders  map[string]string
}

// FeishuConfig 只保留飞书开放平台的固定地址和调试开关。应用凭证按租户存库，回调地址由前端按自身域名传入，
// OAuth scope 由平台功能决定，写在 infra/feishu 里。
type FeishuConfig struct {
	AuthURL                string
	TokenURL               string
	UserInfoURL            string
	UserSearchURL          string
	UserBatchURL           string
	DebugLogSearchResponse bool
}

type GitHubConfig struct {
	APIBaseURL          string
	APIVersion          string
	AppHomepageURL      string
	AppID               string
	AppName             string
	AppSlug             string
	InstallCallbackURL  string
	InstallRedirectURL  string
	ManifestCallbackURL string
	PrivateKey          string
	WebhookSecret       string
	WebhookURL          string
	WebURL              string
}

type SecurityConfig struct {
	// SecretKey 是 secretbox 的 AES-256 主密钥，来自 FORGE_SECRET_KEY（32 字节 base64）。
	SecretKey []byte
}

type PostgresConfig struct {
	DSN             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

type WorkerConfig struct {
	// ArtifactDigests 是清单文件项 digests 字段要计算的摘要算法，来自 FORGE_ARTIFACT_DIGESTS。
	ArtifactDigests []string
	ArtifactPrefix  string
	BuildTimeout    time.Duration
	// Concurrency 是单个 worker 进程同时执行的构建数，来自 FORGE_WORKER_CONCURRENCY，默认 1。
	Concurrency int
	// DepsMaxBytes 是单次构建注入依赖产物的总大小上限，来自 FORGE_WORKER_DEPS_MAX_BYTES，超出时构建失败。
	DepsMaxBytes int64
	// Dockerfile 只在模块未配置 dockerfile_path 时兜底，同时作为新建模块的默认值。
	Dockerfile string
	// ID 是 worker 的唯一标识，多个 worker 进程不能重复；不能包含 #（并发槽位用 # 拼接序号）。
	ID string
	// LeaseDuration 是构建租约时长，执行期间每 1/3 时长续租一次；worker 异常退出后，构建最多等这么久就会被其他 worker 接管。
	LeaseDuration time.Duration
	// LogMaxBytes 是单次构建日志上限，超出后只保留开头和最后一段。
	LogMaxBytes int64
	// LogRetention 是构建日志保留期，由 API 进程的后台巡检清理。
	LogRetention time.Duration
	PollInterval time.Duration
	// ShutdownGrace 是 worker 收到退出信号后等待进行中构建完成的时长，超时后中断构建并放回队列，来自 FORGE_WORKER_SHUTDOWN_GRACE。
	ShutdownGrace time.Duration
	WorkDir       string
}

// ArtifactDigestAlgorithms 是 FORGE_ARTIFACT_DIGESTS 允许的摘要算法。
var ArtifactDigestAlgorithms = []string{"sha256", "md5"}

func Load() (Config, error) {
	if err := loadDotenv(); err != nil {
		return Config{}, err
	}

	shutdownTimeout, err := durationEnv("APP_SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	pgMaxConns, err := intEnv("POSTGRES_MAX_CONNS", 10)
	if err != nil {
		return Config{}, err
	}

	pgMinConns, err := intEnv("POSTGRES_MIN_CONNS", 1)
	if err != nil {
		return Config{}, err
	}

	pgMaxConnLifetime, err := durationEnv("POSTGRES_MAX_CONN_LIFETIME", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}

	pgMaxConnIdleTime, err := durationEnv("POSTGRES_MAX_CONN_IDLE_TIME", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}

	workerPollInterval, err := durationEnv("FORGE_WORKER_POLL_INTERVAL", 5*time.Second)
	if err != nil {
		return Config{}, err
	}

	workerLeaseDuration, err := durationEnv("FORGE_WORKER_LEASE_DURATION", 2*time.Minute)
	if err != nil {
		return Config{}, err
	}
	if workerLeaseDuration < 15*time.Second {
		return Config{}, errors.New("parse FORGE_WORKER_LEASE_DURATION: must be at least 15s")
	}

	workerConcurrency, err := intEnv("FORGE_WORKER_CONCURRENCY", 1)
	if err != nil {
		return Config{}, err
	}
	if workerConcurrency < 1 || workerConcurrency > 64 {
		return Config{}, errors.New("parse FORGE_WORKER_CONCURRENCY: must be between 1 and 64")
	}

	workerShutdownGrace, err := durationEnv("FORGE_WORKER_SHUTDOWN_GRACE", 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	if workerShutdownGrace <= 0 {
		return Config{}, errors.New("parse FORGE_WORKER_SHUTDOWN_GRACE: must be positive")
	}

	workerID := strings.TrimSpace(stringEnv("FORGE_WORKER_ID", ""))
	if strings.Contains(workerID, "#") {
		return Config{}, errors.New("parse FORGE_WORKER_ID: must not contain #")
	}

	workerBuildTimeout, err := durationEnv("FORGE_WORKER_BUILD_TIMEOUT", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}

	workerLogMaxBytes, err := intEnv("FORGE_WORKER_LOG_MAX_BYTES", 5*1024*1024)
	if err != nil {
		return Config{}, err
	}
	if workerLogMaxBytes <= 0 {
		return Config{}, errors.New("parse FORGE_WORKER_LOG_MAX_BYTES: must be positive")
	}

	workerDepsMaxBytes, err := intEnv("FORGE_WORKER_DEPS_MAX_BYTES", 200*1024*1024)
	if err != nil {
		return Config{}, err
	}
	if workerDepsMaxBytes <= 0 {
		return Config{}, errors.New("parse FORGE_WORKER_DEPS_MAX_BYTES: must be positive")
	}

	buildLogRetention, err := retentionEnv("FORGE_BUILD_LOG_RETENTION", 30*24*time.Hour)
	if err != nil {
		return Config{}, err
	}

	artifactDigests, err := artifactDigestsEnv("FORGE_ARTIFACT_DIGESTS")
	if err != nil {
		return Config{}, err
	}

	otelHeaders, err := stringMapEnv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", nil)
	if err != nil {
		return Config{}, err
	}
	if len(otelHeaders) == 0 {
		otelHeaders, err = stringMapEnv("OTEL_EXPORTER_OTLP_HEADERS", nil)
		if err != nil {
			return Config{}, err
		}
	}

	otelLogHeaders, err := stringMapEnv("OTEL_EXPORTER_OTLP_LOGS_HEADERS", nil)
	if err != nil {
		return Config{}, err
	}
	if len(otelLogHeaders) == 0 {
		otelLogHeaders, err = stringMapEnv("OTEL_EXPORTER_OTLP_HEADERS", nil)
		if err != nil {
			return Config{}, err
		}
	}

	secretKey, err := secretKeyEnv("FORGE_SECRET_KEY")
	if err != nil {
		return Config{}, err
	}

	appEnv := stringEnv("APP_ENV", "local")
	cfg := Config{
		App: AppConfig{
			Env:             appEnv,
			ShutdownTimeout: shutdownTimeout,
		},
		HTTP: HTTPConfig{
			Addr: stringEnv("HTTP_ADDR", ":8080"),
			CORSAllowOrigins: stringListEnv("HTTP_CORS_ALLOW_ORIGINS", []string{
				"http://127.0.0.1:5173",
				"http://localhost:5173",
			}),
			CORSAllowHeaders: stringListEnv("HTTP_CORS_ALLOW_HEADERS", []string{
				"Origin",
				"Content-Type",
				"Accept",
				"Authorization",
				"Traceparent",
				"Tracestate",
				"Baggage",
				"X-Request-Id",
			}),
			CORSAllowCredentials: boolEnv("HTTP_CORS_ALLOW_CREDENTIALS", true),
			MetricsToken:         strings.TrimSpace(stringEnv("FORGE_METRICS_TOKEN", "")),
		},
		Feishu: FeishuConfig{
			AuthURL:                defaultFeishuAuthURL,
			TokenURL:               defaultFeishuTokenURL,
			UserInfoURL:            defaultFeishuUserInfoURL,
			UserSearchURL:          defaultFeishuUserSearchURL,
			UserBatchURL:           defaultFeishuUserBatchURL,
			DebugLogSearchResponse: boolEnv("FEISHU_DEBUG_LOG_SEARCH_RESPONSE", false),
		},
		GitHub: GitHubConfig{
			APIBaseURL:          stringEnv("GITHUB_API_BASE_URL", defaultGitHubAPIBaseURL),
			APIVersion:          stringEnv("GITHUB_API_VERSION", defaultGitHubAPIVersion),
			AppHomepageURL:      stringEnv("GITHUB_APP_HOMEPAGE_URL", defaultGitHubAppHomepageURL),
			AppID:               stringEnv("GITHUB_APP_ID", ""),
			AppName:             stringEnv("GITHUB_APP_NAME", defaultGitHubAppName),
			AppSlug:             stringEnv("GITHUB_APP_SLUG", ""),
			InstallCallbackURL:  stringEnv("GITHUB_APP_INSTALL_CALLBACK_URL", defaultGitHubAppInstallCallbackURL),
			InstallRedirectURL:  stringEnv("GITHUB_APP_INSTALL_REDIRECT_URL", defaultGitHubAppInstallRedirectURL),
			ManifestCallbackURL: stringEnv("GITHUB_APP_MANIFEST_CALLBACK_URL", defaultGitHubAppManifestCallbackURL),
			PrivateKey:          privateKeyEnv("GITHUB_APP_PRIVATE_KEY", ""),
			WebhookSecret:       stringEnv("GITHUB_APP_WEBHOOK_SECRET", ""),
			WebhookURL:          stringEnv("GITHUB_APP_WEBHOOK_URL", ""),
			WebURL:              stringEnv("GITHUB_WEB_URL", defaultGitHubWebURL),
		},
		Postgres: PostgresConfig{
			DSN:             stringEnv("POSTGRES_DSN", "postgres://postgres:postgres@127.0.0.1:5432/forge_server?sslmode=disable"),
			MaxConns:        int32(pgMaxConns),
			MinConns:        int32(pgMinConns),
			MaxConnLifetime: pgMaxConnLifetime,
			MaxConnIdleTime: pgMaxConnIdleTime,
		},
		Security: SecurityConfig{
			SecretKey: secretKey,
		},
		Telemetry: TelemetryConfig{
			Enabled:         boolEnv("OTEL_ENABLED", false),
			ServiceName:     stringEnv("OTEL_SERVICE_NAME", "forge-server"),
			ServiceVersion:  stringEnv("OTEL_SERVICE_VERSION", "0.1.0"),
			Environment:     stringEnv("OTEL_DEPLOYMENT_ENVIRONMENT", appEnv),
			OTLPEndpoint:    stringEnv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", stringEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")),
			OTLPHeaders:     otelHeaders,
			OTLPLogEndpoint: stringEnv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", stringEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")),
			OTLPLogHeaders:  otelLogHeaders,
		},
		Worker: WorkerConfig{
			ArtifactDigests: artifactDigests,
			ArtifactPrefix:  strings.Trim(stringEnv("FORGE_WORKER_ARTIFACT_PREFIX", "rn-bundles"), "/"),
			BuildTimeout:    workerBuildTimeout,
			Concurrency:     workerConcurrency,
			DepsMaxBytes:    int64(workerDepsMaxBytes),
			Dockerfile:      stringEnv("FORGE_WORKER_DOCKERFILE", "Dockerfile"),
			ID:              workerID,
			LeaseDuration:   workerLeaseDuration,
			LogMaxBytes:     int64(workerLogMaxBytes),
			LogRetention:    buildLogRetention,
			PollInterval:    workerPollInterval,
			ShutdownGrace:   workerShutdownGrace,
			WorkDir:         stringEnv("FORGE_WORKER_WORK_DIR", "/tmp/forge-worker"),
		},
	}

	return cfg, nil
}

func loadDotenv() error {
	envFile, explicit := os.LookupEnv("ENV_FILE")
	if envFile == "" {
		explicit = false
		envFile = ".env"
	}

	if _, err := os.Stat(envFile); err != nil {
		if errors.Is(err, os.ErrNotExist) && !explicit {
			return nil
		}
		return fmt.Errorf("stat env file %s: %w", envFile, err)
	}

	if err := godotenv.Load(envFile); err != nil {
		return fmt.Errorf("load env file %s: %w", envFile, err)
	}

	return nil
}

func stringEnv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func secretKeyEnv(key string) ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil, fmt.Errorf("%s is required: generate one with `openssl rand -base64 32`", key)
	}
	value, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("parse %s: expected base64: %w", key, err)
	}
	if len(value) != 32 {
		return nil, fmt.Errorf("parse %s: expected 32 bytes after base64 decoding, got %d", key, len(value))
	}
	return value, nil
}

func privateKeyEnv(key string, fallback string) string {
	value := stringEnv(key, fallback)
	return strings.ReplaceAll(value, `\n`, "\n")
}

func stringListEnv(key string, fallback []string) []string {
	raw := os.Getenv(key)
	if strings.TrimSpace(raw) == "" {
		return fallback
	}

	items := strings.Split(raw, ",")
	values := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return fallback
	}
	return values
}

func stringMapEnv(key string, fallback map[string]string) (map[string]string, error) {
	raw := os.Getenv(key)
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}

	items := strings.Split(raw, ",")
	values := make(map[string]string, len(items))
	for _, item := range items {
		pair := strings.SplitN(strings.TrimSpace(item), "=", 2)
		if len(pair) != 2 || strings.TrimSpace(pair[0]) == "" {
			return nil, fmt.Errorf("parse %s: expected comma-separated key=value pairs", key)
		}
		values[strings.TrimSpace(pair[0])] = strings.TrimSpace(pair[1])
	}

	return values, nil
}

func boolEnv(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func intEnv(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	return value, nil
}

// retentionEnv 在 time.ParseDuration 的基础上支持按天书写，例如 30d。
func retentionEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	var value time.Duration
	if days, ok := strings.CutSuffix(raw, "d"); ok {
		count, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("parse %s: %w", key, err)
		}
		value = time.Duration(count) * 24 * time.Hour
	} else {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return 0, fmt.Errorf("parse %s: %w", key, err)
		}
		value = parsed
	}
	if value <= 0 {
		return 0, fmt.Errorf("parse %s: must be positive", key)
	}
	return value, nil
}

func artifactDigestsEnv(key string) ([]string, error) {
	values := []string{}
	for _, item := range stringListEnv(key, []string{"sha256"}) {
		algorithm := strings.ToLower(item)
		if !slices.Contains(ArtifactDigestAlgorithms, algorithm) {
			return nil, fmt.Errorf("parse %s: unsupported digest algorithm %q, expected one of %s", key, item, strings.Join(ArtifactDigestAlgorithms, ","))
		}
		if !slices.Contains(values, algorithm) {
			values = append(values, algorithm)
		}
	}
	return values, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	return value, nil
}
