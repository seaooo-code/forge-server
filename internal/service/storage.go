package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"forge-server/internal/domain"
)

type StorageRepository interface {
	CreateStorageConfiguration(ctx context.Context, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error)
	DeleteStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error)
	GetStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error)
	GetStorageCredentials(ctx context.Context, id string) (domain.StorageCredentials, error)
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

	if err := s.fillStoredCredentials(ctx, normalizedID, &input); err != nil {
		return domain.StorageConfiguration{}, err
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

// TestStorageConfiguration 测试连通性；id 非空表示测试已有配置，留空的密钥沿用已保存值。
func (s *StorageService) TestStorageConfiguration(ctx context.Context, id string, input domain.StorageConfigurationInput) (domain.StorageConfigurationTestResult, error) {
	if normalizedID := strings.TrimSpace(id); normalizedID != "" {
		if err := s.fillStoredCredentials(ctx, normalizedID, &input); err != nil {
			return domain.StorageConfigurationTestResult{}, err
		}
	}

	normalized, err := validateStorageConfigurationInput(input)
	if err != nil {
		return domain.StorageConfigurationTestResult{}, err
	}

	if s.tester == nil {
		return domain.StorageConfigurationTestResult{}, domain.ErrStorageConfigurationTestFailed
	}
	if err := s.tester.TestStorageConfiguration(ctx, normalized); err != nil {
		var testErr *domain.StorageConfigurationTestError
		if errors.As(err, &testErr) {
			return domain.StorageConfigurationTestResult{}, testErr
		}
		return domain.StorageConfigurationTestResult{}, fmt.Errorf("%w: %v", domain.ErrStorageConfigurationTestFailed, err)
	}

	return domain.StorageConfigurationTestResult{
		OK:       true,
		TestedAt: time.Now().UTC(),
	}, nil
}

// fillStoredCredentials 在编辑已有配置时，两个密钥都留空则沿用已保存的密钥；只填一个仍按非法输入处理。
func (s *StorageService) fillStoredCredentials(ctx context.Context, id string, input *domain.StorageConfigurationInput) error {
	if strings.TrimSpace(input.AccessKeyID) != "" || strings.TrimSpace(input.SecretAccessKey) != "" {
		return nil
	}

	stored, err := s.repository.GetStorageCredentials(ctx, id)
	if err != nil {
		return err
	}
	input.AccessKeyID = stored.AccessKeyID
	input.SecretAccessKey = stored.SecretAccessKey
	return nil
}

func validateStorageConfigurationInput(input domain.StorageConfigurationInput) (domain.StorageConfigurationInput, error) {
	normalized := domain.StorageConfigurationInput{
		Name:            strings.TrimSpace(input.Name),
		Adapter:         input.Adapter,
		Bucket:          strings.TrimSpace(input.Bucket),
		Endpoint:        strings.TrimSpace(input.Endpoint),
		Region:          strings.TrimSpace(input.Region),
		PublicBaseURL:   strings.TrimSpace(input.PublicBaseURL),
		ForcePathStyle:  input.ForcePathStyle,
		AccessKeyID:     strings.TrimSpace(input.AccessKeyID),
		SecretAccessKey: strings.TrimSpace(input.SecretAccessKey),
		Status:          input.Status,
		OwnerUserID:     strings.TrimSpace(input.OwnerUserID),
	}

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
		normalized.OwnerUserID == "" {
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
