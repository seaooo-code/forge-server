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
	service := NewStorageService(&repository, nil)

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
	service := NewStorageService(&repository, nil)

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
	service := NewStorageService(&repository, nil)

	input := validStorageInput()
	input.AccessKeyID = " access-key "
	input.SecretAccessKey = " secret-key "
	input.Status = domain.StorageStatusHealthy

	_, err := service.CreateStorageConfiguration(context.Background(), input)
	if err != nil {
		t.Fatalf("create storage configuration: %v", err)
	}

	if repository.lastCreateInput.Name != "生产 OSS" {
		t.Fatalf("expected trimmed name, got %q", repository.lastCreateInput.Name)
	}
	if repository.lastCreateInput.Owner != "rn-platform" {
		t.Fatalf("expected trimmed owner, got %q", repository.lastCreateInput.Owner)
	}
	if repository.lastCreateInput.OwnerUserID != "ou-rn-platform" {
		t.Fatalf("expected trimmed owner user id, got %q", repository.lastCreateInput.OwnerUserID)
	}
	if repository.lastCreateInput.Status != domain.StorageStatusHealthy {
		t.Fatalf("expected healthy status, got %q", repository.lastCreateInput.Status)
	}
	if repository.lastCreateInput.AccessKeyID != "access-key" || repository.lastCreateInput.SecretAccessKey != "secret-key" {
		t.Fatalf("expected trimmed secrets, got %#v", repository.lastCreateInput)
	}
}

func TestStorageServiceUpdatesWithCompleteSecrets(t *testing.T) {
	repository := fakeStorageRepository{
		configuration: testStorageConfiguration("storage-prod"),
	}
	service := NewStorageService(&repository, nil)

	input := validStorageInput()
	input.AccessKeyID = "access-key"
	input.SecretAccessKey = "secret-key"
	input.Status = domain.StorageStatusDisabled

	_, err := service.UpdateStorageConfiguration(context.Background(), " storage-prod ", input)
	if err != nil {
		t.Fatalf("update storage configuration: %v", err)
	}

	if repository.lastUpdateID != "storage-prod" {
		t.Fatalf("expected trimmed id, got %q", repository.lastUpdateID)
	}
	if repository.lastUpdateInput.AccessKeyID != "access-key" || repository.lastUpdateInput.SecretAccessKey != "secret-key" {
		t.Fatalf("expected complete secrets, got %#v", repository.lastUpdateInput)
	}
	if repository.lastUpdateInput.Status != domain.StorageStatusDisabled {
		t.Fatalf("expected disabled status, got %q", repository.lastUpdateInput.Status)
	}
}

func TestStorageServiceRejectsPartialSecretUpdate(t *testing.T) {
	repository := fakeStorageRepository{}
	service := NewStorageService(&repository, nil)

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

func TestStorageServiceTestsProvidedConfiguration(t *testing.T) {
	repository := fakeStorageRepository{}
	tester := fakeStorageConnectivityTester{}
	service := NewStorageService(&repository, &tester)

	input := validStorageInput()
	input.AccessKeyID = " access-key "
	input.SecretAccessKey = " secret-key "

	result, err := service.TestStorageConfiguration(context.Background(), input)
	if err != nil {
		t.Fatalf("test storage configuration: %v", err)
	}

	if !result.OK || result.TestedAt.IsZero() {
		t.Fatalf("expected successful test result, got %#v", result)
	}
	if tester.calls != 1 {
		t.Fatalf("expected tester call, got %d", tester.calls)
	}
	if tester.lastInput.AccessKeyID != "access-key" || tester.lastInput.SecretAccessKey != "secret-key" {
		t.Fatalf("expected trimmed provided credentials, got %#v", tester.lastInput)
	}
}

func TestStorageServiceReturnsTestFailure(t *testing.T) {
	repository := fakeStorageRepository{}
	tester := fakeStorageConnectivityTester{err: domain.ErrStorageConfigurationTestFailed}
	service := NewStorageService(&repository, &tester)

	input := validStorageInput()
	input.AccessKeyID = "access-key"
	input.SecretAccessKey = "secret-key"

	_, err := service.TestStorageConfiguration(context.Background(), input)
	if !errors.Is(err, domain.ErrStorageConfigurationTestFailed) {
		t.Fatalf("expected storage configuration test failure, got %v", err)
	}
}

func TestStorageServiceReturnsDeleteDependencyError(t *testing.T) {
	repository := fakeStorageRepository{
		deleteErr: domain.ErrStorageConfigurationInUse,
	}
	service := NewStorageService(&repository, nil)

	_, err := service.DeleteStorageConfiguration(context.Background(), " storage-prod ")
	if !errors.Is(err, domain.ErrStorageConfigurationInUse) {
		t.Fatalf("expected storage configuration dependency error, got %v", err)
	}
	if repository.lastDeleteID != "storage-prod" {
		t.Fatalf("expected trimmed id, got %q", repository.lastDeleteID)
	}
}

type fakeStorageRepository struct {
	configuration   domain.StorageConfiguration
	configurations  []domain.StorageConfiguration
	lastCreateInput domain.StorageConfigurationInput
	lastUpdateID    string
	lastUpdateInput domain.StorageConfigurationInput
	lastDeleteID    string
	deleteErr       error
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

func (r *fakeStorageRepository) DeleteStorageConfiguration(_ context.Context, id string) (domain.StorageConfiguration, error) {
	r.lastDeleteID = id
	if r.deleteErr != nil {
		return domain.StorageConfiguration{}, r.deleteErr
	}
	return r.configuration, nil
}

type fakeStorageConnectivityTester struct {
	err       error
	lastInput domain.StorageConfigurationInput
	calls     int
}

func (t *fakeStorageConnectivityTester) TestStorageConfiguration(_ context.Context, input domain.StorageConfigurationInput) error {
	t.calls++
	t.lastInput = input
	return t.err
}

func validStorageInput() domain.StorageConfigurationInput {
	return domain.StorageConfigurationInput{
		Name:           " 生产 OSS ",
		Adapter:        domain.StorageAdapterS3,
		Bucket:         " xiaomi-auto-rn-prod ",
		Endpoint:       " https://oss-cn-beijing.aliyuncs.com ",
		Region:         " cn-beijing ",
		OwnerUserID:    " ou-rn-platform ",
		Owner:          " rn-platform ",
		OwnerEmail:     " rn-platform@xiaomi.com ",
		OwnerAvatarURL: " https://example.com/rn-platform.png ",
		PublicBaseURL:  " https://cdn.example.com/rn/prod ",
		Status:         domain.StorageStatusHealthy,
	}
}

func testStorageConfiguration(id string) domain.StorageConfiguration {
	now := time.Date(2026, 6, 12, 10, 30, 0, 0, time.UTC)
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
		Status:         domain.StorageStatusHealthy,
		LastVerifiedAt: &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}
