package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"forge-server/internal/domain"
)

type BuildAdapterRepository interface {
	CreateBuildAdapter(ctx context.Context, input domain.BuildAdapterInput) (domain.BuildAdapter, error)
	GetBuildAdapter(ctx context.Context, id string) (domain.BuildAdapter, error)
	ListBuildAdapters(ctx context.Context, filters domain.BuildAdapterFilters) ([]domain.BuildAdapter, error)
	SetBuildAdapterStatus(ctx context.Context, id string, status domain.BuildAdapterStatus, userID string) (domain.BuildAdapter, error)
	UpdateBuildAdapter(ctx context.Context, id string, expectedVersion int, input domain.BuildAdapterInput) (domain.BuildAdapter, error)
}

// BuildAdapterModuleReader 用于渲染预览时读取指定模块的名称和已保存参数。
type BuildAdapterModuleReader interface {
	GetModule(ctx context.Context, id string) (domain.Module, error)
}

// BuildAdapterUsageRecorder 把 worker 实际使用的适配器版本和渲染结果 sha256 写回构建记录。
type BuildAdapterUsageRecorder interface {
	RecordBuildAdapterUsage(ctx context.Context, buildID string, usage domain.BuildAdapterUsage) error
}

type BuildAdapterServiceDependencies struct {
	Modules    BuildAdapterModuleReader
	Repository BuildAdapterRepository
	Usage      BuildAdapterUsageRecorder
}

type BuildAdapterService struct {
	modules    BuildAdapterModuleReader
	now        func() time.Time
	repository BuildAdapterRepository
	usage      BuildAdapterUsageRecorder
}

func NewBuildAdapterService(deps BuildAdapterServiceDependencies) *BuildAdapterService {
	return &BuildAdapterService{
		modules:    deps.Modules,
		now:        time.Now,
		repository: deps.Repository,
		usage:      deps.Usage,
	}
}

func (s *BuildAdapterService) ListBuildAdapters(ctx context.Context, filters domain.BuildAdapterFilters) ([]domain.BuildAdapter, error) {
	filters.Status = domain.BuildAdapterStatus(strings.TrimSpace(string(filters.Status)))
	if filters.Status != "" && !domain.IsValidBuildAdapterStatus(filters.Status) {
		return nil, domain.ErrInvalidBuildAdapterInput
	}
	return s.repository.ListBuildAdapters(ctx, filters)
}

func (s *BuildAdapterService) GetBuildAdapter(ctx context.Context, id string) (domain.BuildAdapter, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.BuildAdapter{}, domain.ErrBuildAdapterNotFound
	}
	return s.repository.GetBuildAdapter(ctx, id)
}

func (s *BuildAdapterService) CreateBuildAdapter(ctx context.Context, input domain.BuildAdapterInput) (domain.BuildAdapter, error) {
	normalized, err := ValidateBuildAdapterDefinition(input)
	if err != nil {
		return domain.BuildAdapter{}, err
	}
	if normalized.UserID == "" {
		return domain.BuildAdapter{}, domain.ErrInvalidBuildAdapterInput
	}
	return s.repository.CreateBuildAdapter(ctx, normalized)
}

// UpdateBuildAdapter 合并 PATCH 字段后整体校验，并以读取时的版本做乐观锁，成功后版本号 +1。
func (s *BuildAdapterService) UpdateBuildAdapter(ctx context.Context, id string, patch domain.BuildAdapterPatch) (domain.BuildAdapter, error) {
	current, err := s.GetBuildAdapter(ctx, id)
	if err != nil {
		return domain.BuildAdapter{}, err
	}
	if patch.ExpectedVersion != 0 && patch.ExpectedVersion != current.Version {
		return domain.BuildAdapter{}, domain.ErrBuildAdapterVersionConflict
	}
	merged := domain.BuildAdapterInput{
		Name:               current.Name,
		Description:        current.Description,
		DockerfileTemplate: current.DockerfileTemplate,
		Files:              current.Files,
		Parameters:         current.Parameters,
		UserID:             patch.UserID,
	}
	if patch.Name != nil {
		merged.Name = *patch.Name
	}
	if patch.Description != nil {
		merged.Description = *patch.Description
	}
	if patch.DockerfileTemplate != nil {
		merged.DockerfileTemplate = *patch.DockerfileTemplate
	}
	if patch.Files != nil {
		merged.Files = *patch.Files
	}
	if patch.Parameters != nil {
		merged.Parameters = *patch.Parameters
	}
	normalized, err := ValidateBuildAdapterDefinition(merged)
	if err != nil {
		return domain.BuildAdapter{}, err
	}
	if normalized.UserID == "" {
		return domain.BuildAdapter{}, domain.ErrInvalidBuildAdapterInput
	}
	return s.repository.UpdateBuildAdapter(ctx, current.ID, current.Version, normalized)
}

func (s *BuildAdapterService) DisableBuildAdapter(ctx context.Context, id string, userID string) (domain.BuildAdapter, error) {
	return s.setStatus(ctx, id, domain.BuildAdapterStatusDisabled, userID)
}

func (s *BuildAdapterService) EnableBuildAdapter(ctx context.Context, id string, userID string) (domain.BuildAdapter, error) {
	return s.setStatus(ctx, id, domain.BuildAdapterStatusActive, userID)
}

func (s *BuildAdapterService) setStatus(ctx context.Context, id string, status domain.BuildAdapterStatus, userID string) (domain.BuildAdapter, error) {
	id = strings.TrimSpace(id)
	userID = strings.TrimSpace(userID)
	if id == "" {
		return domain.BuildAdapter{}, domain.ErrBuildAdapterNotFound
	}
	if userID == "" {
		return domain.BuildAdapter{}, domain.ErrInvalidBuildAdapterInput
	}
	return s.repository.SetBuildAdapterStatus(ctx, id, status, userID)
}

// PreviewBuildAdapter 用示例上下文或指定模块渲染一次，不执行构建。停用的适配器也可以预览，便于修复后再启用。
// 参数或模板错误分别返回 domain.ErrInvalidBuildAdapterInput 与 *domain.BuildAdapterRenderError。
func (s *BuildAdapterService) PreviewBuildAdapter(ctx context.Context, id string, input domain.BuildAdapterPreviewInput) (domain.BuildAdapterRenderResult, error) {
	adapter, err := s.GetBuildAdapter(ctx, id)
	if err != nil {
		return domain.BuildAdapterRenderResult{}, err
	}
	return s.preview(ctx, adapter, input, false)
}

// PreviewBuildAdapterDraft 预览尚未保存的草稿：先按保存规则校验草稿（名称可省略），再渲染。
// 草稿没有 id，未传 params 且指定了模块时沿用模块已保存的参数，并忽略草稿未声明的参数，
// 便于编辑已有适配器时用引用它的模块实时预览。
func (s *BuildAdapterService) PreviewBuildAdapterDraft(ctx context.Context, draft domain.BuildAdapterInput, input domain.BuildAdapterPreviewInput) (domain.BuildAdapterRenderResult, error) {
	if strings.TrimSpace(draft.Name) == "" {
		draft.Name = "draft"
	}
	normalized, err := ValidateBuildAdapterDefinition(draft)
	if err != nil {
		return domain.BuildAdapterRenderResult{}, err
	}
	adapter := domain.BuildAdapter{
		Name:               normalized.Name,
		DockerfileTemplate: normalized.DockerfileTemplate,
		Files:              normalized.Files,
		Parameters:         normalized.Parameters,
	}
	return s.preview(ctx, adapter, input, true)
}

func (s *BuildAdapterService) preview(ctx context.Context, adapter domain.BuildAdapter, input domain.BuildAdapterPreviewInput, draft bool) (domain.BuildAdapterRenderResult, error) {
	now := s.now().UTC().Format(time.RFC3339)
	renderContext := domain.BuildAdapterRenderContext{
		Module:      domain.BuildAdapterRenderModule{ID: "sample-module", Name: "Sample Module"},
		Environment: "test",
		Branch:      "main",
		CommitSHA:   strings.Repeat("0", 40),
		Build:       domain.BuildAdapterRenderBuild{ID: "00000000-0000-0000-0000-000000000000", Number: 1, Time: now},
	}
	params := input.Params
	allowUndeclared := false
	if moduleID := strings.TrimSpace(input.ModuleID); moduleID != "" {
		if s.modules == nil {
			return domain.BuildAdapterRenderResult{}, domain.ErrModuleNotFound
		}
		module, err := s.modules.GetModule(ctx, moduleID)
		if err != nil {
			return domain.BuildAdapterRenderResult{}, err
		}
		renderContext.Module = domain.BuildAdapterRenderModule{ID: module.ID, Name: module.Name}
		if module.BuildConfig.AutoBuildEnvironment != "" {
			renderContext.Environment = module.BuildConfig.AutoBuildEnvironment
		}
		if module.Repository.DefaultBranch != "" {
			renderContext.Branch = module.Repository.DefaultBranch
		}
		if params == nil && (draft || module.BuildConfig.BuildAdapterID == adapter.ID) {
			params = module.BuildConfig.BuildAdapterParams
			allowUndeclared = draft
		}
	}

	resolved, err := ResolveBuildAdapterParams(adapter.Parameters, NormalizeBuildAdapterParamValues(params), allowUndeclared)
	if err != nil {
		return domain.BuildAdapterRenderResult{}, err
	}
	renderContext.Params = resolved
	return RenderBuildAdapter(adapter, renderContext)
}

func (s *BuildAdapterService) RecordBuildAdapterUsage(ctx context.Context, buildID string, usage domain.BuildAdapterUsage) error {
	if s.usage == nil {
		return errors.New("build adapter usage recorder not configured")
	}
	return s.usage.RecordBuildAdapterUsage(ctx, strings.TrimSpace(buildID), usage)
}
