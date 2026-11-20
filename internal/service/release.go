package service

import (
	"context"
	"strings"

	"forge-server/internal/domain"
)

type ReleaseRepository interface {
	CreateRelease(ctx context.Context, input domain.ReleaseCreateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error)
	ListReleases(ctx context.Context, filters domain.ReleaseRecordFilters) ([]domain.ReleaseRecord, domain.ReleaseRecordSummary, error)
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
	return s.repository.CreateRelease(ctx, normalized)
}

func (s *ReleaseService) ListReleases(ctx context.Context, filters domain.ReleaseRecordFilters) ([]domain.ReleaseRecord, domain.ReleaseRecordSummary, error) {
	normalized, err := normalizeReleaseRecordFilters(filters)
	if err != nil {
		return nil, domain.ReleaseRecordSummary{}, err
	}
	return s.repository.ListReleases(ctx, normalized)
}

func (s *ReleaseService) ResolveReleaseRoute(ctx context.Context, input domain.ReleaseResolveInput) (domain.ReleaseRoute, error) {
	normalized := domain.ReleaseResolveInput{
		ModuleID:       strings.TrimSpace(input.ModuleID),
		Environment:    strings.TrimSpace(input.Environment),
		RuntimeVersion: strings.TrimSpace(input.RuntimeVersion),
		UserID:         strings.TrimSpace(input.UserID),
	}
	if !domain.IsValidModuleID(normalized.ModuleID) ||
		normalized.Environment == "" ||
		normalized.RuntimeVersion != "" && !domain.IsValidRuntimeVersion(normalized.RuntimeVersion) {
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

func validateReleaseCreateInput(input domain.ReleaseCreateInput) (domain.ReleaseCreateInput, error) {
	releaseType := domain.ReleaseType(strings.TrimSpace(string(input.ReleaseType)))
	grayStrategy := domain.ReleaseGrayStrategy(strings.TrimSpace(string(input.GrayStrategy)))
	normalized := domain.ReleaseCreateInput{
		BuildRecordID:   strings.TrimSpace(input.BuildRecordID),
		ReleaseType:     releaseType,
		GrayStrategy:    grayStrategy,
		GrayPercentage:  input.GrayPercentage,
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
		if normalized.GrayStrategy != "" || normalized.GrayPercentage != 0 {
			return domain.ReleaseCreateInput{}, domain.ErrInvalidReleaseInput
		}
	case domain.ReleaseTypeGray:
		// 灰度只剩比例灰度；按 identifier 定向灰度已由提测单取代。
		if normalized.GrayStrategy != domain.ReleaseGrayStrategyPercentage ||
			normalized.GrayPercentage < 0 ||
			normalized.GrayPercentage > 99 {
			return domain.ReleaseCreateInput{}, domain.ErrInvalidReleaseInput
		}
	}
	return normalized, nil
}

func normalizeReleaseRecordFilters(filters domain.ReleaseRecordFilters) (domain.ReleaseRecordFilters, error) {
	normalized := domain.ReleaseRecordFilters{
		ApplicationID: strings.TrimSpace(filters.ApplicationID),
		Environment:   strings.TrimSpace(filters.Environment),
		ModuleID:      strings.TrimSpace(filters.ModuleID),
		Pagination:    filters.Pagination,
		ReleaseType:   domain.ReleaseType(strings.TrimSpace(string(filters.ReleaseType))),
		Status:        domain.ReleaseStatus(strings.TrimSpace(string(filters.Status))),
	}
	if !normalized.Pagination.IsValid() || normalized.ModuleID != "" && !domain.IsValidModuleID(normalized.ModuleID) {
		return domain.ReleaseRecordFilters{}, domain.ErrInvalidReleaseInput
	}
	if normalized.ReleaseType != "" && !domain.IsValidReleaseType(normalized.ReleaseType) {
		return domain.ReleaseRecordFilters{}, domain.ErrInvalidReleaseInput
	}
	if normalized.Status != "" && !domain.IsValidReleaseStatus(normalized.Status) {
		return domain.ReleaseRecordFilters{}, domain.ErrInvalidReleaseInput
	}
	return normalized, nil
}
