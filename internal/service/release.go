package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"forge-server/internal/domain"

	"github.com/google/uuid"
)

type ReleaseRepository interface {
	CreateRelease(ctx context.Context, input domain.ReleaseCreateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error)
	ListReleases(ctx context.Context, filters domain.ReleaseRecordFilters) ([]domain.ReleaseRecord, domain.ReleaseRecordSummary, error)
	ResolveReleaseRoute(ctx context.Context, input domain.ReleaseResolveInput) (domain.ReleaseRoute, error)
	UpdateReleaseGrayPercentage(ctx context.Context, input domain.ReleaseGrayPercentageUpdateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error)
	GetRelease(ctx context.Context, id string) (domain.ReleaseRecord, error)
	DeactivateRelease(ctx context.Context, input domain.ReleaseDeactivateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error)
	PreviousFullRelease(ctx context.Context, releaseID string) (domain.ReleaseRecord, error)
	ListReleaseEvents(ctx context.Context, releaseID string) ([]domain.ReleaseEvent, error)
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
		Channel:        strings.TrimSpace(input.Channel),
	}
	if !domain.IsValidModuleID(normalized.ModuleID) ||
		normalized.Environment == "" ||
		normalized.Channel != "" && !domain.IsValidReleaseChannelCode(normalized.Channel) ||
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
		Channel:         strings.TrimSpace(input.Channel),
		ReleaseType:     releaseType,
		GrayStrategy:    grayStrategy,
		GrayPercentage:  input.GrayPercentage,
		Description:     strings.TrimSpace(input.Description),
		CreatedByUserID: strings.TrimSpace(input.CreatedByUserID),
	}
	if normalized.BuildRecordID == "" ||
		normalized.CreatedByUserID == "" ||
		normalized.Channel != "" && !domain.IsValidReleaseChannelCode(normalized.Channel) ||
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
	if filters.Channel != nil {
		channel := strings.TrimSpace(*filters.Channel)
		if channel != "" && !domain.IsValidReleaseChannelCode(channel) {
			return domain.ReleaseRecordFilters{}, domain.ErrInvalidReleaseInput
		}
		normalized.Channel = &channel
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

// DeactivateRelease 停用一条生效中的发布。原因可选，最多 MaxReleaseReasonLength 个字符。
func (s *ReleaseService) DeactivateRelease(ctx context.Context, input domain.ReleaseDeactivateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	normalized := domain.ReleaseDeactivateInput{
		ID:          strings.TrimSpace(input.ID),
		Reason:      strings.TrimSpace(input.Reason),
		ActorUserID: strings.TrimSpace(input.ActorUserID),
	}
	if uuid.Validate(normalized.ID) != nil || normalized.ActorUserID == "" || utf8.RuneCountInString(normalized.Reason) > domain.MaxReleaseReasonLength {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
	}
	return s.repository.DeactivateRelease(ctx, normalized)
}

// RollbackRelease 把生效中的全量发布回滚到同一模块、环境、渠道、运行时版本下上一次全量发布的构建。
// 回滚是一次新的全量发布，同样经过能力约束校验，并结束进行中的比例灰度。
func (s *ReleaseService) RollbackRelease(ctx context.Context, input domain.ReleaseRollbackInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	normalized := domain.ReleaseRollbackInput{
		ID:          strings.TrimSpace(input.ID),
		Description: strings.TrimSpace(input.Description),
		ActorUserID: strings.TrimSpace(input.ActorUserID),
	}
	if uuid.Validate(normalized.ID) != nil || normalized.ActorUserID == "" || utf8.RuneCountInString(normalized.Description) > domain.MaxReleaseReasonLength {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
	}
	current, err := s.repository.GetRelease(ctx, normalized.ID)
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
	}
	if current.Status != domain.ReleaseStatusActive {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrReleaseNotActive
	}
	if current.ReleaseType != domain.ReleaseTypeFull {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrReleaseRollbackUnavailable
	}
	previous, err := s.repository.PreviousFullRelease(ctx, normalized.ID)
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
	}
	return s.repository.CreateRelease(ctx, domain.ReleaseCreateInput{
		BuildRecordID:         previous.BuildRecordID,
		Channel:               current.Channel,
		ReleaseType:           domain.ReleaseTypeFull,
		Description:           normalized.Description,
		CreatedByUserID:       normalized.ActorUserID,
		RollbackFromReleaseID: normalized.ID,
	})
}

func (s *ReleaseService) ListReleaseEvents(ctx context.Context, releaseID string) ([]domain.ReleaseEvent, error) {
	releaseID = strings.TrimSpace(releaseID)
	if uuid.Validate(releaseID) != nil {
		return nil, domain.ErrInvalidReleaseInput
	}
	return s.repository.ListReleaseEvents(ctx, releaseID)
}
