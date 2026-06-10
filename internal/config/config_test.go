package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsConfiguredDotenvFile(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("APP_ENV=dotenv\nHTTP_ADDR=:9090\nPOSTGRES_MAX_CONNS=42\nREDIS_DB=7\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	unsetEnv(t, "APP_ENV", "HTTP_ADDR", "POSTGRES_MAX_CONNS", "REDIS_DB")
	t.Setenv("ENV_FILE", envPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.App.Env != "dotenv" {
		t.Fatalf("expected APP_ENV from dotenv, got %q", cfg.App.Env)
	}
	if cfg.HTTP.Addr != ":9090" {
		t.Fatalf("expected HTTP_ADDR from dotenv, got %q", cfg.HTTP.Addr)
	}
	if cfg.Postgres.MaxConns != 42 {
		t.Fatalf("expected POSTGRES_MAX_CONNS from dotenv, got %d", cfg.Postgres.MaxConns)
	}
	if cfg.Redis.DB != 7 {
		t.Fatalf("expected REDIS_DB from dotenv, got %d", cfg.Redis.DB)
	}
}

func TestLoadKeepsProcessEnvOverDotenv(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("HTTP_ADDR=:9090\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv("ENV_FILE", envPath)
	t.Setenv("HTTP_ADDR", ":9999")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.HTTP.Addr != ":9999" {
		t.Fatalf("expected process HTTP_ADDR to win, got %q", cfg.HTTP.Addr)
	}
}

func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()

	for _, key := range keys {
		key := key
		original, exists := os.LookupEnv(key)

		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}

		t.Cleanup(func() {
			if exists {
				_ = os.Setenv(key, original)
				return
			}
			_ = os.Unsetenv(key)
		})
	}
}
