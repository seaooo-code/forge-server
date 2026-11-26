package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forge-server/internal/config"
)

func TestSetupFirstTenantFlow(t *testing.T) {
	tenants := &fakeTenantService{setupRequired: true, setupToken: "setup-token"}
	e := New(config.Config{}, Dependencies{TenantService: tenants})

	status := httptest.NewRecorder()
	e.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"setupRequired":true`) {
		t.Fatalf("expected setup required, got %d %s", status.Code, status.Body.String())
	}

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/tenant", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}
	if rec := post(`{"token":"wrong","id":"xiaomi","name":"小米","feishuAppId":"cli_aaaaaaaaaaaa","feishuAppSecret":"s"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for wrong token, got %d", rec.Code)
	}
	rec := post(`{"token":"setup-token","id":"xiaomi","name":"小米","feishuAppId":"cli_aaaaaaaaaaaa","feishuAppSecret":"s"}`)
	if rec.Code != http.StatusCreated || strings.Contains(rec.Body.String(), `"s"`) {
		t.Fatalf("expected 201 without secret, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(`{"token":"setup-token","id":"other","name":"其他","feishuAppId":"cli_bbbbbbbbbbbb","feishuAppSecret":"s"}`); rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 after setup, got %d", rec.Code)
	}
}
