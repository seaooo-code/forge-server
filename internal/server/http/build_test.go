package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	e := newAuthorizedTestServer(Dependencies{
		BuildService: service,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", jsonBody(buildCreateRequest{
		ModuleID:    "module-profile",
		Environment: "qa",
		Branch:      "release/1.2.0",
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var response buildRecordDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Build.ID != "build-1" || response.Build.Status != "pending" || response.Build.CodeSourceConnectionID != "code-source-1" ||
		response.Build.RepositoryID != "1001" || response.Build.CommitSHA != "abc123" || response.Build.RuntimeVersion != "1.2.0-725" ||
		len(response.Build.Platforms) != 2 || response.Build.RepositoryProvider != "gitlab" {
		t.Fatalf("unexpected build response: %#v", response.Build)
	}
	if service.lastInput.Branch != "release/1.2.0" {
		t.Fatalf("expected branch to be passed to service, got %+v", service.lastInput)
	}
	if service.lastInput.TriggerType != domain.BuildTriggerTypeManual || service.lastInput.TriggerID != "" {
		t.Fatalf("expected console builds to be manual, got %+v", service.lastInput)
	}
	if service.lastInput.CreatedByUserID != "ou-test-user" {
		t.Fatalf("expected console builds to record the session user, got %+v", service.lastInput)
	}
	if response.Build.CreatedBy == nil || *response.Build.CreatedBy != (userSummaryResponse{ID: "ou-test-user", Name: "测试用户", AvatarURL: "https://example.com/avatar.png"}) {
		t.Fatalf("unexpected createdBy in response: %#v", response.Build.CreatedBy)
	}
	if response.Build.Branch != "release/1.2.0" {
		t.Fatalf("expected branch in response, got %#v", response.Build)
	}
	if response.Build.TriggerType != "webhook" || response.Build.TriggerID != "delivery-1" {
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds?applicationId=app-main&status=pending&moduleId=module-profile&environment=qa&triggerType=webhook&page=2&pageSize=50", nil)
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
	if response.Builds[0].TriggerType != "webhook" || response.Builds[0].TriggerID != "delivery-1" {
		t.Fatalf("expected trigger in list response, got %#v", response.Builds[0])
	}
	if response.Summary.Total != 10 || response.Summary.Succeeded != 4 {
		t.Fatalf("unexpected build summary response: %#v", response.Summary)
	}
	if response.Pagination.Page != 2 || response.Pagination.PageSize != 50 || response.Pagination.Total != 10 {
		t.Fatalf("unexpected pagination: %+v", response.Pagination)
	}
	if service.lastListFilters.ApplicationID != "app-main" ||
		service.lastListFilters.Status != domain.BuildStatusPending ||
		service.lastListFilters.ModuleID != "module-profile" ||
		service.lastListFilters.Environment != "qa" ||
		service.lastListFilters.TriggerType != domain.BuildTriggerTypeWebhook ||
		service.lastListFilters.Pagination != (domain.Pagination{Page: 2, PageSize: 50}) {
		t.Fatalf("unexpected list filters: %+v", service.lastListFilters)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("build response must not expose storage secrets: %s", rec.Body.String())
	}
}

func TestGetBuildRecord(t *testing.T) {
	record := testHTTPBuildRecord()
	record.CreatedByUserID = ""
	record.CreatedBy = nil
	service := &fakeBuildService{
		record: record,
	}
	e := New(config.Config{}, Dependencies{
		BuildService: service,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-1", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	rawBody := append([]byte(nil), rec.Body.Bytes()...)
	var response buildRecordDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Build.ID != "build-1" || response.Build.ModuleID != "module-profile" {
		t.Fatalf("unexpected build detail response: %#v", response.Build)
	}
	if response.Build.ManifestKey != "rn-bundles/seaooo/profile/QA/build-1/manifest.json" ||
		response.Build.FileCount != 2 ||
		response.Build.TotalSizeBytes != 12 ||
		len(response.Build.ArtifactTree.Nodes) != 1 {
		t.Fatalf("unexpected artifact tree response: %#v", response.Build)
	}
	if service.lastGetID != "build-1" {
		t.Fatalf("expected build id to be passed to service, got %q", service.lastGetID)
	}
	// 历史构建没有创建人时 createdBy 必须序列化为 null，而不是省略字段。
	var raw map[string]map[string]any
	if err := json.Unmarshal(rawBody, &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	if createdBy, ok := raw["build"]["createdBy"]; !ok || createdBy != nil {
		t.Fatalf("expected createdBy to be null, got %#v (present=%v)", createdBy, ok)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("build response must not expose storage secrets: %s", rec.Body.String())
	}
}

func TestGetBuildRecordReturnsDependencyBuilds(t *testing.T) {
	record := testHTTPBuildRecord()
	record.DependencyBuilds = []domain.BuildDependency{{
		ModuleID: "common", ModuleName: "Common", RuntimeVersion: "1.0", BuildRecordID: "build-common", BuildNumber: 9,
	}}
	e := New(config.Config{}, Dependencies{BuildService: &fakeBuildService{record: record}})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	var response struct {
		Build struct {
			DependencyBuilds []map[string]any `json:"dependencyBuilds"`
		} `json:"build"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := map[string]any{"moduleId": "common", "moduleName": "Common", "runtimeVersion": "1.0", "buildRecordId": "build-common", "buildNumber": float64(9)}
	if len(response.Build.DependencyBuilds) != 1 || !reflect.DeepEqual(response.Build.DependencyBuilds[0], want) {
		t.Fatalf("unexpected dependencyBuilds: %#v", response.Build.DependencyBuilds)
	}

	// 没有依赖时序列化为空数组而不是 null。
	rec = httptest.NewRecorder()
	e = New(config.Config{}, Dependencies{BuildService: &fakeBuildService{record: testHTTPBuildRecord()}})
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-1", nil))
	if !strings.Contains(rec.Body.String(), `"dependencyBuilds":[]`) {
		t.Fatalf("expected empty dependencyBuilds array, got %s", rec.Body.String())
	}
}

func TestGetBuildRecordNotFound(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		BuildService: &fakeBuildService{
			err: domain.ErrBuildRecordNotFound,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/missing", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}

func TestCreateBuildRecordRejectsInvalidRequest(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		BuildService: &fakeBuildService{
			err: domain.ErrInvalidBuildInput,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", jsonBody(buildCreateRequest{
		ModuleID: "bad module",
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestCreateBuildRecordModuleNotFound(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		BuildService: &fakeBuildService{
			err: domain.ErrModuleNotFound,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", jsonBody(buildCreateRequest{
		ModuleID:    "missing",
		Environment: "qa",
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}

type fakeBuildService struct {
	record          domain.BuildRecord
	records         []domain.BuildRecord
	err             error
	lastGetID       string
	lastInput       domain.BuildRecordInput
	lastListFilters domain.BuildRecordFilters
	summary         domain.BuildRecordSummary
}

func (s *fakeBuildService) CreateBuildRecord(_ context.Context, input domain.BuildRecordInput) (domain.BuildRecord, error) {
	s.lastInput = input
	if s.err != nil {
		return domain.BuildRecord{}, s.err
	}
	return s.record, nil
}

func (s *fakeBuildService) GetBuildRecord(_ context.Context, id string) (domain.BuildRecord, error) {
	s.lastGetID = id
	if s.err != nil {
		return domain.BuildRecord{}, s.err
	}
	return s.record, nil
}

func (s *fakeBuildService) ListBuildRecords(_ context.Context, filters domain.BuildRecordFilters) ([]domain.BuildRecord, domain.BuildRecordSummary, error) {
	s.lastListFilters = filters
	if s.err != nil {
		return nil, domain.BuildRecordSummary{}, s.err
	}
	return s.records, s.summary, nil
}

func testHTTPBuildRecord() domain.BuildRecord {
	startedAt := time.Date(2026, 6, 16, 11, 0, 0, 0, time.UTC)
	return domain.BuildRecord{
		ID:                 "build-1",
		ApplicationName:    "seaooo",
		ModuleID:           "module-profile",
		ModuleName:         "profile",
		Environment:        "qa",
		EnvironmentName:    "QA",
		CodeSourceProvider: domain.CodeSourceProviderGitLab,
		Repository: domain.ModuleRepository{
			ConnectionID:         "code-source-1",
			ExternalRepositoryID: "1001",
			FullName:             "mi-car/profile",
		},
		Branch:                   "release/1.2.0",
		CommitSHA:                "abc123",
		RuntimeVersion:           "1.2.0-725",
		Platforms:                []string{"android", "ios"},
		DefaultBranch:            "main",
		CreatedByUserID:          "ou-test-user",
		CreatedBy:                &domain.UserSummary{ID: "ou-test-user", Name: "测试用户", AvatarURL: "https://example.com/avatar.png"},
		StorageConfigurationID:   "storage-1",
		StorageConfigurationName: "QA 存储",
		StorageSecretAccessKey:   "secret",
		ArtifactKey:              "rn-bundles/seaooo/profile/QA/build-1",
		ArtifactURL:              "https://cdn.example.com/rn-bundles/seaooo/profile/QA/build-1",
		ManifestKey:              "rn-bundles/seaooo/profile/QA/build-1/manifest.json",
		FileCount:                2,
		TotalSizeBytes:           12,
		ArtifactTree: domain.ArtifactTree{
			FileCount:      2,
			TotalSizeBytes: 12,
			Nodes: []domain.ArtifactTreeNode{
				{
					Name:      "assets",
					Path:      "assets",
					Type:      "directory",
					SizeBytes: 12,
					Children: []domain.ArtifactTreeNode{
						{
							Name:        "icon.png",
							Path:        "assets/icon.png",
							Type:        "file",
							SizeBytes:   12,
							ObjectKey:   "rn-bundles/seaooo/profile/QA/build-1/assets/icon.png",
							ContentType: "image/png",
							SHA256:      "hash",
						},
					},
				},
			},
		},
		Status:      domain.BuildStatusPending,
		TriggerID:   "delivery-1",
		TriggerType: domain.BuildTriggerTypeWebhook,
		QueuedAt:    time.Date(2026, 6, 16, 10, 59, 0, 0, time.UTC),
		StartedAt:   &startedAt,
		CreatedAt:   time.Date(2026, 6, 16, 10, 59, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 6, 16, 10, 59, 0, 0, time.UTC),
	}
}
