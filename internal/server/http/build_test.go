package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

func TestCreateBuildRecord(t *testing.T) {
	service := &fakeBuildService{
		record: testHTTPBuildRecord(),
	}
	e := New(config.Config{}, Dependencies{
		BuildService: service,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", jsonBody(buildCreateRequest{
		ModuleID:    "module-profile",
		Environment: "qa",
		Branch:      "release/1.2.0",
		TriggerID:   "pipeline-main",
		TriggerType: "pipeline",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var response buildRecordDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Build.ID != "build-1" || response.Build.Status != "pending" || response.Build.RepositoryInstallationID != "2001" {
		t.Fatalf("unexpected build response: %#v", response.Build)
	}
	if service.lastInput.Branch != "release/1.2.0" {
		t.Fatalf("expected branch to be passed to service, got %+v", service.lastInput)
	}
	if service.lastInput.TriggerType != domain.BuildTriggerTypePipeline || service.lastInput.TriggerID != "pipeline-main" {
		t.Fatalf("expected trigger to be passed to service, got %+v", service.lastInput)
	}
	if response.Build.Branch != "release/1.2.0" {
		t.Fatalf("expected branch in response, got %#v", response.Build)
	}
	if response.Build.TriggerType != "pipeline" || response.Build.TriggerID != "pipeline-main" {
		t.Fatalf("expected trigger in response, got %#v", response.Build)
	}
	if response.Build.ApplicationName != "seaooo" || response.Build.EnvironmentName != "QA" {
		t.Fatalf("expected application and environment names, got %#v", response.Build)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("build response must not expose storage secrets: %s", rec.Body.String())
	}
}

func TestListBuildRecords(t *testing.T) {
	service := &fakeBuildService{
		records: []domain.BuildRecord{testHTTPBuildRecord()},
		summary: domain.BuildRecordSummary{
			Failed: 1, Pending: 2, Running: 3, Succeeded: 4, Total: 10,
		},
	}
	e := New(config.Config{}, Dependencies{
		BuildService: service,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response buildRecordsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Builds) != 1 || response.Builds[0].ID != "build-1" {
		t.Fatalf("unexpected build list response: %#v", response.Builds)
	}
	if response.Builds[0].TriggerType != "pipeline" || response.Builds[0].TriggerID != "pipeline-main" {
		t.Fatalf("expected trigger in list response, got %#v", response.Builds[0])
	}
	if response.Summary.Total != 10 || response.Summary.Succeeded != 4 {
		t.Fatalf("unexpected build summary response: %#v", response.Summary)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("build response must not expose storage secrets: %s", rec.Body.String())
	}
}

func TestCreateBuildRecordRejectsInvalidRequest(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		BuildService: &fakeBuildService{
			err: domain.ErrInvalidBuildInput,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", jsonBody(buildCreateRequest{
		ModuleID: "bad module",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestCreateBuildRecordModuleNotFound(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		BuildService: &fakeBuildService{
			err: domain.ErrModuleNotFound,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", jsonBody(buildCreateRequest{
		ModuleID:    "missing",
		Environment: "qa",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}

type fakeBuildService struct {
	record    domain.BuildRecord
	records   []domain.BuildRecord
	err       error
	lastInput domain.BuildRecordInput
	summary   domain.BuildRecordSummary
}

func (s *fakeBuildService) CreateBuildRecord(_ context.Context, input domain.BuildRecordInput) (domain.BuildRecord, error) {
	s.lastInput = input
	if s.err != nil {
		return domain.BuildRecord{}, s.err
	}
	return s.record, nil
}

func (s *fakeBuildService) ListBuildRecords(context.Context) ([]domain.BuildRecord, domain.BuildRecordSummary, error) {
	if s.err != nil {
		return nil, domain.BuildRecordSummary{}, s.err
	}
	return s.records, s.summary, nil
}

func testHTTPBuildRecord() domain.BuildRecord {
	startedAt := time.Date(2026, 6, 16, 11, 0, 0, 0, time.UTC)
	return domain.BuildRecord{
		ID:                       "build-1",
		ApplicationName:          "seaooo",
		ModuleID:                 "module-profile",
		ModuleName:               "profile",
		Environment:              "qa",
		EnvironmentName:          "QA",
		RepositoryProvider:       domain.CodeSourceProviderGitHub,
		RepositoryID:             1001,
		RepositoryInstallationID: 2001,
		RepositoryFullName:       "mi-car/profile",
		Branch:                   "release/1.2.0",
		DefaultBranch:            "main",
		StorageConfigurationID:   "storage-1",
		StorageConfigurationName: "QA 存储",
		StorageSecretAccessKey:   "secret",
		Status:                   domain.BuildStatusPending,
		TriggerID:                "pipeline-main",
		TriggerType:              domain.BuildTriggerTypePipeline,
		QueuedAt:                 time.Date(2026, 6, 16, 10, 59, 0, 0, time.UTC),
		StartedAt:                &startedAt,
		CreatedAt:                time.Date(2026, 6, 16, 10, 59, 0, 0, time.UTC),
		UpdatedAt:                time.Date(2026, 6, 16, 10, 59, 0, 0, time.UTC),
	}
}
