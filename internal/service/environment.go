package service

import (
	"context"
	"strings"

	"forge-server/internal/domain"
)

type EnvironmentRepository interface {
	CreateEnvironment(ctx context.Context, input domain.EnvironmentInput) (domain.Environment, error)
	GetEnvironmentDetail(ctx context.Context, code string) (domain.Environment, error)
	ListEnvironments(ctx context.Context) ([]domain.Environment, error)
	UpdateEnvironment(ctx context.Context, code string, input domain.EnvironmentInput) (domain.Environment, error)
}

type EnvironmentService struct {
	repository EnvironmentRepository
}

func NewEnvironmentService(repository EnvironmentRepository) *EnvironmentService {
	return &EnvironmentService{repository: repository}
}

func (s *EnvironmentService) ListEnvironments(ctx context.Context) ([]domain.Environment, error) {
	return s.repository.ListEnvironments(ctx)
}

func (s *EnvironmentService) CreateEnvironment(ctx context.Context, input domain.EnvironmentInput) (domain.Environment, error) {
	normalized, err := validateEnvironmentInput(input)
	if err != nil {
		return domain.Environment{}, err
	}

	return s.repository.CreateEnvironment(ctx, normalized)
}

func (s *EnvironmentService) GetEnvironmentDetail(ctx context.Context, code string) (domain.Environment, error) {
	normalizedCode := strings.TrimSpace(code)
	if normalizedCode == "" {
		return domain.Environment{}, domain.ErrEnvironmentNotFound
	}

	return s.repository.GetEnvironmentDetail(ctx, normalizedCode)
}

func (s *EnvironmentService) UpdateEnvironment(ctx context.Context, code string, input domain.EnvironmentInput) (domain.Environment, error) {
	normalizedCode := strings.TrimSpace(code)
	if normalizedCode == "" {
		return domain.Environment{}, domain.ErrEnvironmentNotFound
	}

	normalized, err := validateEnvironmentInput(input)
	if err != nil {
		return domain.Environment{}, err
	}
	if normalized.Code != normalizedCode {
		return domain.Environment{}, domain.ErrInvalidEnvironmentInput
	}

	return s.repository.UpdateEnvironment(ctx, normalizedCode, normalized)
}

func validateEnvironmentInput(input domain.EnvironmentInput) (domain.EnvironmentInput, error) {
	normalized := domain.EnvironmentInput{
		Code:                   strings.TrimSpace(input.Code),
		Name:                   strings.TrimSpace(input.Name),
		Status:                 input.Status,
		Tone:                   strings.TrimSpace(input.Tone),
		Description:            strings.TrimSpace(input.Description),
		StorageConfigurationID: strings.TrimSpace(input.StorageConfigurationID),
		AllowTestPlans:         input.AllowTestPlans,
	}
	if normalized.Status == "" {
		normalized.Status = domain.EnvironmentStatusHealthy
	}

	if normalized.Code == "" ||
		normalized.Name == "" ||
		normalized.StorageConfigurationID == "" ||
		!isValidEnvironmentCode(normalized.Code) ||
		!domain.IsValidEnvironmentStatus(normalized.Status) ||
		!isValidEnvironmentTone(normalized.Tone) {
		return domain.EnvironmentInput{}, domain.ErrInvalidEnvironmentInput
	}

	return normalized, nil
}

func isValidEnvironmentCode(code string) bool {
	if code == "" || strings.TrimSpace(code) != code {
		return false
	}
	first := code[0]
	if !((first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')) {
		return false
	}
	for _, char := range code {
		switch {
		case char >= 'A' && char <= 'Z':
		case char >= 'a' && char <= 'z':
		case char >= '0' && char <= '9':
		case char == '_' || char == '-':
		default:
			return false
		}
	}
	return true
}

func isValidEnvironmentTone(tone string) bool {
	switch tone {
	case "amber", "blue", "emerald", "rose", "sky":
		return true
	default:
		return false
	}
}
