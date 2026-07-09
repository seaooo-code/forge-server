package service

import (
	"context"
	"testing"
	"time"

	"forge-server/internal/domain"
)

func TestDashboardSnapshotAggregatesRealServices(t *testing.T) {
	now := time.Date(2026, 6, 30, 15, 0, 0, 0, time.UTC)
	startedAt := now.Add(-time.Hour)
	finishedAt := startedAt.Add(3*time.Minute + 12*time.Second)
	service := NewDashboardService(DashboardDependencies{
		ApplicationService: dashboardApplicationFake{summary: domain.ApplicationSummary{Total: 2, Running: 1, Offline: 1}},
		ModuleService:      dashboardModuleFake{summary: domain.ModuleSummary{Total: 3, Common: 1, Feature: 2}},
		BuildService: dashboardBuildFake{records: []domain.BuildRecord{
			{ID: "build-1", ApplicationName: "MiCar", ModuleID: "module-home", ModuleName: "Home", Environment: "product", Status: domain.BuildStatusSucceeded, WorkerID: "worker-1", CreatedAt: now.AddDate(0, 0, -1), StartedAt: &startedAt, FinishedAt: &finishedAt},
			{ID: "build-2", ApplicationName: "MiCar", ModuleID: "module-user", ModuleName: "User", Environment: "staging", Status: domain.BuildStatusFailed, WorkerID: "worker-2", CreatedAt: now.AddDate(0, 0, -8), StartedAt: &startedAt, FinishedAt: &finishedAt},
		}},
		ReleaseService: dashboardReleaseFake{records: []domain.ReleaseRecord{
			{ID: "rel-1", ApplicationName: "MiCar", ModuleID: "module-home", ModuleName: "Home", Environment: "product", ReleaseType: domain.ReleaseTypeGray, GrayStrategy: domain.ReleaseGrayStrategyPercentage, GrayPercentage: 20, Status: domain.ReleaseStatusActive, UpdatedAt: now},
		}},
		CodeSourceService:  dashboardCodeSourceFake{apps: []domain.CodeSourceApp{{Status: domain.CodeSourceAppStatusNotInstalled, ConfigurationStatus: domain.CodeSourceAppConfigurationStatusNotConfigured}}},
		StorageService:     dashboardStorageFake{configs: []domain.StorageConfiguration{{Status: domain.StorageStatusError}}},
		EnvironmentService: dashboardEnvironmentFake{environments: []domain.Environment{{Status: domain.EnvironmentStatusGray}}},
		ArtifactAccessTokenService: dashboardTokenFake{tokens: []domain.ArtifactAccessToken{
			{Status: domain.ArtifactAccessTokenStatusActive},
			{Status: domain.ArtifactAccessTokenStatusDisabled},
		}},
	})
	service.now = func() time.Time { return now }

	snapshot, err := service.GetDashboardSnapshot(context.Background(), "ou-user")
	if err != nil {
		t.Fatalf("get dashboard snapshot: %v", err)
	}

	if len(snapshot.BuildTrend) != 7 {
		t.Fatalf("expected 7 trend points, got %d", len(snapshot.BuildTrend))
	}
	if snapshot.BuildTrend[5].BuildCount != 1 || snapshot.BuildTrend[5].SuccessCount != 1 {
		t.Fatalf("expected in-range successful build on previous day, got %#v", snapshot.BuildTrend)
	}
	if snapshot.Metrics[2].Value != "1" || snapshot.Metrics[3].Value != "100.0%" {
		t.Fatalf("unexpected build metrics: %#v", snapshot.Metrics[2:4])
	}
	if len(snapshot.Builds) != 2 || snapshot.Builds[0].ID != "build-1" || snapshot.Builds[0].Status != "success" {
		t.Fatalf("unexpected recent builds: %#v", snapshot.Builds)
	}
	if len(snapshot.Releases) != 1 || snapshot.Releases[0].Gray != "20%" || snapshot.Releases[0].Status != "active" {
		t.Fatalf("unexpected releases: %#v", snapshot.Releases)
	}
	if snapshot.Resources[0].Status != "attention" || snapshot.Resources[2].Status != "attention" || snapshot.Resources[5].StatusLabel != "1 个需处理" {
		t.Fatalf("unexpected resources: %#v", snapshot.Resources)
	}
}

func TestDashboardSnapshotHandlesEmptyData(t *testing.T) {
	service := NewDashboardService(DashboardDependencies{})
	service.now = func() time.Time { return time.Date(2026, 6, 30, 15, 0, 0, 0, time.UTC) }

	snapshot, err := service.GetDashboardSnapshot(context.Background(), "")
	if err != nil {
		t.Fatalf("get dashboard snapshot: %v", err)
	}

	if len(snapshot.BuildTrend) != 7 || len(snapshot.Builds) != 0 || len(snapshot.Releases) != 0 {
		t.Fatalf("unexpected empty snapshot: %#v", snapshot)
	}
	if snapshot.Metrics[3].Value != "-" {
		t.Fatalf("expected empty success rate, got %#v", snapshot.Metrics[3])
	}
}

type dashboardApplicationFake struct{ summary domain.ApplicationSummary }

func (f dashboardApplicationFake) ListApplications(context.Context, domain.ApplicationListFilters) ([]domain.Application, domain.ApplicationSummary, error) {
	return nil, f.summary, nil
}

type dashboardModuleFake struct{ summary domain.ModuleSummary }

func (f dashboardModuleFake) ListModules(context.Context, domain.ModuleListFilters) ([]domain.Module, domain.ModuleSummary, error) {
	return nil, f.summary, nil
}

type dashboardBuildFake struct{ records []domain.BuildRecord }

func (f dashboardBuildFake) ListBuildRecords(context.Context, domain.BuildRecordFilters) ([]domain.BuildRecord, domain.BuildRecordSummary, error) {
	return f.records, domain.BuildRecordSummary{Total: len(f.records)}, nil
}

type dashboardReleaseFake struct{ records []domain.ReleaseRecord }

func (f dashboardReleaseFake) ListReleases(context.Context, domain.ReleaseRecordFilters) ([]domain.ReleaseRecord, error) {
	return f.records, nil
}

type dashboardCodeSourceFake struct{ apps []domain.CodeSourceApp }

func (f dashboardCodeSourceFake) ListCodeSources(context.Context, string) ([]domain.CodeSourceApp, error) {
	return f.apps, nil
}

type dashboardStorageFake struct{ configs []domain.StorageConfiguration }

func (f dashboardStorageFake) ListStorageConfigurations(context.Context) ([]domain.StorageConfiguration, error) {
	return f.configs, nil
}

type dashboardEnvironmentFake struct{ environments []domain.Environment }

func (f dashboardEnvironmentFake) ListEnvironments(context.Context) ([]domain.Environment, error) {
	return f.environments, nil
}

type dashboardTokenFake struct{ tokens []domain.ArtifactAccessToken }

func (f dashboardTokenFake) ListArtifactAccessTokens(context.Context) ([]domain.ArtifactAccessToken, error) {
	return f.tokens, nil
}
