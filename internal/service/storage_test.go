package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"forge-server/internal/domain"
)

func TestStorageServiceRejectsMissingRequiredFields(t *testing.T) {
	repository := fakeStorageRepository{}
	service := NewStorageService(&repository)

	_, err := service.CreateStorageConfiguration(context.Background(), domain.StorageConfigurationInput{
		Name:            "生产 OSS",
		Adapter:         domain.StorageAdapterS3,
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
	})
	if !errors.Is(err, domain.ErrInvalidStorageConfigurationInput) {
		t.Fatalf("expected invalid storage configuration input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.createCalls)
	}
}

func TestStorageServiceRequiresSecretsOnCreate(t *testing.T) {
	repository := fakeStorageRepository{}
	service := NewStorageService(&repository)

	_, err := service.CreateStorageConfiguration(context.Background(), validStorageInput())
	if !errors.Is(err, domain.ErrInvalidStorageConfigurationInput) {
		t.Fatalf("expected invalid storage configuration input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.createCalls)
	}
}

func TestStorageServiceNormalizesCreateInput(t *testing.T) {
	repository := fakeStorageRepository{
		configuration: testStorageConfiguration("storage-prod"),
	}
	service := NewStorageService(&repository)

	input := validStorageInput()
	input.AccessKeyID = " access-key "
	input.SecretAccessKey = " secret-key "
	input.Status = domain.StorageStatus("正常")
	input.Owner = ""

	_, err := service.CreateStorageConfiguration(context.Background(), input)
	if err != nil {
		t.Fatalf("create storage configuration: %v", err)
	}

	if repository.lastCreateInput.Name != "生产 OSS" {
		t.Fatalf("expected trimmed name, got %q", repository.lastCreateInput.Name)
	}
	if repository.lastCreateInput.Owner != "rn-platform" {
		t.Fatalf("expected default owner, got %q", repository.lastCreateInput.Owner)
	}
	if repository.lastCreateInput.Status != domain.StorageStatusHealthy {
		t.Fatalf("expected healthy status, got %q", repository.lastCreateInput.Status)
	}
	if repository.lastCreateInput.AccessKeyID != "access-key" || repository.lastCreateInput.SecretAccessKey != "secret-key" {
		t.Fatalf("expected trimmed secrets, got %#v", repository.lastCreateInput)
	}
}

func TestStorageServiceAllowsEmptySecretsOnUpdate(t *testing.T) {
	repository := fakeStorageRepository{
		configuration: testStorageConfiguration("storage-prod"),
	}
	service := NewStorageService(&repository)

	input := validStorageInput()
	input.Status = domain.StorageStatus("已停用")

	_, err := service.UpdateStorageConfiguration(context.Background(), " storage-prod ", input)
	if err != nil {
		t.Fatalf("update storage configuration: %v", err)
	}

	if repository.lastUpdateID != "storage-prod" {
		t.Fatalf("expected trimmed id, got %q", repository.lastUpdateID)
	}
	if repository.lastUpdateInput.AccessKeyID != "" || repository.lastUpdateInput.SecretAccessKey != "" {
		t.Fatalf("expected empty secrets to be forwarded for preserve semantics, got %#v", repository.lastUpdateInput)
	}
	if repository.lastUpdateInput.Status != domain.StorageStatusDisabled {
		t.Fatalf("expected disabled status, got %q", repository.lastUpdateInput.Status)
	}
}

func TestStorageServiceRejectsPartialSecretUpdate(t *testing.T) {
	repository := fakeStorageRepository{}
	service := NewStorageService(&repository)

	input := validStorageInput()
	input.AccessKeyID = "access-key"

	_, err := service.UpdateStorageConfiguration(context.Background(), "storage-prod", input)
	if !errors.Is(err, domain.ErrInvalidStorageConfigurationInput) {
		t.Fatalf("expected invalid storage configuration input, got %v", err)
	}
	if repository.updateCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.updateCalls)
	}
}

type fakeStorageRepository struct {
	configuration   domain.StorageConfiguration
	configurations  []domain.StorageConfiguration
	lastCreateInput domain.StorageConfigurationInput
	lastUpdateID    string
	lastUpdateInput domain.StorageConfigurationInput
	createCalls     int
	updateCalls     int
}

func (r *fakeStorageRepository) ListStorageConfigurations(context.Context) ([]domain.StorageConfiguration, error) {
	return r.configurations, nil
}

func (r *fakeStorageRepository) GetStorageConfiguration(context.Context, string) (domain.StorageConfiguration, error) {
	return r.configuration, nil
}

func (r *fakeStorageRepository) CreateStorageConfiguration(_ context.Context, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error) {
	r.createCalls++
	r.lastCreateInput = input
	return r.configuration, nil
}

func (r *fakeStorageRepository) UpdateStorageConfiguration(_ context.Context, id string, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error) {
	r.updateCalls++
	r.lastUpdateID = id
	r.lastUpdateInput = input
	return r.configuration, nil
}

func (r *fakeStorageRepository) DeleteStorageConfiguration(context.Context, string) (domain.StorageConfiguration, error) {
	return r.configuration, nil
}

func validStorageInput() domain.StorageConfigurationInput {
	return domain.StorageConfigurationInput{
		Name:          " 生产 OSS ",
		Adapter:       domain.StorageAdapterS3,
		Bucket:        " xiaomi-auto-rn-prod ",
		Endpoint:      " oss-cn-beijing.aliyuncs.com ",
		Region:        " cn-beijing ",
		Owner:         " rn-platform ",
		PublicBaseURL: " https://cdn.example.com/rn/prod ",
		Status:        domain.StorageStatusHealthy,
	}
}

func testStorageConfiguration(id string) domain.StorageConfiguration {
	now := time.Date(2026, 6, 12, 10, 30, 0, 0, time.UTC)
	return domain.StorageConfiguration{
		ID:             id,
		Name:           "生产 OSS",
		Adapter:        domain.StorageAdapterS3,
		Bucket:         "xiaomi-auto-rn-prod",
		Endpoint:       "oss-cn-beijing.aliyuncs.com",
		Region:         "cn-beijing",
		Owner:          "rn-platform",
		PublicBaseURL:  "https://cdn.example.com/rn/prod",
		Status:         domain.StorageStatusHealthy,
		LastVerifiedAt: &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}
