package config

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testSecretKey = bytes.Repeat([]byte{7}, 32)

func TestMain(m *testing.M) {
	// 其他配置测试不关心主密钥，这里统一提供一个合法值。
	if err := os.Setenv("FORGE_SECRET_KEY", base64.StdEncoding.EncodeToString(testSecretKey)); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func writeEmptyEnvFile(t *testing.T) {
	t.Helper()
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("ENV_FILE", envPath)
}

func TestLoadReadsSecretKey(t *testing.T) {
	writeEmptyEnvFile(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !bytes.Equal(cfg.Security.SecretKey, testSecretKey) {
		t.Fatalf("unexpected secret key %x", cfg.Security.SecretKey)
	}
}

func TestLoadRejectsMissingOrInvalidSecretKey(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "missing", value: "", want: "FORGE_SECRET_KEY is required"},
		{name: "not base64", value: "not-base64!!", want: "expected base64"},
		{name: "wrong length", value: base64.StdEncoding.EncodeToString(make([]byte, 16)), want: "expected 32 bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeEmptyEnvFile(t)
			t.Setenv("FORGE_SECRET_KEY", tt.value)

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}
