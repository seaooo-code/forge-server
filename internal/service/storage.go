package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"forge-server/internal/domain"
)

type StorageRepository interface {
	CreateStorageConfiguration(ctx context.Context, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error)
	DeleteStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error)
	GetStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error)
	ListStorageConfigurations(ctx context.Context, pagination domain.Pagination) ([]domain.StorageConfiguration, int, error)
	UpdateStorageConfiguration(ctx context.Context, id string, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error)
}

type StorageConnectivityTester interface {
	TestStorageConfiguration(ctx context.Context, input domain.StorageConfigurationInput) error
}

type StorageService struct {
	repository StorageRepository
	tester     StorageConnectivityTester
}

func NewStorageService(repository StorageRepository, tester StorageConnectivityTester) *StorageService {
	return &StorageService{repository: repository, tester: tester}
}

func (s *StorageService) ListStorageConfigurations(ctx context.Context, pagination domain.Pagination) ([]domain.StorageConfiguration, int, error) {
	if !pagination.IsValid() {
		return nil, 0, domain.ErrInvalidStorageConfigurationInput
	}
	return s.repository.ListStorageConfigurations(ctx, pagination)
}

func (s *StorageService) GetStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error) {
	normalizedID := strings.TrimSpace(id)
	if normalizedID == "" {
		return domain.StorageConfiguration{}, domain.ErrStorageConfigurationNotFound
	}
	return s.repository.GetStorageConfiguration(ctx, normalizedID)
}

func (s *StorageService) CreateStorageConfiguration(ctx context.Context, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error) {
	normalized, err := validateStorageConfigurationInput(input)
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

	normalized, err := validateStorageConfigurationInput(input)
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

func (s *StorageService) TestStorageConfiguration(ctx context.Context, input domain.StorageConfigurationInput) (domain.StorageConfigurationTestResult, error) {
	normalized, err := validateStorageConfigurationInput(input)
	if err != nil {
		return domain.StorageConfigurationTestResult{}, err
	}

	if s.tester == nil {
		return domain.StorageConfigurationTestResult{}, domain.ErrStorageConfigurationTestFailed
	}
	if err := s.tester.TestStorageConfiguration(ctx, normalized); err != nil {
		return domain.StorageConfigurationTestResult{}, fmt.Errorf("%w: %v", domain.ErrStorageConfigurationTestFailed, err)
	}

	return domain.StorageConfigurationTestResult{
		OK:       true,
		TestedAt: time.Now().UTC(),
	}, nil
}

func validateStorageConfigurationInput(input domain.StorageConfigurationInput) (domain.StorageConfigurationInput, error) {
	normalized := domain.StorageConfigurationInput{
		Name:               strings.TrimSpace(input.Name),
		Adapter:            input.Adapter,
		Bucket:             strings.TrimSpace(input.Bucket),
		Endpoint:           strings.TrimSpace(input.Endpoint),
		Region:             strings.TrimSpace(input.Region),
		PublicBaseURL:      strings.TrimSpace(input.PublicBaseURL),
		AccessKeyID:        strings.TrimSpace(input.AccessKeyID),
		SecretAccessKey:    strings.TrimSpace(input.SecretAccessKey),
		Status:             input.Status,
		OwnerFeishuUnionID: strings.TrimSpace(input.OwnerFeishuUnionID),
		OwnerFeishuOpenID:  strings.TrimSpace(input.OwnerFeishuOpenID),
		OwnerFeishuUserID:  strings.TrimSpace(input.OwnerFeishuUserID),
	}
	owner := normalizeUserInput(domain.UserInput{
		ID:            input.OwnerUserID,
		Name:          input.Owner,
		Email:         input.OwnerEmail,
		AvatarURL:     input.OwnerAvatarURL,
		FeishuUnionID: input.OwnerFeishuUnionID,
		FeishuOpenID:  input.OwnerFeishuOpenID,
		FeishuUserID:  input.OwnerFeishuUserID,
	})
	normalized.OwnerUserID = owner.ID
	normalized.Owner = owner.Name
	normalized.OwnerEmail = owner.Email
	normalized.OwnerAvatarURL = owner.AvatarURL
	normalized.OwnerFeishuUnionID = owner.FeishuUnionID
	normalized.OwnerFeishuOpenID = owner.FeishuOpenID
	normalized.OwnerFeishuUserID = owner.FeishuUserID

	if normalized.Adapter == "" {
		normalized.Adapter = domain.StorageAdapterS3
	}
	if normalized.Status == "" {
		normalized.Status = domain.StorageStatusHealthy
	}
	if normalized.Name == "" ||
		normalized.Bucket == "" ||
		normalized.Endpoint == "" ||
		normalized.Region == "" ||
		normalized.OwnerUserID == "" ||
		normalized.Owner == "" {
		return domain.StorageConfigurationInput{}, domain.ErrInvalidStorageConfigurationInput
	}
	if !domain.IsValidStorageAdapter(normalized.Adapter) {
		return domain.StorageConfigurationInput{}, domain.ErrInvalidStorageConfigurationInput
	}
	if !domain.IsValidStorageStatus(normalized.Status) {
		return domain.StorageConfigurationInput{}, domain.ErrInvalidStorageConfigurationInput
	}
	if normalized.AccessKeyID == "" || normalized.SecretAccessKey == "" {
		return domain.StorageConfigurationInput{}, domain.ErrInvalidStorageConfigurationInput
	}

	return normalized, nil
}
