package service

import (
	"context"
	"strings"
	"testing"

	"forge-server/internal/domain"
)

type fakeBuildDependencyRepository struct {
	builds   []domain.BuildRecord
	calls    [][2]string
	recorded []domain.BuildDependency
}

func (r *fakeBuildDependencyRepository) ListActiveDefaultReleaseBuilds(_ context.Context, moduleID string, environment string) ([]domain.BuildRecord, error) {
	r.calls = append(r.calls, [2]string{moduleID, environment})
	return r.builds, nil
}

func (r *fakeBuildDependencyRepository) RecordBuildDependencies(_ context.Context, _ string, dependencies []domain.BuildDependency) error {
	r.recorded = dependencies
	return nil
}

func dependencyServiceTestBuild(id string, runtimeVersion string) domain.BuildRecord {
	return domain.BuildRecord{
		ID:             id,
		ModuleID:       "common",
		Environment:    "qa",
		Status:         domain.BuildStatusSucceeded,
		RuntimeVersion: runtimeVersion,
		Platforms:      []string{"android"},
		ManifestKey:    id + "/manifest.json",
	}
}

func TestResolveBuildDependenciesOnlyForFeatureWithCommon(t *testing.T) {
	repository := &fakeBuildDependencyRepository{}
	service := NewBuildDependencyService(repository)
	for _, record := range []domain.BuildRecord{
		{ModuleKind: domain.ModuleKindCommon, Environment: "qa"},
		{ModuleKind: domain.ModuleKindFeature, Environment: "qa"},
	} {
		_, ok, err := service.ResolveBuildDependencies(context.Background(), record)
		if err != nil || ok {
			t.Fatalf("expected no dependencies for %+v, got ok=%v err=%v", record, ok, err)
		}
	}
	if len(repository.calls) != 0 {
		t.Fatalf("expected no repository calls, got %v", repository.calls)
	}
}

func TestResolveBuildDependenciesGroupsByRuntimeVersion(t *testing.T) {
	repository := &fakeBuildDependencyRepository{builds: []domain.BuildRecord{
		dependencyServiceTestBuild("build-2", "2.0"),
		dependencyServiceTestBuild("build-1", "1.0"),
	}}
	plan, ok, err := NewBuildDependencyService(repository).ResolveBuildDependencies(context.Background(), domain.BuildRecord{
		ModuleKind: domain.ModuleKindFeature, CommonModuleID: "common", Environment: "qa",
	})
	if err != nil || !ok {
		t.Fatalf("expected dependencies, got ok=%v err=%v", ok, err)
	}
	if len(repository.calls) != 1 || repository.calls[0] != [2]string{"common", "qa"} {
		t.Fatalf("expected lookup of common module in current environment, got %v", repository.calls)
	}
	if plan.ModuleID != "common" || plan.Environment != "qa" || len(plan.Builds) != 2 ||
		plan.Builds[0].ID != "build-1" || plan.Builds[1].ID != "build-2" {
		t.Fatalf("expected builds sorted by runtimeVersion, got %+v", plan)
	}
}

func TestResolveBuildDependenciesEmptyWhenNoDefaultRoute(t *testing.T) {
	plan, ok, err := NewBuildDependencyService(&fakeBuildDependencyRepository{}).ResolveBuildDependencies(context.Background(), domain.BuildRecord{
		ModuleKind: domain.ModuleKindFeature, CommonModuleID: "common", Environment: "qa",
	})
	if err != nil || !ok || len(plan.Builds) != 0 || plan.ModuleID != "common" {
		t.Fatalf("expected empty plan that still injects index, got %+v ok=%v err=%v", plan, ok, err)
	}
}

func TestResolveBuildDependenciesRejectsUnusableBuilds(t *testing.T) {
	tests := []struct {
		name   string
		builds func() []domain.BuildRecord
		want   string
	}{
		{name: "duplicate runtime", want: "多个生效中的默认路由", builds: func() []domain.BuildRecord {
			return []domain.BuildRecord{dependencyServiceTestBuild("build-1", "1.0"), dependencyServiceTestBuild("build-2", "1.0")}
		}},
		{name: "not succeeded", want: "没有可用产物", builds: func() []domain.BuildRecord {
			build := dependencyServiceTestBuild("build-1", "1.0")
			build.Status = domain.BuildStatusFailed
			return []domain.BuildRecord{build}
		}},
		{name: "invalid runtime", want: "runtimeVersion", builds: func() []domain.BuildRecord {
			return []domain.BuildRecord{dependencyServiceTestBuild("build-1", "../1.0")}
		}},
		{name: "invalid platform", want: "平台", builds: func() []domain.BuildRecord {
			build := dependencyServiceTestBuild("build-1", "1.0")
			build.Platforms = []string{"../web"}
			return []domain.BuildRecord{build}
		}},
		{name: "other module", want: "不属于模块", builds: func() []domain.BuildRecord {
			build := dependencyServiceTestBuild("build-1", "1.0")
			build.ModuleID = "other"
			return []domain.BuildRecord{build}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := NewBuildDependencyService(&fakeBuildDependencyRepository{builds: tt.builds()}).ResolveBuildDependencies(context.Background(), domain.BuildRecord{
				ModuleKind: domain.ModuleKindFeature, CommonModuleID: "common", Environment: "qa",
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}
