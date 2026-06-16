package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"forge-server/internal/config"
)

func TestListEnums(t *testing.T) {
	e := New(config.Config{}, Dependencies{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/enums", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response enumsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(response.ModuleKinds) != 2 || response.ModuleKinds[0].Value != "Common" || response.ModuleKinds[1].Value != "Feature" {
		t.Fatalf("unexpected module kind enums: %#v", response.ModuleKinds)
	}
	if len(response.ApplicationStatuses) != 3 || response.ApplicationStatuses[0].Value != "running" {
		t.Fatalf("unexpected application status enums: %#v", response.ApplicationStatuses)
	}
}
