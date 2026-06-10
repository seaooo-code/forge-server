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

func TestListEnvironments(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		EnvironmentService: fakeEnvironmentService{
			environments: testEnvironments(),
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response environmentsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	expectedCodes := []string{"dev", "staging", "preview", "preProduct", "product"}
	if len(response.Environments) != len(expectedCodes) {
		t.Fatalf("expected %d environments, got %d", len(expectedCodes), len(response.Environments))
	}

	for index, expectedCode := range expectedCodes {
		if response.Environments[index].ID != expectedCode {
			t.Fatalf("expected environment %d to be %q, got %q", index, expectedCode, response.Environments[index].ID)
		}
	}

	if response.Environments[0].Status != "健康" {
		t.Fatalf("expected healthy status label, got %q", response.Environments[0].Status)
	}
	if response.Environments[4].Status != "灰度中" {
		t.Fatalf("expected gray status label, got %q", response.Environments[4].Status)
	}
}

func TestGetEnvironmentDetail(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		EnvironmentService: fakeEnvironmentService{
			details: map[string]domain.EnvironmentDetail{
				"product": testEnvironmentDetail("product"),
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/product", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response environmentDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Environment.ID != "product" {
		t.Fatalf("expected product environment, got %q", response.Environment.ID)
	}
	if response.Environment.Status != "灰度中" {
		t.Fatalf("expected gray status label, got %q", response.Environment.Status)
	}
	if response.Detail.ID != "product" {
		t.Fatalf("expected product detail id, got %q", response.Detail.ID)
	}
	if response.Detail.Endpoint == "" || response.Detail.CDNDomain == "" {
		t.Fatalf("expected runtime config fields, got endpoint=%q cdn=%q", response.Detail.Endpoint, response.Detail.CDNDomain)
	}
	if len(response.Detail.Metrics) != 1 {
		t.Fatalf("expected metrics, got %d", len(response.Detail.Metrics))
	}
	if len(response.Detail.Guardrails) != 1 {
		t.Fatalf("expected guardrails, got %d", len(response.Detail.Guardrails))
	}
	if len(response.Detail.Timeline) != 1 {
		t.Fatalf("expected timeline events, got %d", len(response.Detail.Timeline))
	}
}

func TestGetEnvironmentDetailNotFound(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		EnvironmentService: fakeEnvironmentService{
			details: map[string]domain.EnvironmentDetail{},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/unknown", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNotFound, rec.Code, rec.Body.String())
	}

	var response map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response["error"] != "environment not found" {
		t.Fatalf("expected not found error, got %q", response["error"])
	}
}

type fakeEnvironmentService struct {
	environments []domain.Environment
	details      map[string]domain.EnvironmentDetail
}

func (s fakeEnvironmentService) ListEnvironments(context.Context) ([]domain.Environment, error) {
	return s.environments, nil
}

func (s fakeEnvironmentService) GetEnvironmentDetail(_ context.Context, code string) (domain.EnvironmentDetail, error) {
	detail, ok := s.details[code]
	if !ok {
		return domain.EnvironmentDetail{}, domain.ErrEnvironmentNotFound
	}
	return detail, nil
}

func testEnvironments() []domain.Environment {
	return []domain.Environment{
		testEnvironment("dev", "DEV", domain.EnvironmentStatusHealthy),
		testEnvironment("staging", "STAGING", domain.EnvironmentStatusHealthy),
		testEnvironment("preview", "PREVIEW", domain.EnvironmentStatusHealthy),
		testEnvironment("preProduct", "PREPRODUCT", domain.EnvironmentStatusHealthy),
		testEnvironment("product", "PRODUCT", domain.EnvironmentStatusGray),
	}
}

func testEnvironment(code string, shortCode string, status domain.EnvironmentStatus) domain.Environment {
	return domain.Environment{
		Code:               code,
		ShortCode:          shortCode,
		Name:               shortCode + " 环境",
		Status:             status,
		Tone:               "blue",
		Description:        "环境说明",
		CurrentCombination: code + "-combo",
		TrafficLabel:       "研发内网",
		UpdatedAt:          time.Date(2026, 6, 9, 14, 32, 18, 0, time.UTC),
	}
}

func testEnvironmentDetail(code string) domain.EnvironmentDetail {
	return domain.EnvironmentDetail{
		Environment:    testEnvironment(code, "PRODUCT", domain.EnvironmentStatusGray),
		ID:             code,
		Endpoint:       "https://rn.xiaomi.com",
		CDNDomain:      "rn-cdn.xiaomi.com",
		BranchPolicy:   "prod tag 锁定，不允许直接写入",
		ReleasePolicy:  "发布单审批 + 灰度推进",
		CurrentRelease: "RT-20260611",
		BundleCount:    12,
		TotalSize:      "3.9 MB",
		HealthScore:    "99.8%",
		Metrics: []domain.EnvironmentMetric{
			{
				ID:     "traffic",
				Label:  "当前流量",
				Value:  "2.4M 在线",
				Detail: "按宿主应用在线会话估算",
				Tone:   "blue",
			},
		},
		Guardrails: []domain.EnvironmentGuardrail{
			{
				ID:     "dependency",
				Label:  "依赖检查",
				Value:  "Common Bundle 与 Feature Bundle 版本兼容",
				Status: "通过",
			},
		},
		Timeline: []domain.EnvironmentTimelineEvent{
			{
				ID:          "sync",
				DisplayTime: "14:32:18",
				Action:      "同步 Combination",
				Operator:    "release.bot",
				Status:      "进行中",
				Detail:      "product-combo-20260609.0 已写入环境配置",
			},
		},
	}
}
