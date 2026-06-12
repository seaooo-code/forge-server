package service

import (
	"context"
	"strings"

	"forge-server/internal/domain"
)

type StorageRepository interface {
	CreateStorageConfiguration(ctx context.Context, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error)
	DeleteStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error)
	GetStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error)
	ListStorageConfigurations(ctx context.Context) ([]domain.StorageConfiguration, error)
	UpdateStorageConfiguration(ctx context.Context, id string, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error)
}

type StorageService struct {
	repository StorageRepository
}

func NewStorageService(repository StorageRepository) *StorageService {
	return &StorageService{repository: repository}
}

func (s *StorageService) ListStorageConfigurations(ctx context.Context) ([]domain.StorageConfiguration, error) {
	return s.repository.ListStorageConfigurations(ctx)
}

func (s *StorageService) GetStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error) {
	normalizedID := strings.TrimSpace(id)
	if normalizedID == "" {
		return domain.StorageConfiguration{}, domain.ErrStorageConfigurationNotFound
	}
	return s.repository.GetStorageConfiguration(ctx, normalizedID)
}

func (s *StorageService) CreateStorageConfiguration(ctx context.Context, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error) {
	normalized, err := validateStorageConfigurationInput(input, true)
	if err != nil {
		return domain.StorageConfiguration{}, err
	}
	return s.repository.CreateStorageConfiguration(ctx, normalized)
}

func (s *StorageService) UpdateStorageConfiguration(ctx context.Context, id string, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error) {
	normalizedID := strings.TrimSpace(id)
	if normalizedID == "" {
		return domain.StorageConfiguration{}, domain.ErrStorageConfigurationNotFound
	}

	normalized, err := validateStorageConfigurationInput(input, false)
	if err != nil {
		return domain.StorageConfiguration{}, err
	}
	return s.repository.UpdateStorageConfiguration(ctx, normalizedID, normalized)
}

func (s *StorageService) DeleteStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error) {
	normalizedID := strings.TrimSpace(id)
	if normalizedID == "" {
		return domain.StorageConfiguration{}, domain.ErrStorageConfigurationNotFound
	}
	return s.repository.DeleteStorageConfiguration(ctx, normalizedID)
}

func validateStorageConfigurationInput(input domain.StorageConfigurationInput, requireSecret bool) (domain.StorageConfigurationInput, error) {
	normalized := domain.StorageConfigurationInput{
		Name:            strings.TrimSpace(input.Name),
		Adapter:         input.Adapter,
		Bucket:          strings.TrimSpace(input.Bucket),
		Endpoint:        strings.TrimSpace(input.Endpoint),
		Region:          strings.TrimSpace(input.Region),
		Owner:           strings.TrimSpace(input.Owner),
		PublicBaseURL:   strings.TrimSpace(input.PublicBaseURL),
		AccessKeyID:     strings.TrimSpace(input.AccessKeyID),
		SecretAccessKey: strings.TrimSpace(input.SecretAccessKey),
		Status:          input.Status,
	}

	if normalized.Adapter == "" {
		normalized.Adapter = domain.StorageAdapterS3
	}
	if normalized.Status == "" {
		normalized.Status = domain.StorageStatusHealthy
	}
	if normalized.Owner == "" {
		normalized.Owner = "rn-platform"
	}

	if normalized.Name == "" || normalized.Bucket == "" || normalized.Endpoint == "" {
		return domain.StorageConfigurationInput{}, domain.ErrInvalidStorageConfigurationInput
	}
	if !domain.IsValidStorageAdapter(normalized.Adapter) {
		return domain.StorageConfigurationInput{}, domain.ErrInvalidStorageConfigurationInput
	}
	status, ok := domain.ParseStorageStatus(string(normalized.Status))
	if !ok {
		return domain.StorageConfigurationInput{}, domain.ErrInvalidStorageConfigurationInput
	}
	normalized.Status = status
	if requireSecret && (normalized.AccessKeyID == "" || normalized.SecretAccessKey == "") {
		return domain.StorageConfigurationInput{}, domain.ErrInvalidStorageConfigurationInput
	}
	if !requireSecret && ((normalized.AccessKeyID == "") != (normalized.SecretAccessKey == "")) {
		return domain.StorageConfigurationInput{}, domain.ErrInvalidStorageConfigurationInput
	}

	return normalized, nil
}
