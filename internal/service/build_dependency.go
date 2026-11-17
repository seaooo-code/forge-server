package service

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"forge-server/internal/domain"
)

// BuildDependencyRepository 读取依赖模块生效中的默认路由构建，并把注入结果写回构建记录。
type BuildDependencyRepository interface {
	ListActiveDefaultReleaseBuilds(ctx context.Context, moduleID string, environment string) ([]domain.BuildRecord, error)
	RecordBuildDependencies(ctx context.Context, buildID string, dependencies []domain.BuildDependency) error
}

// BuildDependencyPlan 是一次构建需要注入的依赖：所属 Common 模块，以及它在当前环境下每个运行时版本生效中的默认路由构建。
type BuildDependencyPlan struct {
	ModuleID    string
	Environment string
	// Builds 按 runtimeVersion 排序，每个运行时版本一个构建；为空表示 Common 模块没有生效中的默认路由。
	Builds []domain.BuildRecord
}

type BuildDependencyService struct {
	repository BuildDependencyRepository
}

func NewBuildDependencyService(repository BuildDependencyRepository) *BuildDependencyService {
	return &BuildDependencyService{repository: repository}
}

// ResolveBuildDependencies 只对配置了 common_module_id 的 Feature 模块返回 ok=true。
// 选择规则：所属 Common 模块在当前构建环境下所有生效中的默认路由（定向、百分比路由不注入），按 runtimeVersion 区分，
// 每个运行时版本取一个构建。路由指向的构建不可用（未成功、没有清单、运行时版本或平台不合法）时返回错误。
func (s *BuildDependencyService) ResolveBuildDependencies(ctx context.Context, record domain.BuildRecord) (BuildDependencyPlan, bool, error) {
	commonModuleID := strings.TrimSpace(record.CommonModuleID)
	if record.ModuleKind != domain.ModuleKindFeature || commonModuleID == "" {
		return BuildDependencyPlan{}, false, nil
	}
	builds, err := s.repository.ListActiveDefaultReleaseBuilds(ctx, commonModuleID, record.Environment)
	if err != nil {
		return BuildDependencyPlan{}, false, err
	}

	plan := BuildDependencyPlan{ModuleID: commonModuleID, Environment: record.Environment, Builds: make([]domain.BuildRecord, 0, len(builds))}
	for _, build := range builds {
		if build.ModuleID != commonModuleID || build.Environment != record.Environment {
			return BuildDependencyPlan{}, false, fmt.Errorf("依赖构建 %s 不属于模块 %s 的 %s 环境", build.ID, commonModuleID, record.Environment)
		}
		if build.Status != domain.BuildStatusSucceeded || strings.TrimSpace(build.ManifestKey) == "" {
			return BuildDependencyPlan{}, false, fmt.Errorf("依赖构建 #%d（%s）没有可用产物", build.Number, build.ID)
		}
		if !domain.IsValidRuntimeVersion(build.RuntimeVersion) {
			return BuildDependencyPlan{}, false, fmt.Errorf("依赖构建 #%d（%s）的 runtimeVersion %q 不合法", build.Number, build.ID, build.RuntimeVersion)
		}
		for _, platform := range build.Platforms {
			if !domain.IsValidPlatform(platform) {
				return BuildDependencyPlan{}, false, fmt.Errorf("依赖构建 #%d（%s）的平台 %q 不合法", build.Number, build.ID, platform)
			}
		}
		if slices.ContainsFunc(plan.Builds, func(existing domain.BuildRecord) bool { return existing.RuntimeVersion == build.RuntimeVersion }) {
			return BuildDependencyPlan{}, false, fmt.Errorf("模块 %s 在 runtimeVersion %s 下有多个生效中的默认路由", commonModuleID, build.RuntimeVersion)
		}
		plan.Builds = append(plan.Builds, build)
	}
	slices.SortFunc(plan.Builds, func(a, b domain.BuildRecord) int { return strings.Compare(a.RuntimeVersion, b.RuntimeVersion) })
	return plan, true, nil
}

func (s *BuildDependencyService) RecordBuildDependencies(ctx context.Context, buildID string, dependencies []domain.BuildDependency) error {
	return s.repository.RecordBuildDependencies(ctx, buildID, dependencies)
}
