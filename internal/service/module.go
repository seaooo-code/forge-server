package service

import (
	"context"
	"errors"
	"path"
	"strings"

	"forge-server/internal/domain"
)

const (
	maxModuleAutoBuildBranches  = 20
	maxModuleAutoPreviewTTLHour = 24 * 365
	maxModuleBuildPathLength    = 255
)

type ModuleRepository interface {
	CreateModule(ctx context.Context, input domain.ModuleInput) (domain.Module, error)
	DeleteModule(ctx context.Context, id string) error
	GetModule(ctx context.Context, id string) (domain.Module, error)
	ListModules(ctx context.Context, filters domain.ModuleListFilters) ([]domain.Module, domain.ModuleSummary, error)
	UpdateModule(ctx context.Context, id string, input domain.ModuleInput) (domain.Module, error)
}

// ModuleCodeSourceStore 用于在绑定仓库时校验连接，并以仓库缓存（code_source_repositories）为准填充仓库信息，
// 避免客户端提交任意 clone 地址导致 worker 把仓库凭证发往其他主机。
type ModuleCodeSourceStore interface {
	GetCodeSourceConnection(ctx context.Context, id string) (domain.CodeSourceConnection, error)
	GetCodeSourceRepository(ctx context.Context, connectionID string, externalRepositoryID string) (domain.CodeSourceRepository, error)
}

// ModuleBuildAdapterReader 用于在模块选用构建适配器时校验适配器状态和参数定义。
type ModuleBuildAdapterReader interface {
	GetBuildAdapter(ctx context.Context, id string) (domain.BuildAdapter, error)
}

type ModuleService struct {
	buildAdapters         ModuleBuildAdapterReader
	codeSources           ModuleCodeSourceStore
	defaultDockerfilePath string
	repository            ModuleRepository
}

// NewModuleService 的 defaultDockerfilePath 来自 FORGE_WORKER_DOCKERFILE，只作为新建模块时 dockerfile_path 的默认值。
func NewModuleService(repository ModuleRepository, codeSources ModuleCodeSourceStore, defaultDockerfilePath string) *ModuleService {
	defaultDockerfilePath = strings.TrimSpace(defaultDockerfilePath)
	if defaultDockerfilePath == "" {
		defaultDockerfilePath = domain.DefaultModuleDockerfilePath
	}
	return &ModuleService{
		codeSources:           codeSources,
		defaultDockerfilePath: defaultDockerfilePath,
		repository:            repository,
	}
}

// WithBuildAdapters 注入构建适配器读取；未注入时模块不能选择 adapter 构建方式。
func (s *ModuleService) WithBuildAdapters(reader ModuleBuildAdapterReader) *ModuleService {
	s.buildAdapters = reader
	return s
}

func (s *ModuleService) ListModules(ctx context.Context, filters domain.ModuleListFilters) ([]domain.Module, domain.ModuleSummary, error) {
	normalized, err := normalizeModuleListFilters(filters)
	if err != nil {
		return nil, domain.ModuleSummary{}, err
	}

	return s.repository.ListModules(ctx, normalized)
}

func (s *ModuleService) GetModule(ctx context.Context, id string) (domain.Module, error) {
	normalizedID := strings.TrimSpace(id)
	if !domain.IsValidModuleID(normalizedID) {
		return domain.Module{}, domain.ErrModuleNotFound
	}
	return s.repository.GetModule(ctx, normalizedID)
}

func (s *ModuleService) CreateModule(ctx context.Context, input domain.ModuleInput) (domain.Module, error) {
	normalized, err := s.normalizeModuleInput(ctx, input, true)
	if err != nil {
		return domain.Module{}, err
	}
	if err := s.validateModuleBuildAdapter(ctx, normalized.BuildConfig, ""); err != nil {
		return domain.Module{}, err
	}
	return s.repository.CreateModule(ctx, normalized)
}

func (s *ModuleService) UpdateModule(ctx context.Context, id string, input domain.ModuleInput) (domain.Module, error) {
	normalizedID := strings.TrimSpace(id)
	if !domain.IsValidModuleID(normalizedID) {
		return domain.Module{}, domain.ErrModuleNotFound
	}

	normalized, err := s.normalizeModuleInput(ctx, input, false)
	if err != nil {
		return domain.Module{}, err
	}
	previousAdapterID := ""
	if normalized.BuildConfig.BuildMode == domain.BuildModeAdapter {
		previous, err := s.repository.GetModule(ctx, normalizedID)
		if err != nil {
			return domain.Module{}, err
		}
		previousAdapterID = previous.BuildConfig.BuildAdapterID
	}
	if err := s.validateModuleBuildAdapter(ctx, normalized.BuildConfig, previousAdapterID); err != nil {
		return domain.Module{}, err
	}
	return s.repository.UpdateModule(ctx, normalizedID, normalized)
}

// validateModuleBuildAdapter 校验 adapter 构建方式：适配器必须存在；新选择的适配器必须处于启用状态
// （模块继续沿用已停用的适配器时允许保存其他字段）；参数按适配器的参数定义校验。
func (s *ModuleService) validateModuleBuildAdapter(ctx context.Context, config domain.ModuleBuildConfig, previousAdapterID string) error {
	if config.BuildMode != domain.BuildModeAdapter {
		return nil
	}
	if s.buildAdapters == nil {
		return domain.ErrInvalidModuleInput
	}
	adapter, err := s.buildAdapters.GetBuildAdapter(ctx, config.BuildAdapterID)
	if err != nil {
		if errors.Is(err, domain.ErrBuildAdapterNotFound) {
			return domain.ErrInvalidModuleInput
		}
		return err
	}
	if adapter.Status != domain.BuildAdapterStatusActive && adapter.ID != previousAdapterID {
		return domain.ErrBuildAdapterDisabled
	}
	_, err = ResolveBuildAdapterParams(adapter.Parameters, config.BuildAdapterParams, false)
	return err
}

func (s *ModuleService) DeleteModule(ctx context.Context, id string) error {
	normalizedID := strings.TrimSpace(id)
	if !domain.IsValidModuleID(normalizedID) {
		return domain.ErrModuleNotFound
	}
	return s.repository.DeleteModule(ctx, normalizedID)
}

func (s *ModuleService) normalizeModuleInput(ctx context.Context, input domain.ModuleInput, requireID bool) (domain.ModuleInput, error) {
	normalized, err := validateModuleInput(input, requireID, s.defaultDockerfilePath)
	if err != nil {
		return domain.ModuleInput{}, err
	}
	if s.codeSources == nil {
		return domain.ModuleInput{}, domain.ErrCodeSourceAuthNotConfigured
	}

	connection, err := s.codeSources.GetCodeSourceConnection(ctx, normalized.Repository.ConnectionID)
	if err != nil {
		if errors.Is(err, domain.ErrCodeSourceConnectionNotFound) {
			return domain.ModuleInput{}, domain.ErrInvalidModuleInput
		}
		return domain.ModuleInput{}, err
	}
	if connection.Status != domain.CodeSourceConnectionStatusActive {
		return domain.ModuleInput{}, domain.ErrCodeSourceConnectionInactive
	}

	repository, err := s.codeSources.GetCodeSourceRepository(ctx, connection.ID, normalized.Repository.ExternalRepositoryID)
	if err != nil {
		if errors.Is(err, domain.ErrCodeSourceRepositoryNotFound) {
			return domain.ModuleInput{}, domain.ErrInvalidModuleInput
		}
		return domain.ModuleInput{}, err
	}
	normalized.Repository = domain.ModuleRepository{
		ConnectionID:         connection.ID,
		ExternalRepositoryID: repository.ExternalRepositoryID,
		FullName:             repository.FullName,
		WebURL:               repository.WebURL,
		CloneURL:             repository.CloneURL,
		DefaultBranch:        repository.DefaultBranch,
	}
	return normalized, nil
}

func validateModuleInput(input domain.ModuleInput, requireID bool, defaultDockerfilePath string) (domain.ModuleInput, error) {
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

	normalized := domain.ModuleInput{
		ApplicationID:      strings.TrimSpace(input.ApplicationID),
		CommonModuleID:     strings.TrimSpace(input.CommonModuleID),
		ID:                 strings.TrimSpace(input.ID),
		Kind:               input.Kind,
		Name:               strings.TrimSpace(input.Name),
		Owner:              owner.Name,
		OwnerAvatarURL:     owner.AvatarURL,
		OwnerDepartmentIDs: owner.DepartmentIDs,
		OwnerEmail:         owner.Email,
		OwnerFeishuOpenID:  owner.FeishuOpenID,
		OwnerFeishuUnionID: owner.FeishuUnionID,
		OwnerFeishuUserID:  owner.FeishuUserID,
		OwnerUserID:        owner.ID,
		Repository: domain.ModuleRepository{
			ConnectionID:         strings.TrimSpace(input.Repository.ConnectionID),
			ExternalRepositoryID: strings.TrimSpace(input.Repository.ExternalRepositoryID),
		},
	}
	buildConfig, ok := normalizeModuleBuildConfig(input.BuildConfig, defaultDockerfilePath)
	if !ok {
		return domain.ModuleInput{}, domain.ErrInvalidModuleInput
	}
	normalized.BuildConfig = buildConfig

	if requireID && !domain.IsValidModuleID(normalized.ID) {
		return domain.ModuleInput{}, domain.ErrInvalidModuleInput
	}
	if !requireID && normalized.ID != "" {
		return domain.ModuleInput{}, domain.ErrInvalidModuleInput
	}
	if normalized.ApplicationID == "" ||
		normalized.Name == "" ||
		normalized.OwnerUserID == "" ||
		normalized.Owner == "" ||
		normalized.Repository.ConnectionID == "" ||
		normalized.Repository.ExternalRepositoryID == "" {
		return domain.ModuleInput{}, domain.ErrInvalidModuleInput
	}
	if !domain.IsValidModuleKind(normalized.Kind) {
		return domain.ModuleInput{}, domain.ErrInvalidModuleInput
	}
	if normalized.Kind == domain.ModuleKindCommon && normalized.CommonModuleID != "" {
		return domain.ModuleInput{}, domain.ErrInvalidModuleInput
	}
	if normalized.Kind == domain.ModuleKindFeature && normalized.CommonModuleID == "" {
		return domain.ModuleInput{}, domain.ErrModuleCommonMissing
	}
	return normalized, nil
}

func normalizeModuleBuildConfig(config domain.ModuleBuildConfig, defaultDockerfilePath string) (domain.ModuleBuildConfig, bool) {
	normalized := domain.ModuleBuildConfig{
		BuildMode:            domain.BuildMode(strings.TrimSpace(string(config.BuildMode))),
		BuildAdapterID:       strings.TrimSpace(config.BuildAdapterID),
		BuildAdapterParams:   NormalizeBuildAdapterParamValues(config.BuildAdapterParams),
		DockerfilePath:       strings.TrimSpace(config.DockerfilePath),
		BuildContext:         strings.TrimSpace(config.BuildContext),
		AutoBuildBranches:    []string{},
		AutoBuildEnvironment: strings.TrimSpace(config.AutoBuildEnvironment),
		AutoPreviewRelease:   config.AutoPreviewRelease,
		AutoPreviewTTLHours:  config.AutoPreviewTTLHours,
	}
	if normalized.BuildMode == "" {
		normalized.BuildMode = domain.BuildModeDockerfile
	}
	// dockerfile 模式不能带适配器；adapter 模式必须指定适配器。参数是否合法在 validateModuleBuildAdapter 中按参数定义校验。
	switch normalized.BuildMode {
	case domain.BuildModeDockerfile:
		if normalized.BuildAdapterID != "" || len(normalized.BuildAdapterParams) > 0 {
			return domain.ModuleBuildConfig{}, false
		}
	case domain.BuildModeAdapter:
		if normalized.BuildAdapterID == "" {
			return domain.ModuleBuildConfig{}, false
		}
	default:
		return domain.ModuleBuildConfig{}, false
	}
	if normalized.DockerfilePath == "" {
		normalized.DockerfilePath = defaultDockerfilePath
	}
	if normalized.BuildContext == "" {
		normalized.BuildContext = domain.DefaultModuleBuildContext
	}
	if normalized.AutoPreviewTTLHours == 0 {
		normalized.AutoPreviewTTLHours = domain.DefaultModuleAutoPreviewTTLHours
	}

	// 构建路径必须是仓库内的相对路径。
	for _, value := range []string{normalized.DockerfilePath, normalized.BuildContext} {
		cleaned := path.Clean(value)
		if len(value) > maxModuleBuildPathLength || strings.Contains(value, "\\") || path.IsAbs(value) ||
			cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return domain.ModuleBuildConfig{}, false
		}
	}
	if path.Clean(normalized.DockerfilePath) == "." {
		return domain.ModuleBuildConfig{}, false
	}

	seen := map[string]struct{}{}
	for _, branch := range config.AutoBuildBranches {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}
		if _, err := path.Match(branch, ""); err != nil {
			return domain.ModuleBuildConfig{}, false
		}
		if _, ok := seen[branch]; ok {
			continue
		}
		seen[branch] = struct{}{}
		normalized.AutoBuildBranches = append(normalized.AutoBuildBranches, branch)
	}
	if len(normalized.AutoBuildBranches) > maxModuleAutoBuildBranches ||
		(len(normalized.AutoBuildBranches) > 0 && normalized.AutoBuildEnvironment == "") ||
		normalized.AutoPreviewTTLHours < 1 || normalized.AutoPreviewTTLHours > maxModuleAutoPreviewTTLHour {
		return domain.ModuleBuildConfig{}, false
	}
	return normalized, true
}

func normalizeModuleListFilters(filters domain.ModuleListFilters) (domain.ModuleListFilters, error) {
	normalized := domain.ModuleListFilters{
		ApplicationID: strings.TrimSpace(filters.ApplicationID),
		Kind:          filters.Kind,
		Keyword:       strings.TrimSpace(filters.Keyword),
		Owner:         strings.TrimSpace(filters.Owner),
		Pagination:    filters.Pagination,
	}
	if !normalized.Pagination.IsValid() || normalized.Kind != "" && !domain.IsValidModuleKind(normalized.Kind) {
		return domain.ModuleListFilters{}, domain.ErrInvalidModuleInput
	}
	return normalized, nil
}
