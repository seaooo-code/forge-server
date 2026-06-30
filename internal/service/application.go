package service

import (
	"context"
	"strconv"
	"strings"

	"forge-server/internal/domain"
)

type ApplicationRepository interface {
	CheckApplicationDelete(ctx context.Context, slug string) (domain.ApplicationDeleteCheck, error)
	CreateApplication(ctx context.Context, input domain.ApplicationInput) (domain.ApplicationDetail, error)
	DeleteApplication(ctx context.Context, slug string) error
	GetApplicationDetail(ctx context.Context, slug string) (domain.ApplicationDetail, error)
	DeleteApplicationMember(ctx context.Context, applicationID string, userID string) error
	ListApplicationEnvironments(ctx context.Context) ([]domain.ApplicationOptionItem, error)
	ListApplicationMembers(ctx context.Context, applicationID string) ([]domain.ApplicationMember, error)
	ListApplicationOwners(ctx context.Context) ([]domain.ApplicationOptionItem, error)
	ListApplications(ctx context.Context, filters domain.ApplicationListFilters) ([]domain.Application, error)
	UpsertApplicationMember(ctx context.Context, applicationID string, input domain.ApplicationMemberInput) (domain.ApplicationMember, error)
	UpdateApplication(ctx context.Context, slug string, input domain.ApplicationInput) (domain.ApplicationDetail, error)
	UpdateApplicationStatus(ctx context.Context, slug string, status domain.ApplicationStatus) (domain.ApplicationDetail, error)
}

type ApplicationService struct {
	repository ApplicationRepository
}

func NewApplicationService(repository ApplicationRepository) *ApplicationService {
	return &ApplicationService{repository: repository}
}

func (s *ApplicationService) ListApplications(ctx context.Context, filters domain.ApplicationListFilters) ([]domain.Application, domain.ApplicationSummary, error) {
	normalizedFilters, err := normalizeApplicationListFilters(filters)
	if err != nil {
		return nil, domain.ApplicationSummary{}, err
	}

	applications, err := s.repository.ListApplications(ctx, normalizedFilters)
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
	if normalized.OwnerUserID != "" {
		member, err := s.repository.UpsertApplicationMember(ctx, detail.ID, domain.ApplicationMemberInput{
			Role:   domain.ApplicationMemberRoleOwner,
			UserID: normalized.OwnerUserID,
		})
		if err != nil {
			return domain.ApplicationDetail{}, err
		}
		detail.Members = []domain.ApplicationMember{member}
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

func (s *ApplicationService) UpdateApplicationStatus(ctx context.Context, slug string, status domain.ApplicationStatus) (domain.ApplicationDetail, error) {
	if !domain.IsValidApplicationSlug(slug) {
		return domain.ApplicationDetail{}, domain.ErrApplicationNotFound
	}
	if !domain.IsValidApplicationStatus(status) {
		return domain.ApplicationDetail{}, domain.ErrInvalidApplicationInput
	}

	detail, err := s.repository.UpdateApplicationStatus(ctx, slug, status)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}

	return withApplicationDerivedDetail(detail), nil
}

func (s *ApplicationService) CheckApplicationDelete(ctx context.Context, slug string) (domain.ApplicationDeleteCheck, error) {
	if !domain.IsValidApplicationSlug(slug) {
		return domain.ApplicationDeleteCheck{}, domain.ErrApplicationNotFound
	}

	check, err := s.repository.CheckApplicationDelete(ctx, slug)
	if err != nil {
		return domain.ApplicationDeleteCheck{}, err
	}
	return check, nil
}

func (s *ApplicationService) DeleteApplication(ctx context.Context, slug string) error {
	if !domain.IsValidApplicationSlug(slug) {
		return domain.ErrApplicationNotFound
	}

	check, err := s.repository.CheckApplicationDelete(ctx, slug)
	if err != nil {
		return err
	}
	if !check.Deletable {
		return domain.ErrApplicationInUse
	}

	return s.repository.DeleteApplication(ctx, slug)
}

func (s *ApplicationService) ListApplicationMembers(ctx context.Context, slug string) ([]domain.ApplicationMember, error) {
	detail, err := s.GetApplicationDetail(ctx, slug)
	if err != nil {
		return nil, err
	}
	return s.repository.ListApplicationMembers(ctx, detail.ID)
}

func (s *ApplicationService) UpsertApplicationMember(ctx context.Context, slug string, input domain.ApplicationMemberInput) (domain.ApplicationMember, error) {
	detail, err := s.GetApplicationDetail(ctx, slug)
	if err != nil {
		return domain.ApplicationMember{}, err
	}
	normalized, err := normalizeApplicationMemberInput(input)
	if err != nil {
		return domain.ApplicationMember{}, err
	}
	return s.repository.UpsertApplicationMember(ctx, detail.ID, normalized)
}

func (s *ApplicationService) DeleteApplicationMember(ctx context.Context, slug string, userID string) error {
	detail, err := s.GetApplicationDetail(ctx, slug)
	if err != nil {
		return err
	}
	normalizedUserID := strings.TrimSpace(userID)
	if normalizedUserID == "" {
		return domain.ErrInvalidApplicationInput
	}
	return s.repository.DeleteApplicationMember(ctx, detail.ID, normalizedUserID)
}

func (s *ApplicationService) GetApplicationOptions(ctx context.Context) (domain.ApplicationOptions, error) {
	owners, err := s.repository.ListApplicationOwners(ctx)
	if err != nil {
		return domain.ApplicationOptions{}, err
	}
	environments, err := s.repository.ListApplicationEnvironments(ctx)
	if err != nil {
		return domain.ApplicationOptions{}, err
	}

	return domain.ApplicationOptions{
		ApplicationStatuses: []domain.ApplicationOptionItem{
			{Label: "运行中", Value: string(domain.ApplicationStatusRunning)},
			{Label: "维护中", Value: string(domain.ApplicationStatusMaintenance)},
			{Label: "已下线", Value: string(domain.ApplicationStatusOffline)},
		},
		Environments: environments,
		Owners:       owners,
		Platforms: []domain.ApplicationOptionItem{
			{Label: "iOS", Value: "iOS"},
			{Label: "Android", Value: "Android"},
			{Label: "HarmonyOS", Value: "HarmonyOS"},
		},
	}, nil
}

func validateApplicationInput(input domain.ApplicationInput, requireSlug bool) (domain.ApplicationInput, error) {
	normalized := domain.ApplicationInput{
		Slug:         strings.TrimSpace(input.Slug),
		Name:         strings.TrimSpace(input.Name),
		Platform:     strings.TrimSpace(input.Platform),
		Description:  strings.TrimSpace(input.Description),
		Status:       input.Status,
		Environments: make([]domain.ApplicationEnvironmentInput, 0, len(input.Environments)),
		Members:      make([]domain.ApplicationMemberInput, 0, len(input.Members)),
	}
	owner := normalizeUserInput(domain.UserInput{
		ID:            input.OwnerUserID,
		Name:          input.Owner,
		Email:         input.OwnerEmail,
		AvatarURL:     input.OwnerAvatarURL,
		DepartmentIDs: input.OwnerDepartmentIDs,
		FeishuUnionID: input.OwnerFeishuUnionID,
		FeishuOpenID:  input.OwnerFeishuOpenID,
		FeishuUserID:  input.OwnerFeishuUserID,
	})
	normalized.OwnerUserID = owner.ID
	normalized.Owner = owner.Name
	normalized.OwnerEmail = owner.Email
	normalized.OwnerAvatarURL = owner.AvatarURL
	normalized.OwnerDepartmentIDs = owner.DepartmentIDs
	normalized.OwnerFeishuUnionID = owner.FeishuUnionID
	normalized.OwnerFeishuOpenID = owner.FeishuOpenID
	normalized.OwnerFeishuUserID = owner.FeishuUserID

	if requireSlug && !domain.IsValidApplicationSlug(normalized.Slug) {
		return domain.ApplicationInput{}, domain.ErrInvalidApplicationInput
	}
	if !requireSlug && normalized.Slug != "" {
		return domain.ApplicationInput{}, domain.ErrInvalidApplicationInput
	}
	if normalized.Name == "" ||
		normalized.Platform == "" ||
		normalized.OwnerUserID == "" ||
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
		if code == "" {
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
		normalizedMember, err := normalizeApplicationMemberInput(member)
		if err != nil {
			return domain.ApplicationInput{}, err
		}
		normalized.Members = append(normalized.Members, normalizedMember)
	}

	return normalized, nil
}

func normalizeApplicationMemberInput(input domain.ApplicationMemberInput) (domain.ApplicationMemberInput, error) {
	normalized := domain.ApplicationMemberInput{
		Role:   input.Role,
		UserID: strings.TrimSpace(input.UserID),
	}
	if normalized.UserID == "" || !domain.IsValidApplicationMemberRole(normalized.Role) {
		return domain.ApplicationMemberInput{}, domain.ErrInvalidApplicationInput
	}
	return normalized, nil
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

func normalizeApplicationListFilters(filters domain.ApplicationListFilters) (domain.ApplicationListFilters, error) {
	normalized := domain.ApplicationListFilters{
		Platform: strings.TrimSpace(filters.Platform),
		Status:   filters.Status,
		Owner:    strings.TrimSpace(filters.Owner),
		Keyword:  strings.TrimSpace(filters.Keyword),
	}
	if normalized.Status != "" && !domain.IsValidApplicationStatus(normalized.Status) {
		return domain.ApplicationListFilters{}, domain.ErrInvalidApplicationInput
	}
	return normalized, nil
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
	detail.OwnerUserID = detail.Application.OwnerUserID
	detail.Owner = detail.Application.Owner
	detail.OwnerEmail = detail.Application.OwnerEmail
	detail.OwnerAvatarURL = detail.Application.OwnerAvatarURL
	detail.OwnerDepartmentIDs = detail.Application.OwnerDepartmentIDs
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
	detail.ClientIntegration = clientIntegrationInfo(detail.Application.Slug)
	if detail.ClientIntegration.Endpoint.RequestFields == nil {
		detail.ClientIntegration.Endpoint.RequestFields = []domain.ClientIntegrationField{}
	}
	if detail.ClientIntegration.Endpoint.ResponseFields == nil {
		detail.ClientIntegration.Endpoint.ResponseFields = []domain.ClientIntegrationField{}
	}

	return detail
}

func clientIntegrationInfo(applicationSlug string) domain.ClientIntegrationInfo {
	path := "/api/v1/artifacts/applications/" + applicationSlug + "/environments/{environment}/manifest"
	exampleURL := "/api/v1/artifacts/applications/" + applicationSlug + "/environments/product/manifest?identifier=user-123"

	return domain.ClientIntegrationInfo{
		Endpoint: domain.ClientIntegrationEndpoint{
			Path:        path,
			Method:      "GET",
			Description: "客户端获取指定应用环境下模块 manifest 的接口说明",
			ExampleURL:  exampleURL,
			RequestFields: []domain.ClientIntegrationField{
				{Name: "environment", Description: "客户端所属环境编码，来自路径参数", Required: true, Type: "string", Example: "product"},
				{Name: "identifier", Description: "定向灰度标识；传入后优先命中 identifier 路由", Required: false, Type: "string", Example: "user-123"},
				{Name: "userId", Description: "用户标识；用于百分比灰度路由计算", Required: false, Type: "string", Example: "user-123"},
			},
			ResponseFields: []domain.ClientIntegrationField{
				{Name: "application", Description: "应用信息，包含 slug 和名称", Required: true, Type: "object", Example: `{"slug":"` + applicationSlug + `","name":"小米汽车"}`},
				{Name: "environment", Description: "环境信息，包含环境编码", Required: true, Type: "object", Example: `{"code":"product"}`},
				{Name: "identifier", Description: "请求使用的定向灰度标识，未传时为空", Required: false, Type: "string", Example: "user-123"},
				{Name: "modules", Description: "客户端需要加载的模块 manifest 列表", Required: true, Type: "array", Example: `[{"id":"profile","files":[]}]`},
				{Name: "missingModules", Description: "未能解析到发布路由或 manifest 的模块列表", Required: true, Type: "array", Example: "[]"},
			},
		},
	}
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
