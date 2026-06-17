package service

import (
	"context"
	"strings"

	"forge-server/internal/domain"
)

type ReleaseRepository interface {
	CreateRelease(ctx context.Context, input domain.ReleaseCreateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error)
	ListReleases(ctx context.Context, moduleID string, environment string) ([]domain.ReleaseRecord, error)
	ResolveReleaseRoute(ctx context.Context, input domain.ReleaseResolveInput) (domain.ReleaseRoute, error)
}

type ReleaseService struct {
	repository ReleaseRepository
}

func NewReleaseService(repository ReleaseRepository) *ReleaseService {
	return &ReleaseService{repository: repository}
}

func (s *ReleaseService) CreateRelease(ctx context.Context, input domain.ReleaseCreateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	normalized, err := validateReleaseCreateInput(input)
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
	}
	return s.repository.CreateRelease(ctx, normalized)
}

func (s *ReleaseService) ListReleases(ctx context.Context, moduleID string, environment string) ([]domain.ReleaseRecord, error) {
	normalizedModuleID := strings.TrimSpace(moduleID)
	normalizedEnvironment := strings.TrimSpace(environment)
	if normalizedModuleID != "" && !domain.IsValidModuleID(normalizedModuleID) {
		return nil, domain.ErrInvalidReleaseInput
	}
	return s.repository.ListReleases(ctx, normalizedModuleID, normalizedEnvironment)
}

func (s *ReleaseService) ResolveReleaseRoute(ctx context.Context, input domain.ReleaseResolveInput) (domain.ReleaseRoute, error) {
	normalized := domain.ReleaseResolveInput{
		ModuleID:    strings.TrimSpace(input.ModuleID),
		Environment: strings.TrimSpace(input.Environment),
		Identifier:  strings.TrimSpace(input.Identifier),
	}
	if !domain.IsValidModuleID(normalized.ModuleID) || normalized.Environment == "" {
		return domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
	}
	return s.repository.ResolveReleaseRoute(ctx, normalized)
}

func validateReleaseCreateInput(input domain.ReleaseCreateInput) (domain.ReleaseCreateInput, error) {
	releaseType := domain.ReleaseType(strings.TrimSpace(string(input.ReleaseType)))
	normalized := domain.ReleaseCreateInput{
		BuildRecordID:   strings.TrimSpace(input.BuildRecordID),
		ReleaseType:     releaseType,
		Identifier:      strings.TrimSpace(input.Identifier),
		Description:     strings.TrimSpace(input.Description),
		CreatedByUserID: strings.TrimSpace(input.CreatedByUserID),
	}
	if normalized.BuildRecordID == "" ||
		normalized.CreatedByUserID == "" ||
		!domain.IsValidReleaseType(normalized.ReleaseType) {
		return domain.ReleaseCreateInput{}, domain.ErrInvalidReleaseInput
	}
	if normalized.ReleaseType == domain.ReleaseTypeFull && normalized.Identifier != "" {
		return domain.ReleaseCreateInput{}, domain.ErrInvalidReleaseInput
	}
	if normalized.ReleaseType == domain.ReleaseTypeTargeted && normalized.Identifier == "" {
		return domain.ReleaseCreateInput{}, domain.ErrInvalidReleaseInput
	}
	return normalized, nil
}
