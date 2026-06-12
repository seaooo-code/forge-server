package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	App       AppConfig
	Feishu    FeishuConfig
	HTTP      HTTPConfig
	Postgres  PostgresConfig
	Redis     RedisConfig
	Telemetry TelemetryConfig
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

type FeishuConfig struct {
	AppID                  string
	AppSecret              string
	RedirectURI            string
	Scope                  string
	Prompt                 string
	AuthURL                string
	TokenURL               string
	UserInfoURL            string
	UserSearchURL          string
	UserBatchURL           string
	DebugLogSearchResponse bool
}

type PostgresConfig struct {
	DSN             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

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

	redisDB, err := intEnv("REDIS_DB", 0)
	if err != nil {
		return Config{}, err
	}

	otelHeaders, err := stringMapEnv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", nil)
	if err != nil {
		return Config{}, err
	}
	if len(otelHeaders) == 0 {
		otelHeaders, err = stringMapEnv("OTEL_EXPORTER_OTLP_HEADERS", nil)
	}
	if err != nil {
		return Config{}, err
	}

	otelLogHeaders, err := stringMapEnv("OTEL_EXPORTER_OTLP_LOGS_HEADERS", nil)
	if err != nil {
		return Config{}, err
	}
	if len(otelLogHeaders) == 0 {
		otelLogHeaders, err = stringMapEnv("OTEL_EXPORTER_OTLP_HEADERS", nil)
	}
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
		},
		Feishu: FeishuConfig{
			AppID:                  stringEnv("FEISHU_APP_ID", ""),
			AppSecret:              stringEnv("FEISHU_APP_SECRET", ""),
			RedirectURI:            stringEnv("FEISHU_REDIRECT_URI", ""),
			Scope:                  stringEnv("FEISHU_OAUTH_SCOPE", ""),
			Prompt:                 stringEnv("FEISHU_OAUTH_PROMPT", ""),
			AuthURL:                defaultFeishuAuthURL,
			TokenURL:               defaultFeishuTokenURL,
			UserInfoURL:            defaultFeishuUserInfoURL,
			UserSearchURL:          defaultFeishuUserSearchURL,
			UserBatchURL:           defaultFeishuUserBatchURL,
			DebugLogSearchResponse: boolEnv("FEISHU_DEBUG_LOG_SEARCH_RESPONSE", false),
		},
		Postgres: PostgresConfig{
			DSN:             stringEnv("POSTGRES_DSN", "postgres://postgres:postgres@127.0.0.1:5432/forge_server?sslmode=disable"),
			MaxConns:        int32(pgMaxConns),
			MinConns:        int32(pgMinConns),
			MaxConnLifetime: pgMaxConnLifetime,
			MaxConnIdleTime: pgMaxConnIdleTime,
		},
		Redis: RedisConfig{
			Addr:     stringEnv("REDIS_ADDR", "127.0.0.1:6379"),
			Password: stringEnv("REDIS_PASSWORD", ""),
			DB:       redisDB,
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
