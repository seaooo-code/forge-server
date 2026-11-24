package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"forge-server/internal/domain"
)

type ReleaseChannelRepository interface {
	ListReleaseChannels(ctx context.Context, applicationSlug string) ([]domain.ReleaseChannel, error)
	CreateReleaseChannel(ctx context.Context, applicationSlug string, input domain.ReleaseChannelInput) (domain.ReleaseChannel, error)
	UpdateReleaseChannel(ctx context.Context, applicationSlug string, code string, input domain.ReleaseChannelInput) (domain.ReleaseChannel, error)
	DeleteReleaseChannel(ctx context.Context, applicationSlug string, code string) error
}

// ReleaseChannelService 管理应用的发布渠道，规则见 docs/channels.md。
type ReleaseChannelService struct {
	repository ReleaseChannelRepository
}

func NewReleaseChannelService(repository ReleaseChannelRepository) *ReleaseChannelService {
	return &ReleaseChannelService{repository: repository}
}

func (s *ReleaseChannelService) ListReleaseChannels(ctx context.Context, applicationSlug string) ([]domain.ReleaseChannel, error) {
	applicationSlug = strings.TrimSpace(applicationSlug)
	if !domain.IsValidApplicationSlug(applicationSlug) {
		return nil, domain.ErrInvalidReleaseChannelInput
	}
	return s.repository.ListReleaseChannels(ctx, applicationSlug)
}

func (s *ReleaseChannelService) CreateReleaseChannel(ctx context.Context, applicationSlug string, input domain.ReleaseChannelInput) (domain.ReleaseChannel, error) {
	applicationSlug = strings.TrimSpace(applicationSlug)
	normalized, err := normalizeReleaseChannelInput(input)
	if err != nil {
		return domain.ReleaseChannel{}, err
	}
	normalized.Code = strings.TrimSpace(input.Code)
	normalized.CreatedByUserID = strings.TrimSpace(input.CreatedByUserID)
	if !domain.IsValidApplicationSlug(applicationSlug) || !domain.IsValidReleaseChannelCode(normalized.Code) {
		return domain.ReleaseChannel{}, domain.ErrInvalidReleaseChannelInput
	}
	return s.repository.CreateReleaseChannel(ctx, applicationSlug, normalized)
}

// UpdateReleaseChannel 只能改名称和说明，code 创建后不能改：客户端打包时已经写死了它。
func (s *ReleaseChannelService) UpdateReleaseChannel(ctx context.Context, applicationSlug string, code string, input domain.ReleaseChannelInput) (domain.ReleaseChannel, error) {
	applicationSlug, code = strings.TrimSpace(applicationSlug), strings.TrimSpace(code)
	normalized, err := normalizeReleaseChannelInput(input)
	if err != nil {
		return domain.ReleaseChannel{}, err
	}
	if !domain.IsValidApplicationSlug(applicationSlug) || !domain.IsValidReleaseChannelCode(code) {
		return domain.ReleaseChannel{}, domain.ErrInvalidReleaseChannelInput
	}
	return s.repository.UpdateReleaseChannel(ctx, applicationSlug, code, normalized)
}

func (s *ReleaseChannelService) DeleteReleaseChannel(ctx context.Context, applicationSlug string, code string) error {
	applicationSlug, code = strings.TrimSpace(applicationSlug), strings.TrimSpace(code)
	if !domain.IsValidApplicationSlug(applicationSlug) || !domain.IsValidReleaseChannelCode(code) {
		return domain.ErrInvalidReleaseChannelInput
	}
	return s.repository.DeleteReleaseChannel(ctx, applicationSlug, code)
}

func normalizeReleaseChannelInput(input domain.ReleaseChannelInput) (domain.ReleaseChannelInput, error) {
	normalized := domain.ReleaseChannelInput{
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
	}
	if normalized.Name == "" ||
		utf8.RuneCountInString(normalized.Name) > domain.MaxReleaseChannelNameLength ||
		utf8.RuneCountInString(normalized.Description) > domain.MaxReleaseChannelDescriptionLength {
		return domain.ReleaseChannelInput{}, domain.ErrInvalidReleaseChannelInput
	}
	return normalized, nil
}
