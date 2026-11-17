package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadWorkerBuildLogAndDigestDefaults(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	unsetEnv(t, "FORGE_WORKER_LOG_MAX_BYTES", "FORGE_BUILD_LOG_RETENTION", "FORGE_ARTIFACT_DIGESTS")
	t.Setenv("ENV_FILE", envPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Worker.LogMaxBytes != 5*1024*1024 || cfg.Worker.LogRetention != 30*24*time.Hour {
		t.Fatalf("unexpected build log defaults: %+v", cfg.Worker)
	}
	if !reflect.DeepEqual(cfg.Worker.ArtifactDigests, []string{"sha256"}) {
		t.Fatalf("unexpected digest defaults: %#v", cfg.Worker.ArtifactDigests)
	}
}

func TestLoadWorkerBuildLogAndDigestOverrides(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("FORGE_WORKER_LOG_MAX_BYTES=1048576\nFORGE_BUILD_LOG_RETENTION=7d\nFORGE_ARTIFACT_DIGESTS=sha256, MD5,md5\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	unsetEnv(t, "FORGE_WORKER_LOG_MAX_BYTES", "FORGE_BUILD_LOG_RETENTION", "FORGE_ARTIFACT_DIGESTS")
	t.Setenv("ENV_FILE", envPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Worker.LogMaxBytes != 1048576 || cfg.Worker.LogRetention != 7*24*time.Hour {
		t.Fatalf("unexpected build log config: %+v", cfg.Worker)
	}
	if !reflect.DeepEqual(cfg.Worker.ArtifactDigests, []string{"sha256", "md5"}) {
		t.Fatalf("unexpected digests: %#v", cfg.Worker.ArtifactDigests)
	}
}

func TestLoadRejectsInvalidWorkerBuildLogAndDigestConfig(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{name: "unknown digest", env: "FORGE_ARTIFACT_DIGESTS=sha1\n", want: "FORGE_ARTIFACT_DIGESTS"},
		{name: "non-positive log size", env: "FORGE_WORKER_LOG_MAX_BYTES=0\n", want: "FORGE_WORKER_LOG_MAX_BYTES"},
		{name: "invalid retention", env: "FORGE_BUILD_LOG_RETENTION=abc\n", want: "FORGE_BUILD_LOG_RETENTION"},
		{name: "zero retention", env: "FORGE_BUILD_LOG_RETENTION=0d\n", want: "FORGE_BUILD_LOG_RETENTION"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envPath := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(envPath, []byte(tt.env), 0o600); err != nil {
				t.Fatalf("write env file: %v", err)
			}
			unsetEnv(t, "FORGE_WORKER_LOG_MAX_BYTES", "FORGE_BUILD_LOG_RETENTION", "FORGE_ARTIFACT_DIGESTS")
			t.Setenv("ENV_FILE", envPath)

			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error mentioning %s, got %v", tt.want, err)
			}
		})
	}
}

func TestLoadWorkerDepsMaxBytes(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		want    int64
		wantErr bool
	}{
		{name: "default", env: "", want: 200 * 1024 * 1024},
		{name: "override", env: "FORGE_WORKER_DEPS_MAX_BYTES=1048576\n", want: 1048576},
		{name: "zero", env: "FORGE_WORKER_DEPS_MAX_BYTES=0\n", wantErr: true},
		{name: "invalid", env: "FORGE_WORKER_DEPS_MAX_BYTES=abc\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envPath := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(envPath, []byte(tt.env), 0o600); err != nil {
				t.Fatalf("write env file: %v", err)
			}
			unsetEnv(t, "FORGE_WORKER_DEPS_MAX_BYTES")
			t.Setenv("ENV_FILE", envPath)

			cfg, err := Load()
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "FORGE_WORKER_DEPS_MAX_BYTES") {
					t.Fatalf("expected FORGE_WORKER_DEPS_MAX_BYTES error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("load config: %v", err)
			}
			if cfg.Worker.DepsMaxBytes != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, cfg.Worker.DepsMaxBytes)
			}
		})
	}
}
