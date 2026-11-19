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

func TestListStorageConfigurations(t *testing.T) {
	service := &fakeStorageService{
		configurations: []domain.StorageConfiguration{
			testHTTPStorageConfiguration("storage-prod", domain.StorageStatusHealthy),
			testHTTPStorageConfiguration("storage-minio", domain.StorageStatusError),
		},
	}
	e := New(config.Config{}, Dependencies{
		StorageService: service,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/storage/configurations?page=2&pageSize=50", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response storageConfigurationsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Configurations) != 2 {
		t.Fatalf("expected 2 storage configurations, got %d", len(response.Configurations))
	}
	if response.Configurations[0].Status != "healthy" {
		t.Fatalf("expected healthy status, got %q", response.Configurations[0].Status)
	}
	if response.Configurations[1].Status != "error" {
		t.Fatalf("expected error status, got %q", response.Configurations[1].Status)
	}
	if response.Configurations[0].LastVerifiedAt == "" {
		t.Fatal("expected last verified time")
	}
	if response.Configurations[0].OwnerUserID != "ou-rn-platform" || response.Configurations[0].OwnerEmail == "" || response.Configurations[0].OwnerAvatarURL == "" {
		t.Fatalf("expected owner profile fields, got %#v", response.Configurations[0])
	}
	if response.Pagination.Page != 2 || response.Pagination.PageSize != 50 || response.Pagination.Total != 2 || service.lastPagination != (domain.Pagination{Page: 2, PageSize: 50}) {
		t.Fatalf("unexpected pagination: response=%+v service=%+v", response.Pagination, service.lastPagination)
	}
}

func TestGetStorageConfiguration(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		StorageService: &fakeStorageService{
			configuration: testHTTPStorageConfiguration("storage-prod", domain.StorageStatusHealthy),
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/storage/configurations/storage-prod", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response storageConfigurationDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Configuration.ID != "storage-prod" {
		t.Fatalf("expected storage-prod, got %q", response.Configuration.ID)
	}
}

func TestCreateStorageConfiguration(t *testing.T) {
	service := &fakeStorageService{
		configuration: testHTTPStorageConfiguration("storage-prod", domain.StorageStatusHealthy),
	}
	e := newAuthorizedTestServer(Dependencies{StorageService: service})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/storage/configurations", jsonBody(storageRequest()))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}
	if service.lastCreateInput.Status != domain.StorageStatusHealthy {
		t.Fatalf("expected healthy status, got %q", service.lastCreateInput.Status)
	}
	if service.lastCreateInput.SecretAccessKey != "secret-key" {
		t.Fatalf("expected secret to be bound")
	}
	if service.lastCreateInput.OwnerUserID != "ou-rn-platform" || service.lastCreateInput.OwnerEmail == "" || service.lastCreateInput.OwnerAvatarURL == "" {
		t.Fatalf("expected owner profile input, got %#v", service.lastCreateInput)
	}
}

func TestUpdateStorageConfigurationRequiresCompletePayload(t *testing.T) {
	service := &fakeStorageService{
		configuration: testHTTPStorageConfiguration("storage-prod", domain.StorageStatusDisabled),
	}
	e := newAuthorizedTestServer(Dependencies{StorageService: service})

	request := storageRequest()
	request["status"] = "disabled"
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/storage/configurations/storage-prod", jsonBody(request))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if service.lastUpdateID != "storage-prod" {
		t.Fatalf("expected storage id, got %q", service.lastUpdateID)
	}
	if service.lastUpdateInput.AccessKeyID != "access-key" || service.lastUpdateInput.SecretAccessKey != "secret-key" {
		t.Fatalf("expected complete secrets, got %#v", service.lastUpdateInput)
	}
	if service.lastUpdateInput.Status != domain.StorageStatusDisabled {
		t.Fatalf("expected disabled status, got %q", service.lastUpdateInput.Status)
	}
}

func TestDeleteStorageConfiguration(t *testing.T) {
	service := &fakeStorageService{
		configuration: testHTTPStorageConfiguration("storage-prod", domain.StorageStatusHealthy),
	}
	e := newAuthorizedTestServer(Dependencies{StorageService: service})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/storage/configurations/storage-prod", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if service.lastDeleteID != "storage-prod" {
		t.Fatalf("expected storage id, got %q", service.lastDeleteID)
	}
}

func TestDeleteStorageConfigurationInUse(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		StorageService: &fakeStorageService{
			err: domain.ErrStorageConfigurationInUse,
		},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/storage/configurations/storage-prod", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d: %s", http.StatusConflict, rec.Code, rec.Body.String())
	}

	var response map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["error"] != "storage configuration is referenced by environments" {
		t.Fatalf("expected dependency error, got %#v", response)
	}
}

func TestTestStorageConfiguration(t *testing.T) {
	service := &fakeStorageService{
		testResult: domain.StorageConfigurationTestResult{
			OK:       true,
			TestedAt: time.Date(2026, 6, 12, 11, 20, 0, 0, time.UTC),
		},
	}
	e := newAuthorizedTestServer(Dependencies{StorageService: service})

	request := storageRequest()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/storage/configurations/test", jsonBody(request))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if service.lastTestInput.SecretAccessKey != "secret-key" {
		t.Fatalf("expected secret to be bound")
	}

	var response storageConfigurationTestResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.OK || response.TestedAt == "" {
		t.Fatalf("expected successful test response, got %#v", response)
	}
}

func TestTestStorageConfigurationBindsExistingID(t *testing.T) {
	service := &fakeStorageService{
		testResult: domain.StorageConfigurationTestResult{OK: true, TestedAt: time.Date(2026, 6, 12, 11, 20, 0, 0, time.UTC)},
	}
	e := newAuthorizedTestServer(Dependencies{StorageService: service})

	request := storageRequest()
	request["accessKeyId"] = ""
	request["secretAccessKey"] = ""
	request["id"] = "storage-prod"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/storage/configurations/test", jsonBody(request))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if service.lastTestID != "storage-prod" {
		t.Fatalf("expected storage id to be bound, got %q", service.lastTestID)
	}
}

func TestTestStorageConfigurationFailure(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		StorageService: &fakeStorageService{
			err: domain.ErrStorageConfigurationTestFailed,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/storage/configurations/test", jsonBody(storageRequest()))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadGateway, rec.Code, rec.Body.String())
	}
}

func TestStorageConfigurationNotFound(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		StorageService: &fakeStorageService{
			err: domain.ErrStorageConfigurationNotFound,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/storage/configurations/unknown", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}

func TestStorageConfigurationRejectsInvalidStatus(t *testing.T) {
	e := New(config.Config{}, Dependencies{StorageService: &fakeStorageService{}})

	request := storageRequest()
	request["status"] = "unknown"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/storage/configurations", jsonBody(request))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

type fakeStorageService struct {
	configuration   domain.StorageConfiguration
	configurations  []domain.StorageConfiguration
	testResult      domain.StorageConfigurationTestResult
	err             error
	lastCreateInput domain.StorageConfigurationInput
	lastUpdateID    string
	lastUpdateInput domain.StorageConfigurationInput
	lastDeleteID    string
	lastTestInput   domain.StorageConfigurationInput
	lastTestID      string
	lastPagination  domain.Pagination
}

func (s *fakeStorageService) ListStorageConfigurations(_ context.Context, pagination domain.Pagination) ([]domain.StorageConfiguration, int, error) {
	s.lastPagination = pagination
	if s.err != nil {
		return nil, 0, s.err
	}
	return s.configurations, len(s.configurations), nil
}

func (s fakeStorageService) GetStorageConfiguration(context.Context, string) (domain.StorageConfiguration, error) {
	if s.err != nil {
		return domain.StorageConfiguration{}, s.err
	}
	return s.configuration, nil
}

func (s *fakeStorageService) CreateStorageConfiguration(_ context.Context, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error) {
	s.lastCreateInput = input
	if s.err != nil {
		return domain.StorageConfiguration{}, s.err
	}
	return s.configuration, nil
}

func (s *fakeStorageService) UpdateStorageConfiguration(_ context.Context, id string, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error) {
	s.lastUpdateID = id
	s.lastUpdateInput = input
	if s.err != nil {
		return domain.StorageConfiguration{}, s.err
	}
	return s.configuration, nil
}

func (s *fakeStorageService) DeleteStorageConfiguration(_ context.Context, id string) (domain.StorageConfiguration, error) {
	s.lastDeleteID = id
	if s.err != nil {
		return domain.StorageConfiguration{}, s.err
	}
	return s.configuration, nil
}

func (s *fakeStorageService) TestStorageConfiguration(_ context.Context, id string, input domain.StorageConfigurationInput) (domain.StorageConfigurationTestResult, error) {
	s.lastTestID = id
	s.lastTestInput = input
	if s.err != nil {
		return domain.StorageConfigurationTestResult{}, s.err
	}
	return s.testResult, nil
}

func testHTTPStorageConfiguration(id string, status domain.StorageStatus) domain.StorageConfiguration {
	lastVerifiedAt := time.Date(2026, 6, 12, 10, 30, 0, 0, time.UTC)
	return domain.StorageConfiguration{
		ID:             id,
		Name:           "生产 OSS",
		Adapter:        domain.StorageAdapterS3,
		Bucket:         "xiaomi-auto-rn-prod",
		Endpoint:       "https://oss-cn-beijing.aliyuncs.com",
		Region:         "cn-beijing",
		OwnerUserID:    "ou-rn-platform",
		Owner:          "rn-platform",
		OwnerEmail:     "rn-platform@xiaomi.com",
		OwnerAvatarURL: "https://example.com/rn-platform.png",
		PublicBaseURL:  "https://cdn.example.com/rn/prod",
		Status:         status,
		LastVerifiedAt: &lastVerifiedAt,
	}
}

func storageRequest() map[string]string {
	return map[string]string{
		"accessKeyId":     "access-key",
		"adapter":         "S3",
		"bucket":          "xiaomi-auto-rn-prod",
		"endpoint":        "https://oss-cn-beijing.aliyuncs.com",
		"name":            "生产 OSS",
		"ownerUserId":     "ou-rn-platform",
		"owner":           "rn-platform",
		"ownerEmail":      "rn-platform@xiaomi.com",
		"ownerAvatarUrl":  "https://example.com/rn-platform.png",
		"publicBaseUrl":   "https://cdn.example.com/rn/prod",
		"region":          "cn-beijing",
		"secretAccessKey": "secret-key",
		"status":          "healthy",
	}
}

var _ StorageService = (*fakeStorageService)(nil)

func TestStorageConfigurationForcePathStyleRoundTrip(t *testing.T) {
	configuration := testHTTPStorageConfiguration("storage-fds", domain.StorageStatusHealthy)
	configuration.ForcePathStyle = true
	service := &fakeStorageService{configuration: configuration}
	e := newAuthorizedTestServer(Dependencies{StorageService: service})

	body := map[string]any{}
	for key, value := range storageRequest() {
		body[key] = value
	}
	body["forcePathStyle"] = true
	req := httptest.NewRequest(http.MethodPost, "/api/v1/storage/configurations", jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}
	if !service.lastCreateInput.ForcePathStyle {
		t.Fatal("expected forcePathStyle to be bound into the create input")
	}
	var response struct {
		Configuration storageConfigurationResponse `json:"configuration"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Configuration.ForcePathStyle {
		t.Fatalf("expected forcePathStyle in response, got %#v", response.Configuration)
	}
}
