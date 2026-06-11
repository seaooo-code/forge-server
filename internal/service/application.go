package service

import (
	"context"
	"strconv"
	"strings"

	"forge-server/internal/domain"
)

type ApplicationRepository interface {
	CreateApplication(ctx context.Context, input domain.ApplicationInput) (domain.ApplicationDetail, error)
	DeleteApplication(ctx context.Context, slug string) (domain.ApplicationDetail, error)
	GetApplicationDetail(ctx context.Context, slug string) (domain.ApplicationDetail, error)
	ListApplicationOwners(ctx context.Context) ([]domain.ApplicationOptionItem, error)
	ListApplications(ctx context.Context) ([]domain.Application, error)
	UpdateApplication(ctx context.Context, slug string, input domain.ApplicationInput) (domain.ApplicationDetail, error)
}

type ApplicationService struct {
	repository ApplicationRepository
}

func NewApplicationService(repository ApplicationRepository) *ApplicationService {
	return &ApplicationService{repository: repository}
}

func (s *ApplicationService) ListApplications(ctx context.Context) ([]domain.Application, domain.ApplicationSummary, error) {
	applications, err := s.repository.ListApplications(ctx)
	if err != nil {
		return nil, domain.ApplicationSummary{}, err
	}

	return applications, summarizeApplications(applications), nil
}

func (s *ApplicationService) GetApplicationDetail(ctx context.Context, slug string) (domain.ApplicationDetail, error) {
	if !domain.IsValidApplicationSlug(slug) {
		return domain.ApplicationDetail{}, domain.ErrApplicationNotFound
	}

	detail, err := s.repository.GetApplicationDetail(ctx, slug)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}

	return withApplicationDerivedDetail(detail), nil
}

func (s *ApplicationService) CreateApplication(ctx context.Context, input domain.ApplicationInput) (domain.ApplicationDetail, error) {
	normalized, err := validateApplicationInput(input, true)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}

	detail, err := s.repository.CreateApplication(ctx, normalized)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}

	return withApplicationDerivedDetail(detail), nil
}

func (s *ApplicationService) UpdateApplication(ctx context.Context, slug string, input domain.ApplicationInput) (domain.ApplicationDetail, error) {
	if !domain.IsValidApplicationSlug(slug) {
		return domain.ApplicationDetail{}, domain.ErrApplicationNotFound
	}

	normalized, err := validateApplicationInput(input, false)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}

	detail, err := s.repository.UpdateApplication(ctx, slug, normalized)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}

	return withApplicationDerivedDetail(detail), nil
}

func (s *ApplicationService) DeleteApplication(ctx context.Context, slug string) (domain.ApplicationDetail, error) {
	if !domain.IsValidApplicationSlug(slug) {
		return domain.ApplicationDetail{}, domain.ErrApplicationNotFound
	}

	detail, err := s.repository.DeleteApplication(ctx, slug)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}

	return withApplicationDerivedDetail(detail), nil
}

func (s *ApplicationService) GetApplicationOptions(ctx context.Context) (domain.ApplicationOptions, error) {
	owners, err := s.repository.ListApplicationOwners(ctx)
	if err != nil {
		return domain.ApplicationOptions{}, err
	}

	return domain.ApplicationOptions{
		ApplicationStatuses: []domain.ApplicationOptionItem{
			{Label: "运行中", Value: string(domain.ApplicationStatusRunning)},
			{Label: "维护中", Value: string(domain.ApplicationStatusMaintenance)},
			{Label: "已下线", Value: string(domain.ApplicationStatusOffline)},
		},
		Environments: []domain.ApplicationOptionItem{
			{Label: "dev", Value: "dev"},
			{Label: "staging", Value: "staging"},
			{Label: "preview", Value: "preview"},
			{Label: "preProduct", Value: "preProduct"},
			{Label: "product", Value: "product"},
		},
		Owners: owners,
		Platforms: []domain.ApplicationOptionItem{
			{Label: "iOS", Value: "iOS"},
			{Label: "Android", Value: "Android"},
			{Label: "HarmonyOS", Value: "HarmonyOS"},
		},
	}, nil
}

func validateApplicationInput(input domain.ApplicationInput, requireSlug bool) (domain.ApplicationInput, error) {
	normalized := domain.ApplicationInput{
		Slug:               strings.TrimSpace(input.Slug),
		Name:               strings.TrimSpace(input.Name),
		Platform:           strings.TrimSpace(input.Platform),
		Owner:              strings.TrimSpace(input.Owner),
		OwnerEmail:         strings.TrimSpace(input.OwnerEmail),
		OwnerAvatarURL:     strings.TrimSpace(input.OwnerAvatarURL),
		OwnerDepartmentIDs: trimStringSlice(input.OwnerDepartmentIDs),
		Description:        strings.TrimSpace(input.Description),
		Status:             input.Status,
		Environments:       make([]domain.ApplicationEnvironmentInput, 0, len(input.Environments)),
		Members:            make([]domain.ApplicationMemberInput, 0, len(input.Members)),
	}

	if requireSlug && !domain.IsValidApplicationSlug(normalized.Slug) {
		return domain.ApplicationInput{}, domain.ErrInvalidApplicationInput
	}
	if !requireSlug && normalized.Slug != "" {
		return domain.ApplicationInput{}, domain.ErrInvalidApplicationInput
	}
	if normalized.Name == "" ||
		normalized.Platform == "" ||
		normalized.Owner == "" ||
		normalized.Description == "" {
		return domain.ApplicationInput{}, domain.ErrInvalidApplicationInput
	}
	if normalized.Status == "" {
		normalized.Status = domain.ApplicationStatusRunning
	}
	if !domain.IsValidApplicationStatus(normalized.Status) {
		return domain.ApplicationInput{}, domain.ErrInvalidApplicationInput
	}
	if len(input.Environments) == 0 {
		return domain.ApplicationInput{}, domain.ErrInvalidApplicationInput
	}

	environmentSeen := map[string]struct{}{}
	for _, environment := range input.Environments {
		code := strings.TrimSpace(environment.Environment)
		if !domain.IsValidEnvironmentCode(code) {
			return domain.ApplicationInput{}, domain.ErrInvalidApplicationInput
		}
		if _, ok := environmentSeen[code]; ok {
			return domain.ApplicationInput{}, domain.ErrInvalidApplicationInput
		}
		environmentSeen[code] = struct{}{}
		normalized.Environments = append(normalized.Environments, domain.ApplicationEnvironmentInput{
			Environment: code,
		})
	}

	for _, member := range input.Members {
		normalizedMember := domain.ApplicationMemberInput{
			Name: strings.TrimSpace(member.Name),
			Role: member.Role,
			Team: strings.TrimSpace(member.Team),
		}
		if normalizedMember.Name == "" || !domain.IsValidApplicationMemberRole(normalizedMember.Role) {
			return domain.ApplicationInput{}, domain.ErrInvalidApplicationInput
		}
		normalized.Members = append(normalized.Members, normalizedMember)
	}
	normalized.Members = ensureOwnerMember(normalized.Members, normalized.Owner)

	return normalized, nil
}

func ensureOwnerMember(
	members []domain.ApplicationMemberInput,
	owner string,
) []domain.ApplicationMemberInput {
	normalizedOwner := strings.TrimSpace(owner)
	if normalizedOwner == "" {
		return members
	}

	for _, member := range members {
		if member.Role == domain.ApplicationMemberRoleOwner &&
			strings.EqualFold(strings.TrimSpace(member.Name), normalizedOwner) {
			return members
		}
	}

	return append([]domain.ApplicationMemberInput{
		{
			Name: normalizedOwner,
			Role: domain.ApplicationMemberRoleOwner,
			Team: "应用负责人",
		},
	}, members...)
}

func trimStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func summarizeApplications(applications []domain.Application) domain.ApplicationSummary {
	summary := domain.ApplicationSummary{
		Total: len(applications),
	}

	for _, application := range applications {
		switch application.Status {
		case domain.ApplicationStatusRunning:
			summary.Running++
		case domain.ApplicationStatusMaintenance:
			summary.Maintenance++
		case domain.ApplicationStatusOffline:
			summary.Offline++
		}
	}

	return summary
}

func withApplicationDerivedDetail(detail domain.ApplicationDetail) domain.ApplicationDetail {
	detail.ID = detail.Application.ID
	detail.AppKey = "rn." + detail.Application.Slug
	detail.Slug = detail.Application.Slug
	detail.Name = detail.Application.Name
	detail.Owner = detail.Application.Owner
	detail.OwnerEmail = detail.Application.OwnerEmail
	detail.OwnerAvatarURL = detail.Application.OwnerAvatarURL
	detail.Application.EnvironmentCount = len(detail.Environments)
	detail.Metrics = []domain.ApplicationDetailMetric{
		{
			ID:     "platform",
			Label:  "支持平台",
			Value:  strconv.Itoa(platformCount(detail.Application.Platform)),
			Detail: detail.Application.Platform,
			Tone:   "blue",
		},
		{
			ID:     "modules",
			Label:  "模块数量",
			Value:  strconv.Itoa(detail.Application.ModuleCount),
			Detail: "已接入模块",
			Tone:   "emerald",
		},
		{
			ID:     "environments",
			Label:  "环境数量",
			Value:  strconv.Itoa(len(detail.Environments)),
			Detail: "已启用环境",
			Tone:   "amber",
		},
		{
			ID:     "status",
			Label:  "应用状态",
			Value:  detail.Application.Status.Label(),
			Detail: "当前运行状态",
			Tone:   applicationStatusTone(detail.Application.Status),
		},
	}
	if detail.Modules == nil {
		detail.Modules = []domain.ApplicationModule{}
	}
	if detail.Pipelines == nil {
		detail.Pipelines = []domain.ApplicationPipeline{}
	}

	return detail
}

func platformCount(platform string) int {
	parts := strings.Split(platform, "/")
	count := 0
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	if count == 0 {
		return 1
	}
	return count
}

func applicationStatusTone(status domain.ApplicationStatus) string {
	switch status {
	case domain.ApplicationStatusRunning:
		return "emerald"
	case domain.ApplicationStatusMaintenance:
		return "amber"
	case domain.ApplicationStatusOffline:
		return "slate"
	default:
		return "blue"
	}
}
