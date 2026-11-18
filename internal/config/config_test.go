package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsConfiguredDotenvFile(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("APP_ENV=dotenv\nHTTP_ADDR=:9090\nPOSTGRES_MAX_CONNS=42\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	unsetEnv(t, "APP_ENV", "HTTP_ADDR", "POSTGRES_MAX_CONNS")
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
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "authorization=trace-key,other=value")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_HEADERS", "authorization=log-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Telemetry.OTLPHeaders["authorization"] != "trace-key" {
		t.Fatalf("expected clickstack authorization header, got %#v", cfg.Telemetry.OTLPHeaders)
	}
	if cfg.Telemetry.OTLPHeaders["other"] != "value" {
		t.Fatalf("expected secondary header, got %#v", cfg.Telemetry.OTLPHeaders)
	}
	if cfg.Telemetry.OTLPLogHeaders["authorization"] != "log-key" {
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

func TestLoadReadsGitHubAppConfig(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("GITHUB_API_BASE_URL=https://api.github.test\nGITHUB_API_VERSION=2026-03-10\nGITHUB_APP_HOMEPAGE_URL=https://forge.example.com\nGITHUB_APP_ID=12345\nGITHUB_APP_INSTALL_CALLBACK_URL=https://api.forge.example.com/api/v1/code-sources/github/install/callback\nGITHUB_APP_INSTALL_REDIRECT_URL=https://forge.example.com/code-sources\nGITHUB_APP_MANIFEST_CALLBACK_URL=https://api.forge.example.com/api/v1/code-sources/github/manifest/callback\nGITHUB_APP_NAME=Forge Test\nGITHUB_APP_SLUG=forge-console\nGITHUB_APP_PRIVATE_KEY=line1\\nline2\nGITHUB_WEB_URL=https://github.test\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	unsetEnv(t, "GITHUB_API_BASE_URL", "GITHUB_API_VERSION", "GITHUB_APP_HOMEPAGE_URL", "GITHUB_APP_ID", "GITHUB_APP_INSTALL_CALLBACK_URL", "GITHUB_APP_INSTALL_REDIRECT_URL", "GITHUB_APP_MANIFEST_CALLBACK_URL", "GITHUB_APP_NAME", "GITHUB_APP_SLUG", "GITHUB_APP_PRIVATE_KEY", "GITHUB_WEB_URL")
	t.Setenv("ENV_FILE", envPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.GitHub.APIBaseURL != "https://api.github.test" {
		t.Fatalf("expected github api base url, got %q", cfg.GitHub.APIBaseURL)
	}
	if cfg.GitHub.APIVersion != "2026-03-10" {
		t.Fatalf("expected github api version, got %q", cfg.GitHub.APIVersion)
	}
	if cfg.GitHub.AppID != "12345" || cfg.GitHub.AppSlug != "forge-console" {
		t.Fatalf("unexpected github app identity: %+v", cfg.GitHub)
	}
	if cfg.GitHub.AppHomepageURL != "https://forge.example.com" || cfg.GitHub.AppName != "Forge Test" {
		t.Fatalf("unexpected github app metadata: %+v", cfg.GitHub)
	}
	if cfg.GitHub.InstallCallbackURL != "https://api.forge.example.com/api/v1/code-sources/github/install/callback" {
		t.Fatalf("unexpected github install callback url: %+v", cfg.GitHub)
	}
	if cfg.GitHub.InstallRedirectURL != "https://forge.example.com/code-sources" {
		t.Fatalf("unexpected github install redirect url: %+v", cfg.GitHub)
	}
	if cfg.GitHub.ManifestCallbackURL != "https://api.forge.example.com/api/v1/code-sources/github/manifest/callback" {
		t.Fatalf("unexpected github manifest callback url: %+v", cfg.GitHub)
	}
	if cfg.GitHub.PrivateKey != "line1\nline2" {
		t.Fatalf("expected escaped private key newlines to be restored, got %q", cfg.GitHub.PrivateKey)
	}
	if cfg.GitHub.WebURL != "https://github.test" {
		t.Fatalf("expected github web url, got %q", cfg.GitHub.WebURL)
	}
}

func TestLoadReadsWorkerConfig(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("FORGE_WORKER_ID=worker-a\nFORGE_WORKER_WORK_DIR=/tmp/forge-worker-test\nFORGE_WORKER_POLL_INTERVAL=2s\nFORGE_WORKER_LEASE_DURATION=3m\nFORGE_WORKER_BUILD_TIMEOUT=4m\nFORGE_WORKER_DOCKERFILE=docker/rn.Dockerfile\nFORGE_WORKER_ARTIFACT_PREFIX=/rn-bundles/\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	unsetEnv(t, "FORGE_WORKER_ID", "FORGE_WORKER_WORK_DIR", "FORGE_WORKER_POLL_INTERVAL", "FORGE_WORKER_LEASE_DURATION", "FORGE_WORKER_BUILD_TIMEOUT", "FORGE_WORKER_DOCKERFILE", "FORGE_WORKER_ARTIFACT_PREFIX")
	t.Setenv("ENV_FILE", envPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Worker.ID != "worker-a" || cfg.Worker.WorkDir != "/tmp/forge-worker-test" {
		t.Fatalf("unexpected worker identity config: %+v", cfg.Worker)
	}
	if cfg.Worker.PollInterval.String() != "2s" || cfg.Worker.LeaseDuration.String() != "3m0s" || cfg.Worker.BuildTimeout.String() != "4m0s" {
		t.Fatalf("unexpected worker duration config: %+v", cfg.Worker)
	}
	if cfg.Worker.Dockerfile != "docker/rn.Dockerfile" || cfg.Worker.ArtifactPrefix != "rn-bundles" {
		t.Fatalf("unexpected worker build config: %+v", cfg.Worker)
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
	t.Setenv("FEISHU_USER_BATCH_URL", "https://example.com/user_batch")

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
	if cfg.Feishu.UserBatchURL != defaultFeishuUserBatchURL {
		t.Fatalf("expected fixed feishu user batch url, got %q", cfg.Feishu.UserBatchURL)
	}
}

func TestLoadUsesGlobalOTLPHeaders(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv("ENV_FILE", envPath)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=global-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Telemetry.OTLPHeaders["authorization"] != "global-key" {
		t.Fatalf("expected global trace header, got %#v", cfg.Telemetry.OTLPHeaders)
	}
	if cfg.Telemetry.OTLPLogHeaders["authorization"] != "global-key" {
		t.Fatalf("expected global log header, got %#v", cfg.Telemetry.OTLPLogHeaders)
	}
}

func TestLoadReadsSeparateOTLPEndpoints(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv("ENV_FILE", envPath)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://global.example:4318")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://traces.example:4318")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "http://logs.example:4318")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Telemetry.OTLPEndpoint != "http://traces.example:4318" {
		t.Fatalf("expected trace endpoint, got %q", cfg.Telemetry.OTLPEndpoint)
	}
	if cfg.Telemetry.OTLPLogEndpoint != "http://logs.example:4318" {
		t.Fatalf("expected log endpoint, got %q", cfg.Telemetry.OTLPLogEndpoint)
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
