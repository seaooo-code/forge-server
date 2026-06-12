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
	e := New(config.Config{}, Dependencies{
		StorageService: &fakeStorageService{
			configurations: []domain.StorageConfiguration{
				testHTTPStorageConfiguration("storage-prod", domain.StorageStatusHealthy),
				testHTTPStorageConfiguration("storage-minio", domain.StorageStatusError),
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/storage/configurations", nil)
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
	if response.Configurations[0].Status != "正常" {
		t.Fatalf("expected healthy status label, got %q", response.Configurations[0].Status)
	}
	if response.Configurations[1].Status != "异常" {
		t.Fatalf("expected error status label, got %q", response.Configurations[1].Status)
	}
	if response.Configurations[0].LastVerifiedAt == "" {
		t.Fatal("expected last verified time")
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
	e := New(config.Config{}, Dependencies{StorageService: service})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/storage/configurations", jsonBody(storageRequest()))
	req.Header.Set("Content-Type", "application/json")
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
}

func TestUpdateStorageConfigurationAllowsEmptySecrets(t *testing.T) {
	service := &fakeStorageService{
		configuration: testHTTPStorageConfiguration("storage-prod", domain.StorageStatusDisabled),
	}
	e := New(config.Config{}, Dependencies{StorageService: service})

	request := storageRequest()
	request["accessKeyId"] = ""
	request["secretAccessKey"] = ""
	request["status"] = "已停用"
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/storage/configurations/storage-prod", jsonBody(request))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if service.lastUpdateID != "storage-prod" {
		t.Fatalf("expected storage id, got %q", service.lastUpdateID)
	}
	if service.lastUpdateInput.AccessKeyID != "" || service.lastUpdateInput.SecretAccessKey != "" {
		t.Fatalf("expected empty secrets, got %#v", service.lastUpdateInput)
	}
	if service.lastUpdateInput.Status != domain.StorageStatusDisabled {
		t.Fatalf("expected disabled status, got %q", service.lastUpdateInput.Status)
	}
}

func TestDeleteStorageConfiguration(t *testing.T) {
	service := &fakeStorageService{
		configuration: testHTTPStorageConfiguration("storage-prod", domain.StorageStatusHealthy),
	}
	e := New(config.Config{}, Dependencies{StorageService: service})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/storage/configurations/storage-prod", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if service.lastDeleteID != "storage-prod" {
		t.Fatalf("expected storage id, got %q", service.lastDeleteID)
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
	err             error
	lastCreateInput domain.StorageConfigurationInput
	lastUpdateID    string
	lastUpdateInput domain.StorageConfigurationInput
	lastDeleteID    string
}

func (s fakeStorageService) ListStorageConfigurations(context.Context) ([]domain.StorageConfiguration, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.configurations, nil
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

func testHTTPStorageConfiguration(id string, status domain.StorageStatus) domain.StorageConfiguration {
	lastVerifiedAt := time.Date(2026, 6, 12, 10, 30, 0, 0, time.UTC)
	return domain.StorageConfiguration{
		ID:             id,
		Name:           "生产 OSS",
		Adapter:        domain.StorageAdapterS3,
		Bucket:         "xiaomi-auto-rn-prod",
		Endpoint:       "oss-cn-beijing.aliyuncs.com",
		Region:         "cn-beijing",
		Owner:          "rn-platform",
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
		"endpoint":        "oss-cn-beijing.aliyuncs.com",
		"name":            "生产 OSS",
		"owner":           "rn-platform",
		"publicBaseUrl":   "https://cdn.example.com/rn/prod",
		"region":          "cn-beijing",
		"secretAccessKey": "secret-key",
		"status":          "正常",
	}
}

var _ StorageService = (*fakeStorageService)(nil)
