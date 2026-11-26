package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

func TestGetDashboard(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		DashboardService: fakeDashboardService{snapshot: domain.DashboardSnapshot{
			Metrics:    []domain.DashboardMetric{{ID: "apps", Label: "应用数量", Value: "2", TrendTone: "positive", Icon: "app", IconTone: "blue", Sparkline: []int{1, 2}}},
			BuildTrend: []domain.DashboardBuildTrendPoint{{Date: "6/30", BuildCount: 2, SuccessCount: 1}},
			Builds:     []domain.DashboardBuildRecord{{ID: "build-1", Application: "MiCar", Module: "Home", Environment: "product", Status: "success"}},
			Releases:   []domain.DashboardReleaseRecord{{ID: "rel-1", Application: "MiCar", Module: "Home", Environment: "product", Gray: "20%", Status: "active"}},
			Resources:  []domain.DashboardResourceStatus{{ID: "apps", Label: "应用", Value: "2", Href: "/apps", Status: "healthy", StatusLabel: "运行中"}},
			UpdatedAt:  time.Date(2026, 6, 30, 15, 0, 0, 0, time.UTC),
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response dashboardResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Metrics) != 1 || response.Metrics[0].ID != "apps" {
		t.Fatalf("unexpected metrics: %#v", response.Metrics)
	}
	if len(response.BuildTrend) != 1 || response.BuildTrend[0].BuildCount != 2 {
		t.Fatalf("unexpected build trend: %#v", response.BuildTrend)
	}
	if response.UpdatedAt == "" {
		t.Fatalf("expected updatedAt")
	}
}

func TestGetDashboardRequiresSession(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		DashboardService:  fakeDashboardService{},
		FeishuAuthService: &fakeFeishuAuthService{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

type fakeDashboardService struct {
	snapshot domain.DashboardSnapshot
	err      error
}

func (s fakeDashboardService) GetDashboardSnapshot(context.Context) (domain.DashboardSnapshot, error) {
	if s.err != nil {
		return domain.DashboardSnapshot{}, s.err
	}
	return s.snapshot, nil
}
