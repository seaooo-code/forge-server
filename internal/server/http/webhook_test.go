package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

func TestWebhookRoutesDispatchWithoutConsoleSession(t *testing.T) {
	service := &fakeWebhookService{result: domain.WebhookResult{Handled: true}}
	// 带飞书鉴权的完整服务器：webhook 路由不能要求控制台 session。
	e := newAuthorizedTestServer(Dependencies{WebhookService: service})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/gitlab/code-source-1", strings.NewReader(`{"object_kind":"push"}`))
	req.Header.Set("x-gitlab-token", "hook-secret")
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d: %s", http.StatusAccepted, rec.Code, rec.Body.String())
	}
	got := service.lastRequest
	if got.Provider != domain.CodeSourceProviderGitLab || got.ConnectionID != "code-source-1" || string(got.Body) != `{"object_kind":"push"}` {
		t.Fatalf("unexpected webhook request: %+v", got)
	}
	if got.Header("X-Gitlab-Token") != "hook-secret" || got.Header("x-gitlab-event") != "Push Hook" {
		t.Fatalf("expected canonical headers, got %+v", got.Headers)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted || service.lastRequest.Provider != domain.CodeSourceProviderGitHub || service.lastRequest.ConnectionID != "" {
		t.Fatalf("unexpected github webhook handling: %d %+v", rec.Code, service.lastRequest)
	}
}

func TestWebhookRoutesMapResults(t *testing.T) {
	tests := []struct {
		name   string
		result domain.WebhookResult
		err    error
		status int
	}{
		{name: "ignored event", status: http.StatusNoContent},
		{name: "bad signature", err: domain.ErrCodeSourceWebhookUnauthorized, status: http.StatusUnauthorized},
		{name: "unknown connection", err: domain.ErrCodeSourceConnectionNotFound, status: http.StatusNotFound},
		{name: "bad payload", err: domain.ErrInvalidCodeSourceWebhookPayload, status: http.StatusBadRequest},
		{name: "not configured", err: domain.ErrCodeSourceAuthNotConfigured, status: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := New(config.Config{}, Dependencies{WebhookService: &fakeWebhookService{result: tt.result, err: tt.err}})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(`{}`))
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("expected status %d, got %d: %s", tt.status, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestWebhookRoutesRejectOversizedPayload(t *testing.T) {
	service := &fakeWebhookService{}
	e := New(config.Config{}, Dependencies{WebhookService: service})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(strings.Repeat("a", maxWebhookBodyBytes+1)))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge || service.calls != 0 {
		t.Fatalf("expected payload too large without service call, got %d (calls=%d)", rec.Code, service.calls)
	}
}

type fakeWebhookService struct {
	calls       int
	err         error
	lastRequest domain.WebhookRequest
	result      domain.WebhookResult
}

func (s *fakeWebhookService) HandleWebhook(_ context.Context, req domain.WebhookRequest) (domain.WebhookResult, error) {
	s.calls++
	s.lastRequest = req
	return s.result, s.err
}
