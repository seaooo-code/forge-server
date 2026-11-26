package postgres

import (
	"fmt"
	"slices"
	"testing"

	"forge-server/internal/domain"

	"github.com/google/uuid"
)

// 选择规则：只取 Common 模块在当前环境下 route_type = 'default' 且 status = 'active' 的路由，每个运行时版本一个构建；
// 定向、百分比、已失效的路由和其他环境都不返回。
func TestIntegrationListActiveDefaultReleaseBuildsAndRecordDependencies(t *testing.T) {
	pool := newIntegrationPool(t)
	ctx := integrationCtx()
	commonID, environment := seedIntegrationModule(t, pool)
	repository := NewBuildRepository(pool)

	var userID, applicationID, connectionID, storageID string
	if err := pool.QueryRow(ctx, `
		select m.owner_user_id, m.application_id, m.code_source_connection_id, e.storage_configuration_id
		from modules m cross join environments e
		where m.id = $1 and e.code = $2
	`, commonID, environment).Scan(&userID, &applicationID, &connectionID, &storageID); err != nil {
		t.Fatalf("query seed: %v", err)
	}
	otherEnvironment := environment + "-other"
	featureID := commonID + "-feature"
	if _, err := pool.Exec(ctx, `
		insert into environments (id, tenant_id, code, name, status, tone, description, storage_configuration_id, sort_order)
		select 'envid-' || $1, tenant_id, $1, 'other', 'healthy', 'blue', '', $2, 2 from storage_configurations where id = $2
	`, otherEnvironment, storageID); err != nil {
		t.Fatalf("seed environment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into modules (id, tenant_id, name, kind, common_module_id, application_id, owner_user_id, code_source_connection_id, external_repository_id, repository_full_name, sort_order)
		select $1, tenant_id, 'feature', 'Feature', $2, $3, $4, $5, 'repo-feature-' || $1, 'demo/feature', 2 from applications where id = $3
	`, featureID, commonID, applicationID, userID, connectionID); err != nil {
		t.Fatalf("seed feature module: %v", err)
	}

	// route 在 environment 下创建一条成功构建、一条发布和一条路由，返回构建 id。
	route := func(env string, runtimeVersion string, routeType string, identifier string, status string) string {
		t.Helper()
		record, err := repository.CreateBuildRecord(ctx, domain.BuildRecordInput{ModuleID: commonID, Environment: env, TriggerType: domain.BuildTriggerTypeManual})
		if err != nil {
			t.Fatalf("create build: %v", err)
		}
		if _, err := pool.Exec(ctx, `update build_records set status = 'running', worker_id = 'worker-1' where id = $1`, record.ID); err != nil {
			t.Fatalf("mark build running: %v", err)
		}
		if _, err := repository.CompleteBuildRecord(ctx, record.ID, "worker-1", domain.BuildRecordCompletion{
			CommitSHA: fmt.Sprintf("%040d", record.Number), RuntimeVersion: runtimeVersion, Platforms: []string{"android"},
			ArtifactKey: "k/" + record.ID, ManifestKey: "k/" + record.ID + "/manifest.json", FileCount: 1, TotalSizeBytes: 1,
		}); err != nil {
			t.Fatalf("complete build: %v", err)
		}
		releaseID := uuid.NewString()
		releaseType, grayStrategy, expiresAt := "full", "", "null"
		switch routeType {
		case "identifier":
			releaseType, grayStrategy, expiresAt = "gray", "targeted", "now() + interval '1 day'"
		case "percentage":
			releaseType, grayStrategy = "gray", "percentage"
		}
		if _, err := pool.Exec(ctx, `
			insert into release_records (id, module_id, environment_id, build_record_id, release_type, gray_strategy, expires_at, status, artifact_key, created_by_user_id)
			select $1, $2, e.id, $4, $5, $6, `+expiresAt+`, $7, 'k', $8
			from environments e join modules m on m.tenant_id = e.tenant_id where m.id = $2 and e.code = $3
		`, releaseID, commonID, env, record.ID, releaseType, grayStrategy, status, userID); err != nil {
			t.Fatalf("insert release: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			insert into release_routes (id, module_id, environment_id, release_id, runtime_version, route_type, identifier, status, created_by_user_id)
			select $1, $2, e.id, $4, $5, $6, $7, $8, $9
			from environments e join modules m on m.tenant_id = e.tenant_id where m.id = $2 and e.code = $3
		`, uuid.NewString(), commonID, env, releaseID, runtimeVersion, routeType, identifier, status, userID); err != nil {
			t.Fatalf("insert route: %v", err)
		}
		return record.ID
	}

	route(environment, "1.0", "default", "", "inactive")
	wantFirst := route(environment, "1.0", "default", "", "active")
	wantSecond := route(environment, "2.0", "default", "", "active")
	route(environment, "1.0", "identifier", "feature/x", "active")
	route(environment, "3.0", "percentage", "", "active")
	route(otherEnvironment, "4.0", "default", "", "active")

	builds, err := repository.ListActiveDefaultReleaseBuilds(ctx, commonID, environment)
	if err != nil {
		t.Fatalf("list builds: %v", err)
	}
	ids := []string{}
	for _, build := range builds {
		ids = append(ids, build.ID)
		if build.StorageConfigurationID != storageID || build.StorageBucket == "" {
			t.Fatalf("expected dependency build storage from its own record, got %+v", build)
		}
	}
	if !slices.Equal(ids, []string{wantFirst, wantSecond}) {
		t.Fatalf("expected only active default routes per runtimeVersion %v, got %v", []string{wantFirst, wantSecond}, ids)
	}

	feature, err := repository.CreateBuildRecord(ctx, domain.BuildRecordInput{ModuleID: featureID, Environment: environment, TriggerType: domain.BuildTriggerTypeManual})
	if err != nil {
		t.Fatalf("create feature build: %v", err)
	}
	if feature.ModuleKind != domain.ModuleKindFeature || feature.CommonModuleID != commonID || len(feature.DependencyBuilds) != 0 {
		t.Fatalf("unexpected feature build: kind=%s common=%s deps=%v", feature.ModuleKind, feature.CommonModuleID, feature.DependencyBuilds)
	}
	if err := repository.RecordBuildDependencies(ctx, feature.ID, []domain.BuildDependency{
		{ModuleID: commonID, RuntimeVersion: "1.0", BuildRecordID: wantFirst},
		{ModuleID: commonID, RuntimeVersion: "2.0", BuildRecordID: wantSecond},
	}); err != nil {
		t.Fatalf("record dependencies: %v", err)
	}
	var stored string
	if err := pool.QueryRow(ctx, `select dependency_builds::text from build_records where id = $1`, feature.ID).Scan(&stored); err != nil {
		t.Fatalf("query stored dependencies: %v", err)
	}
	if want := fmt.Sprintf(`[{"moduleId": %q, "buildRecordId": %q, "runtimeVersion": "1.0"}, {"moduleId": %q, "buildRecordId": %q, "runtimeVersion": "2.0"}]`, commonID, wantFirst, commonID, wantSecond); stored != want {
		t.Fatalf("unexpected stored dependency_builds:\n%s\nwant\n%s", stored, want)
	}

	loaded, err := repository.GetBuildRecord(ctx, feature.ID)
	if err != nil {
		t.Fatalf("get feature build: %v", err)
	}
	firstBuild, _ := repository.GetBuildRecord(ctx, wantFirst)
	if len(loaded.DependencyBuilds) != 2 || loaded.DependencyBuilds[0] != (domain.BuildDependency{
		ModuleID: commonID, ModuleName: "m", RuntimeVersion: "1.0", BuildRecordID: wantFirst, BuildNumber: firstBuild.Number,
	}) || loaded.DependencyBuilds[1].RuntimeVersion != "2.0" {
		t.Fatalf("unexpected loaded dependencies: %+v", loaded.DependencyBuilds)
	}
	records, _, err := repository.ListBuildRecords(ctx, domain.BuildRecordFilters{ModuleID: featureID, Pagination: domain.Pagination{Page: 1, PageSize: 10}})
	if err != nil || len(records) != 1 || len(records[0].DependencyBuilds) != 2 {
		t.Fatalf("expected dependencies in list, got %v %+v", err, records)
	}
	if err := repository.RecordBuildDependencies(ctx, uuid.NewString(), nil); err != domain.ErrBuildRecordNotFound {
		t.Fatalf("expected not found for missing build, got %v", err)
	}
}
