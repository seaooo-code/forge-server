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

func TestLoadReadsOTLPHeaders(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv("ENV_FILE", envPath)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=clickstack-key,other=value")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Telemetry.OTLPHeaders["authorization"] != "clickstack-key" {
		t.Fatalf("expected clickstack authorization header, got %#v", cfg.Telemetry.OTLPHeaders)
	}
	if cfg.Telemetry.OTLPHeaders["other"] != "value" {
		t.Fatalf("expected secondary header, got %#v", cfg.Telemetry.OTLPHeaders)
	}
	if cfg.Telemetry.OTLPLogHeaders["authorization"] != "clickstack-key" {
		t.Fatalf("expected clickstack log authorization header, got %#v", cfg.Telemetry.OTLPLogHeaders)
	}
}

func TestLoadReadsFeishuSearchDebugFlag(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("FEISHU_DEBUG_LOG_SEARCH_RESPONSE=true\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	unsetEnv(t, "FEISHU_DEBUG_LOG_SEARCH_RESPONSE")
	t.Setenv("ENV_FILE", envPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if !cfg.Feishu.DebugLogSearchResponse {
		t.Fatal("expected feishu search debug log flag to be enabled")
	}
}

func TestLoadUsesFixedFeishuEndpointURLs(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv("ENV_FILE", envPath)
	t.Setenv("FEISHU_AUTH_URL", "https://example.com/auth")
	t.Setenv("FEISHU_TOKEN_URL", "https://example.com/token")
	t.Setenv("FEISHU_USER_INFO_URL", "https://example.com/user_info")
	t.Setenv("FEISHU_USER_SEARCH_URL", "https://example.com/user_search")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Feishu.AuthURL != defaultFeishuAuthURL {
		t.Fatalf("expected fixed feishu auth url, got %q", cfg.Feishu.AuthURL)
	}
	if cfg.Feishu.TokenURL != defaultFeishuTokenURL {
		t.Fatalf("expected fixed feishu token url, got %q", cfg.Feishu.TokenURL)
	}
	if cfg.Feishu.UserInfoURL != defaultFeishuUserInfoURL {
		t.Fatalf("expected fixed feishu user info url, got %q", cfg.Feishu.UserInfoURL)
	}
	if cfg.Feishu.UserSearchURL != defaultFeishuUserSearchURL {
		t.Fatalf("expected fixed feishu user search url, got %q", cfg.Feishu.UserSearchURL)
	}
}

func TestLoadPrefersTraceOTLPHeaders(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv("ENV_FILE", envPath)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=global-key")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "authorization=trace-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Telemetry.OTLPHeaders["authorization"] != "trace-key" {
		t.Fatalf("expected trace header to win, got %#v", cfg.Telemetry.OTLPHeaders)
	}
}

func TestLoadPrefersLogOTLPHeaders(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv("ENV_FILE", envPath)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=global-key")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_HEADERS", "authorization=log-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Telemetry.OTLPLogHeaders["authorization"] != "log-key" {
		t.Fatalf("expected log header to win, got %#v", cfg.Telemetry.OTLPLogHeaders)
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
