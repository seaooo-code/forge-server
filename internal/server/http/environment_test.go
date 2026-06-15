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
		EnvironmentService: &fakeEnvironmentService{
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

	expectedCodes := []string{"qa", "prod-cn"}
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
	if response.Environments[1].Status != "灰度中" {
		t.Fatalf("expected gray status label, got %q", response.Environments[1].Status)
	}
}

func TestCreateEnvironment(t *testing.T) {
	service := fakeEnvironmentService{}
	e := New(config.Config{}, Dependencies{EnvironmentService: &service})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", jsonBody(environmentRequest{
		Code:                   "qa",
		Name:                   "QA 环境",
		Status:                 "healthy",
		Tone:                   "blue",
		StorageConfigurationID: "storage-qa",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	if service.lastCreateInput.StorageConfigurationID != "storage-qa" {
		t.Fatalf("expected storage configuration id, got %q", service.lastCreateInput.StorageConfigurationID)
	}

	var response environmentDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Environment.StorageConfigurationID != "storage-qa" {
		t.Fatalf("expected storage configuration in response, got %#v", response.Environment)
	}
}

func TestUpdateEnvironment(t *testing.T) {
	service := fakeEnvironmentService{}
	e := New(config.Config{}, Dependencies{EnvironmentService: &service})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/environments/qa", jsonBody(environmentRequest{
		Code:                   "qa",
		Name:                   "QA 验收环境",
		Status:                 "gray",
		Tone:                   "sky",
		StorageConfigurationID: "storage-qa-2",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	if service.lastUpdateCode != "qa" {
		t.Fatalf("expected update code qa, got %q", service.lastUpdateCode)
	}
	if service.lastUpdateInput.StorageConfigurationID != "storage-qa-2" {
		t.Fatalf("expected storage configuration id, got %q", service.lastUpdateInput.StorageConfigurationID)
	}

	var response environmentDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Environment.ID != "qa" {
		t.Fatalf("expected qa environment, got %q", response.Environment.ID)
	}
	if response.Environment.StorageConfigurationID != "storage-qa-2" {
		t.Fatalf("expected updated storage configuration, got %#v", response.Environment)
	}
}

func TestGetEnvironmentDetail(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		EnvironmentService: &fakeEnvironmentService{
			details: map[string]domain.Environment{
				"product": testEnvironment("product", domain.EnvironmentStatusGray),
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
}

func TestGetEnvironmentDetailNotFound(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		EnvironmentService: &fakeEnvironmentService{
			details: map[string]domain.Environment{},
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
	environments    []domain.Environment
	details         map[string]domain.Environment
	lastCreateInput domain.EnvironmentInput
	lastUpdateCode  string
	lastUpdateInput domain.EnvironmentInput
}

func (s fakeEnvironmentService) ListEnvironments(context.Context) ([]domain.Environment, error) {
	return s.environments, nil
}

func (s *fakeEnvironmentService) CreateEnvironment(_ context.Context, input domain.EnvironmentInput) (domain.Environment, error) {
	s.lastCreateInput = input
	environment := testEnvironment(input.Code, input.Status)
	environment.Name = input.Name
	environment.StorageConfigurationID = input.StorageConfigurationID
	environment.StorageConfigurationName = "QA OSS"
	return environment, nil
}

func (s fakeEnvironmentService) GetEnvironmentDetail(_ context.Context, code string) (domain.Environment, error) {
	detail, ok := s.details[code]
	if !ok {
		return domain.Environment{}, domain.ErrEnvironmentNotFound
	}
	return detail, nil
}

func (s *fakeEnvironmentService) UpdateEnvironment(_ context.Context, code string, input domain.EnvironmentInput) (domain.Environment, error) {
	s.lastUpdateCode = code
	s.lastUpdateInput = input
	environment := testEnvironment(input.Code, input.Status)
	environment.Name = input.Name
	environment.Tone = input.Tone
	environment.StorageConfigurationID = input.StorageConfigurationID
	environment.StorageConfigurationName = "QA 灰度 OSS"
	return environment, nil
}

func testEnvironments() []domain.Environment {
	return []domain.Environment{
		testEnvironment("qa", domain.EnvironmentStatusHealthy),
		testEnvironment("prod-cn", domain.EnvironmentStatusGray),
	}
}

func testEnvironment(code string, status domain.EnvironmentStatus) domain.Environment {
	return domain.Environment{
		Code:                     code,
		Name:                     code + " 环境",
		Status:                   status,
		Tone:                     "blue",
		Description:              "环境说明",
		StorageConfigurationID:   "storage-qa",
		StorageConfigurationName: "QA OSS",
		UpdatedAt:                time.Date(2026, 6, 9, 14, 32, 18, 0, time.UTC),
	}
}
