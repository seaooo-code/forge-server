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

type fakeBuildQueueService struct {
	stats domain.BuildQueueStats
}

func (s fakeBuildQueueService) GetBuildQueueStats(context.Context) (domain.BuildQueueStats, error) {
	return s.stats, nil
}

func TestMetricsExposesBuildQueueStats(t *testing.T) {
	e := New(config.Config{}, Dependencies{BuildQueueService: fakeBuildQueueService{stats: domain.BuildQueueStats{
		Waiting: 3, OldestWaitSeconds: 42.5, Running: 2, ActiveWorkers: 1,
	}}})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("expected text/plain, got %q", rec.Header().Get("Content-Type"))
	}
	for _, line := range []string{
		"# TYPE forge_build_queue_waiting gauge",
		"forge_build_queue_waiting 3\n",
		"forge_build_queue_oldest_wait_seconds 42.5\n",
		"forge_build_running 2\n",
		"forge_build_workers_active 1\n",
	} {
		if !strings.Contains(rec.Body.String(), line) {
			t.Fatalf("expected %q in metrics:\n%s", line, rec.Body.String())
		}
	}
}

func TestMetricsRequiresTokenWhenConfigured(t *testing.T) {
	cfg := config.Config{HTTP: config.HTTPConfig{MetricsToken: "scrape-token"}}
	e := New(cfg, Dependencies{BuildQueueService: fakeBuildQueueService{}})

	for _, header := range []string{"", "Bearer wrong", "scrape-token"} {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("authorization %q: expected 401, got %d", header, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer scrape-token")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d", rec.Code)
	}
}
