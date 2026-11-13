package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"forge-server/internal/config"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestOpenAPIJSON(t *testing.T) {
	spec := openAPIDocument()
	loader := openapi3.NewLoader()
	if err := loader.ResolveRefsIn(spec, nil); err != nil {
		t.Fatalf("resolve OpenAPI refs: %v", err)
	}
	if err := spec.Validate(context.Background()); err != nil {
		t.Fatalf("validate OpenAPI document: %v", err)
	}

	e := New(config.Config{}, Dependencies{})

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload["openapi"] != "3.0.3" {
		t.Fatalf("expected OpenAPI 3.0.3, got %v", payload["openapi"])
	}

	paths, ok := payload["paths"].(map[string]any)
	if !ok {
		t.Fatalf("expected paths object, got %T", payload["paths"])
	}

	for _, path := range []string{
		"/healthz",
		"/readyz",
		"/api/v1/ping",
		"/api/v1/enums",
		"/api/v1/environments",
		"/api/v1/environments/{code}",
		"/api/v1/applications",
		"/api/v1/applications/options",
		"/api/v1/applications/{slug}",
		"/api/v1/applications/{slug}/status",
		"/api/v1/applications/{slug}/delete-check",
		"/api/v1/modules",
		"/api/v1/modules/{id}",
		"/api/v1/builds",
		"/api/v1/builds/{id}",
		"/api/v1/artifacts/applications/{applicationSlug}/modules/{moduleId}/environments/{environment}/builds/{buildId}/file",
		"/api/v1/artifacts/applications/{applicationSlug}/environments/{environment}/manifest",
		"/api/v1/releases",
		"/api/v1/releases/resolve",
		"/api/v1/code-sources",
		"/api/v1/code-sources/{provider}/manifest",
		"/api/v1/code-sources/{provider}/manifest/callback",
		"/api/v1/code-sources/{provider}/install-url",
		"/api/v1/code-sources/{provider}/install/callback",
		"/api/v1/code-sources/{provider}/installations",
		"/api/v1/code-sources/{provider}/repositories/search",
		"/api/v1/storage/configurations",
		"/api/v1/storage/configurations/test",
		"/api/v1/storage/configurations/{id}",
		"/api/v1/feishu/users/search",
	} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("expected OpenAPI path %q", path)
		}
	}
}

// TestOpenAPISnapshot 保证 docs/openapi.json 与代码中的 OpenAPI 文档一致，前端据此生成接口类型。
// 更新快照：UPDATE_OPENAPI_SNAPSHOT=1 go test ./internal/server/http -run TestOpenAPISnapshot
func TestOpenAPISnapshot(t *testing.T) {
	const snapshotPath = "../../../docs/openapi.json"

	actual, err := json.MarshalIndent(openAPIDocument(), "", "  ")
	if err != nil {
		t.Fatalf("marshal OpenAPI document: %v", err)
	}
	actual = append(actual, '\n')

	if os.Getenv("UPDATE_OPENAPI_SNAPSHOT") == "1" {
		if err := os.MkdirAll(filepath.Dir(snapshotPath), 0o755); err != nil {
			t.Fatalf("create snapshot dir: %v", err)
		}
		if err := os.WriteFile(snapshotPath, actual, 0o644); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}
		return
	}

	expected, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot %s: %v；运行 UPDATE_OPENAPI_SNAPSHOT=1 go test ./internal/server/http -run TestOpenAPISnapshot 生成", snapshotPath, err)
	}
	if !bytes.Equal(expected, actual) {
		t.Fatalf("docs/openapi.json 已过期；运行 UPDATE_OPENAPI_SNAPSHOT=1 go test ./internal/server/http -run TestOpenAPISnapshot 更新，并在前端执行 bun run gen:api")
	}
}
