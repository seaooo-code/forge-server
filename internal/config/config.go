package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	App      AppConfig
	HTTP     HTTPConfig
	Postgres PostgresConfig
	Redis    RedisConfig
}

type AppConfig struct {
	Env             string
	ShutdownTimeout time.Duration
}

type HTTPConfig struct {
	Addr string
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

	cfg := Config{
		App: AppConfig{
			Env:             stringEnv("APP_ENV", "local"),
			ShutdownTimeout: shutdownTimeout,
		},
		HTTP: HTTPConfig{
			Addr: stringEnv("HTTP_ADDR", ":8080"),
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
