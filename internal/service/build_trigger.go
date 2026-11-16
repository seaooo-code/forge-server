package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"slices"
	"strings"
	"time"

	"forge-server/internal/domain"
)

// AutoBuildModuleRepository 查找自动构建规则相关的模块。
type AutoBuildModuleRepository interface {
	ListAutoBuildModules(ctx context.Context, connectionID string, externalRepositoryID string) ([]domain.Module, error)
	GetModule(ctx context.Context, id string) (domain.Module, error)
}

// WebhookBuildRepository 幂等创建 webhook 构建，需要在数据库层面保证并发重投不会重复创建。
type WebhookBuildRepository interface {
	// CreateBuildRecordUnlessActive 在同一 (module_id, environment_code, commit_sha) 已有
	// pending/running/succeeded 构建时返回已有构建且 created=false。
	CreateBuildRecordUnlessActive(ctx context.Context, input domain.BuildRecordInput) (record domain.BuildRecord, created bool, err error)
}

// PreviewReleaseCreator 复用发布服务创建预览通道发布。
type PreviewReleaseCreator interface {
	CreateRelease(ctx context.Context, input domain.ReleaseCreateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error)
}

// BuildTriggerService 负责 push 自动构建与构建成功后的预览通道发布，与具体 provider 无关。
type BuildTriggerService struct {
	builds   WebhookBuildRepository
	logger   *slog.Logger
	modules  AutoBuildModuleRepository
	now      func() time.Time
	releases PreviewReleaseCreator
}

type BuildTriggerServiceDependencies struct {
	Builds   WebhookBuildRepository
	Logger   *slog.Logger
	Modules  AutoBuildModuleRepository
	Releases PreviewReleaseCreator
}

func NewBuildTriggerService(deps BuildTriggerServiceDependencies) *BuildTriggerService {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &BuildTriggerService{
		builds:   deps.Builds,
		logger:   logger,
		modules:  deps.Modules,
		now:      time.Now,
		releases: deps.Releases,
	}
}

// HandlePushEvent 找出绑定该仓库且自动构建分支匹配的模块，在 auto_build_environment_code 下幂等创建 webhook 构建。
// 单个模块失败只记录日志；所有命中模块都失败时返回错误，让托管方重投（幂等保证不会重复创建）。
func (s *BuildTriggerService) HandlePushEvent(ctx context.Context, event domain.PushEvent) (domain.PushEventResult, error) {
	result := domain.PushEventResult{CreatedBuildIDs: []string{}, DuplicateBuildIDs: []string{}}
	branch := strings.TrimSpace(event.Branch)
	commitSHA := strings.TrimSpace(event.CommitSHA)
	if branch == "" || commitSHA == "" || strings.TrimSpace(event.ConnectionID) == "" || strings.TrimSpace(event.ExternalRepositoryID) == "" {
		return result, nil
	}

	modules, err := s.modules.ListAutoBuildModules(ctx, strings.TrimSpace(event.ConnectionID), strings.TrimSpace(event.ExternalRepositoryID))
	if err != nil {
		return result, err
	}

	var failures error
	matched := 0
	for _, module := range modules {
		environment := strings.TrimSpace(module.BuildConfig.AutoBuildEnvironment)
		matchesBranch := slices.ContainsFunc(module.BuildConfig.AutoBuildBranches, func(pattern string) bool {
			return MatchBranchPattern(strings.TrimSpace(pattern), branch)
		})
		if environment == "" || !matchesBranch {
			continue
		}
		matched++

		input, err := validateBuildRecordInput(domain.BuildRecordInput{
			ModuleID:    module.ID,
			Environment: environment,
			Branch:      branch,
			CommitSHA:   commitSHA,
			TriggerID:   event.DeliveryID,
			TriggerType: domain.BuildTriggerTypeWebhook,
			// webhook 构建记在代码源连接创建人名下。
			CreatedByUserID: event.ConnectionCreatedByUserID,
		})
		var record domain.BuildRecord
		created := false
		if err == nil {
			record, created, err = s.builds.CreateBuildRecordUnlessActive(ctx, input)
		}
		if err != nil {
			s.logger.Error("create webhook build failed", "module", module.ID, "environment", environment, "branch", branch, "commit", commitSHA, "error", err)
			failures = errors.Join(failures, fmt.Errorf("module %s: %w", module.ID, err))
			continue
		}
		if created {
			result.CreatedBuildIDs = append(result.CreatedBuildIDs, record.ID)
		} else {
			result.DuplicateBuildIDs = append(result.DuplicateBuildIDs, record.ID)
		}
		s.logger.Info("webhook build triggered", "module", module.ID, "environment", environment, "branch", branch, "commit", commitSHA, "build", record.ID, "created", created)
	}

	if failures != nil && !result.Matched() {
		return result, failures
	}
	if matched == 0 {
		s.logger.Info("push event ignored: no module matched", "connection", event.ConnectionID, "repository", event.ExternalRepositoryID, "branch", branch)
	}
	return result, nil
}

// HandleBuildSucceeded 实现 BuildSucceededHook：webhook 构建成功且模块开启 auto_preview_release 时，
// 以分支名为 identifier 创建定向灰度发布。失败只记录日志，不影响构建状态。
func (s *BuildTriggerService) HandleBuildSucceeded(ctx context.Context, record domain.BuildRecord) {
	if record.TriggerType != domain.BuildTriggerTypeWebhook || s.releases == nil {
		return
	}
	logger := s.logger.With("build", record.ID, "module", record.ModuleID, "environment", record.Environment, "branch", record.Branch)

	module, err := s.modules.GetModule(ctx, record.ModuleID)
	if err != nil {
		logger.Error("load module for preview release failed", "error", err)
		return
	}
	if !module.BuildConfig.AutoPreviewRelease {
		return
	}
	// 发布必须有创建人；历史构建或创建人已删除时 created_by_user_id 为空，跳过预览发布。
	if strings.TrimSpace(record.CreatedByUserID) == "" {
		logger.Warn("skip preview release: build has no creator")
		return
	}

	input, err := s.previewReleaseInput(record, module)
	if err != nil {
		logger.Error("prepare preview release failed", "error", err)
		return
	}
	release, route, err := s.releases.CreateRelease(ctx, input)
	if err != nil {
		logger.Error("create preview release failed", "error", err)
		return
	}
	logger.Info("preview release created", "release", release.ID, "route", route.ID, "runtimeVersion", route.RuntimeVersion, "expiresAt", input.ExpiresAt)
}

func (s *BuildTriggerService) previewReleaseInput(record domain.BuildRecord, module domain.Module) (domain.ReleaseCreateInput, error) {
	branch := strings.TrimSpace(record.Branch)
	if branch == "" {
		return domain.ReleaseCreateInput{}, errors.New("build branch is empty")
	}

	ttlHours := module.BuildConfig.AutoPreviewTTLHours
	if ttlHours <= 0 {
		ttlHours = domain.DefaultModuleAutoPreviewTTLHours
	}
	expiresAt := s.now().Add(time.Duration(ttlHours) * time.Hour)
	shortSHA := record.CommitSHA
	if len(shortSHA) > 12 {
		shortSHA = shortSHA[:12]
	}
	return domain.ReleaseCreateInput{
		BuildRecordID:   record.ID,
		ReleaseType:     domain.ReleaseTypeGray,
		GrayStrategy:    domain.ReleaseGrayStrategyTargeted,
		ExpiresAt:       &expiresAt,
		Identifier:      branch,
		Description:     "预览通道：" + branch + "@" + shortSHA,
		CreatedByUserID: strings.TrimSpace(record.CreatedByUserID),
	}, nil
}

// MatchBranchPattern 按 "/" 分段匹配分支名：`*` 只匹配单段内的任意字符（语义同 path.Match），
// `**` 作为独立一段时匹配零个或多个段。例如 `feature/*` 匹配 `feature/login`，`release/**` 匹配 `release/1.0/hotfix`。
func MatchBranchPattern(pattern string, branch string) bool {
	if pattern == "" || branch == "" {
		return false
	}
	return matchBranchSegments(strings.Split(pattern, "/"), strings.Split(branch, "/"))
}

func matchBranchSegments(patterns []string, segments []string) bool {
	if len(patterns) == 0 {
		return len(segments) == 0
	}
	if patterns[0] == "**" {
		for index := 0; index <= len(segments); index++ {
			if matchBranchSegments(patterns[1:], segments[index:]) {
				return true
			}
		}
		return false
	}
	if len(segments) == 0 {
		return false
	}
	ok, err := path.Match(patterns[0], segments[0])
	return err == nil && ok && matchBranchSegments(patterns[1:], segments[1:])
}
