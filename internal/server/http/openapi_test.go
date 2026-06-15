package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		"/api/v1/environments",
		"/api/v1/environments/{code}",
		"/api/v1/applications",
		"/api/v1/applications/options",
		"/api/v1/applications/{slug}",
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
