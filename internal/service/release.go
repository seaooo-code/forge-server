package service

import (
	"context"
	"strings"
	"time"

	"forge-server/internal/domain"
)

type ReleaseRepository interface {
	CreateRelease(ctx context.Context, input domain.ReleaseCreateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error)
	ExpireTargetedGrayReleases(ctx context.Context) (int64, error)
	InactivateTargetedGrayRelease(ctx context.Context, input domain.ReleaseInactivateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error)
	ListReleases(ctx context.Context, moduleID string, environment string) ([]domain.ReleaseRecord, error)
	ResolveReleaseRoute(ctx context.Context, input domain.ReleaseResolveInput) (domain.ReleaseRoute, error)
	UpdateReleaseGrayPercentage(ctx context.Context, input domain.ReleaseGrayPercentageUpdateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error)
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
	if normalized.ReleaseType == domain.ReleaseTypeGray && normalized.GrayStrategy == domain.ReleaseGrayStrategyTargeted {
		if _, err := s.repository.ExpireTargetedGrayReleases(ctx); err != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
		}
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
		UserID:      strings.TrimSpace(input.UserID),
	}
	if !domain.IsValidModuleID(normalized.ModuleID) || normalized.Environment == "" {
		return domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
	}
	return s.repository.ResolveReleaseRoute(ctx, normalized)
}

func (s *ReleaseService) UpdateReleaseGrayPercentage(ctx context.Context, input domain.ReleaseGrayPercentageUpdateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	normalized := domain.ReleaseGrayPercentageUpdateInput{
		ID:              strings.TrimSpace(input.ID),
		GrayPercentage:  input.GrayPercentage,
		UpdatedByUserID: strings.TrimSpace(input.UpdatedByUserID),
	}
	if normalized.ID == "" || normalized.UpdatedByUserID == "" || normalized.GrayPercentage < 0 || normalized.GrayPercentage > 100 {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
	}
	return s.repository.UpdateReleaseGrayPercentage(ctx, normalized)
}

func (s *ReleaseService) InactivateTargetedGrayRelease(ctx context.Context, input domain.ReleaseInactivateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	normalized := domain.ReleaseInactivateInput{
		ID:              strings.TrimSpace(input.ID),
		UpdatedByUserID: strings.TrimSpace(input.UpdatedByUserID),
	}
	if normalized.ID == "" || normalized.UpdatedByUserID == "" {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
	}
	return s.repository.InactivateTargetedGrayRelease(ctx, normalized)
}

func (s *ReleaseService) ExpireTargetedGrayReleases(ctx context.Context) (int64, error) {
	return s.repository.ExpireTargetedGrayReleases(ctx)
}

func validateReleaseCreateInput(input domain.ReleaseCreateInput) (domain.ReleaseCreateInput, error) {
	releaseType := domain.ReleaseType(strings.TrimSpace(string(input.ReleaseType)))
	grayStrategy := domain.ReleaseGrayStrategy(strings.TrimSpace(string(input.GrayStrategy)))
	normalized := domain.ReleaseCreateInput{
		BuildRecordID:   strings.TrimSpace(input.BuildRecordID),
		ReleaseType:     releaseType,
		GrayStrategy:    grayStrategy,
		GrayPercentage:  input.GrayPercentage,
		ExpiresAt:       input.ExpiresAt,
		Identifier:      strings.TrimSpace(input.Identifier),
		Description:     strings.TrimSpace(input.Description),
		CreatedByUserID: strings.TrimSpace(input.CreatedByUserID),
	}
	if normalized.BuildRecordID == "" ||
		normalized.CreatedByUserID == "" ||
		!domain.IsValidReleaseType(normalized.ReleaseType) {
		return domain.ReleaseCreateInput{}, domain.ErrInvalidReleaseInput
	}
	switch normalized.ReleaseType {
	case domain.ReleaseTypeFull:
		if normalized.GrayStrategy != "" || normalized.Identifier != "" || normalized.GrayPercentage != 0 || normalized.ExpiresAt != nil {
			return domain.ReleaseCreateInput{}, domain.ErrInvalidReleaseInput
		}
	case domain.ReleaseTypeGray:
		if !domain.IsValidReleaseGrayStrategy(normalized.GrayStrategy) {
			return domain.ReleaseCreateInput{}, domain.ErrInvalidReleaseInput
		}
		switch normalized.GrayStrategy {
		case domain.ReleaseGrayStrategyTargeted:
			if normalized.Identifier == "" ||
				normalized.GrayPercentage != 0 ||
				normalized.ExpiresAt == nil ||
				!normalized.ExpiresAt.After(time.Now()) {
				return domain.ReleaseCreateInput{}, domain.ErrInvalidReleaseInput
			}
		case domain.ReleaseGrayStrategyPercentage:
			if normalized.Identifier != "" ||
				normalized.ExpiresAt != nil ||
				normalized.GrayPercentage < 0 ||
				normalized.GrayPercentage > 99 {
				return domain.ReleaseCreateInput{}, domain.ErrInvalidReleaseInput
			}
		}
	}
	return normalized, nil
}
