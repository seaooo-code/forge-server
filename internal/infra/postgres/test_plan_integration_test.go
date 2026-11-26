package postgres

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"forge-server/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testPlanFixture 是提测单集成测试的数据：一个应用、一个开启了提测单的环境、一个 Common 模块和一个 Feature 模块。
type testPlanFixture struct {
	pool        *pgxpool.Pool
	repository  *TestPlanRepository
	artifacts   *ArtifactRepository
	userID      string
	application string
	slug        string
	environment string
	common      string
	feature     string
	nextNumber  map[string]int64
}

func newTestPlanFixture(t *testing.T) *testPlanFixture {
	t.Helper()
	pool := newIntegrationPool(t)
	ctx := integrationCtx()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	f := &testPlanFixture{
		pool:        pool,
		repository:  NewTestPlanRepository(pool),
		artifacts:   NewArtifactRepository(pool),
		userID:      "user-" + suffix,
		application: "app-" + suffix,
		slug:        "app-" + suffix,
		environment: "env-" + suffix,
		common:      "common-" + suffix,
		feature:     "feature-" + suffix,
		nextNumber:  map[string]int64{},
	}
	for _, statement := range []string{
		`insert into tenants (id, name, status, sort_order) select 'tnt-' || $2::text, 't', 'active', 1 where $1::text <> ''`,
		`insert into users (id, tenant_id, name) select $1::text, 'tnt-' || $2::text, 'tester' || $2::text`,
		`insert into storage_configurations (id, tenant_id, name, adapter, bucket, endpoint, region, owner_user_id, access_key_id, secret_access_key, status, sort_order)
			values ('storage-' || $2::text, 'tnt-' || $2, 's', 'S3', 'b', 'http://s3.example.com', 'r', $1, 'k', 's', 'healthy', 1)`,
		`insert into environments (id, tenant_id, code, name, status, tone, description, storage_configuration_id, sort_order, allow_test_plans)
			select 'envid-' || $2::text, 'tnt-' || $2, 'env-' || $2::text, 'e', 'healthy', 'blue', '', 'storage-' || $2::text, 1, true where $1::text <> ''`,
		`insert into applications (id, tenant_id, slug, name, status, platform, owner_user_id, description, sort_order)
			values ('app-' || $2::text, 'tnt-' || $2, 'app-' || $2::text, 'a', 'running', 'rn', $1, '', 1)`,
		`insert into application_environment_bindings (tenant_id, application_id, environment_id, sort_order)
			select 'tnt-' || $2, 'app-' || $2::text, 'envid-' || $2::text, 1 where $1::text <> ''`,
		`insert into code_source_connections (id, tenant_id, provider, name, web_base_url, api_base_url, status, created_by_user_id)
			values ('conn-' || $2::text, 'tnt-' || $2, 'gitlab', 'c', 'https://git.example.com', 'https://git.example.com/api/v4', 'active', $1)`,
		`insert into modules (id, tenant_id, name, kind, application_id, owner_user_id, code_source_connection_id, external_repository_id, repository_full_name, sort_order)
			values ('common-' || $2::text, 'tnt-' || $2, 'common', 'Common', 'app-' || $2::text, $1, 'conn-' || $2::text, 'repo-c-' || $2, 'demo/common', 1)`,
		`insert into modules (id, tenant_id, name, kind, application_id, owner_user_id, code_source_connection_id, external_repository_id, repository_full_name, sort_order, common_module_id)
			values ('feature-' || $2::text, 'tnt-' || $2, 'feature', 'Feature', 'app-' || $2::text, $1, 'conn-' || $2::text, 'repo-f-' || $2, 'demo/feature', 2, 'common-' || $2::text)`,
	} {
		if _, err := pool.Exec(ctx, statement, f.userID, suffix); err != nil {
			t.Fatalf("seed: %v\n%s", err, statement)
		}
	}
	return f
}

// build 插入一条构建成功、有产物的构建记录，返回 ID 和构建号。
func (f *testPlanFixture) build(t *testing.T, moduleID string, branch string, runtimeVersion string) (string, int64) {
	t.Helper()
	f.nextNumber[moduleID]++
	number := f.nextNumber[moduleID]
	id := uuid.NewString()
	if _, err := f.pool.Exec(integrationCtx(), `
		insert into build_records (id, module_id, number, environment_id, storage_configuration_id, branch, commit_sha, status,
			runtime_version, platforms, artifact_key, manifest_key, created_by_user_id)
		select $1::uuid, $2, $3::bigint, e.id, e.storage_configuration_id, $5, 'sha-' || $3::text, 'succeeded', $6, '{android}', 'artifacts/' || $1::text, 'artifacts/' || $1::text || '/manifest.json', $7
		from environments e join modules m on m.tenant_id = e.tenant_id where m.id = $2 and e.code = $4
	`, id, moduleID, number, f.environment, branch, runtimeVersion, f.userID); err != nil {
		t.Fatalf("insert build: %v", err)
	}
	return id, number
}

func (f *testPlanFixture) createPlan(t *testing.T, name string, items []domain.TestPlanItemInput, members []string) domain.TestPlanMutationResult {
	t.Helper()
	result, err := f.createPlanErr(name, items, members, false)
	if err != nil {
		t.Fatalf("create test plan %s: %v", name, err)
	}
	return result
}

func (f *testPlanFixture) createPlanErr(name string, items []domain.TestPlanItemInput, members []string, resolve bool) (domain.TestPlanMutationResult, error) {
	expiresAt := time.Now().Add(24 * time.Hour)
	memberInputs := make([]domain.TestPlanMemberInput, 0, len(members))
	for _, member := range members {
		memberInputs = append(memberInputs, domain.TestPlanMemberInput{UserID: member})
	}
	return f.repository.CreateTestPlan(integrationCtx(), domain.TestPlanCreateInput{
		ApplicationID:    f.application,
		Environment:      f.environment,
		Name:             name,
		ExpiresAt:        &expiresAt,
		Items:            items,
		Members:          memberInputs,
		ResolveConflicts: resolve,
		ActorUserID:      f.userID,
	})
}

func (f *testPlanFixture) manifestBuild(t *testing.T, userID string, runtimeVersion string, moduleID string) domain.ArtifactModuleReleaseSource {
	t.Helper()
	source, err := f.artifacts.GetArtifactManifestSource(integrationCtx(), domain.ArtifactManifestInput{
		ApplicationSlug: f.slug,
		Environment:     f.environment,
		Platform:        domain.PlatformAndroid,
		RuntimeVersion:  runtimeVersion,
		UserID:          userID,
	})
	if err != nil {
		t.Fatalf("manifest source: %v", err)
	}
	for _, module := range source.Modules {
		if module.ID == moduleID {
			return module
		}
	}
	t.Fatalf("module %s missing from manifest source", moduleID)
	return domain.ArtifactModuleReleaseSource{}
}

func follow(moduleID string, branch string) domain.TestPlanItemInput {
	return domain.TestPlanItemInput{ModuleID: moduleID, Branch: branch, Mode: domain.TestPlanItemModeFollow}
}

// 同一模块的同一分支只能属于一张进行中的提测单；关闭后可以再用。
func TestIntegrationTestPlanBranchUniqueness(t *testing.T) {
	f := newTestPlanFixture(t)
	f.build(t, f.feature, "feature/a", "rt-1")
	first := f.createPlan(t, "A", []domain.TestPlanItemInput{follow(f.feature, "feature/a")}, nil)

	_, err := f.createPlanErr("B", []domain.TestPlanItemInput{follow(f.feature, "feature/a")}, nil, true)
	var inUse *domain.TestPlanBranchInUseError
	if !errors.As(err, &inUse) || inUse.TestPlan.ID != first.Detail.Plan.ID || inUse.Branch != "feature/a" {
		t.Fatalf("expected branch in use by A, got %v", err)
	}

	if _, err := f.repository.CloseTestPlan(integrationCtx(), first.Detail.Plan.ID, f.userID); err != nil {
		t.Fatalf("close A: %v", err)
	}
	if _, err := f.createPlanErr("B", []domain.TestPlanItemInput{follow(f.feature, "feature/a")}, nil, false); err != nil {
		t.Fatalf("expected branch to be free after A closed, got %v", err)
	}
}

// 成员冲突：不确认时返回冲突且不修改；确认后只把冲突的 userId 从对方移出，并记录操作。
func TestIntegrationTestPlanMemberConflict(t *testing.T) {
	f := newTestPlanFixture(t)
	f.build(t, f.feature, "feature/a", "rt-1")
	f.build(t, f.feature, "feature/b", "rt-1")
	planA := f.createPlan(t, "A", []domain.TestPlanItemInput{follow(f.feature, "feature/a")}, []string{"1001", "1002"})
	planB := f.createPlan(t, "B", []domain.TestPlanItemInput{follow(f.feature, "feature/b")}, nil)
	ctx := integrationCtx()

	_, err := f.repository.AddTestPlanMembers(ctx, planB.Detail.Plan.ID, []domain.TestPlanMemberInput{{UserID: "1001"}}, false, f.userID)
	var conflict *domain.TestPlanMemberConflictError
	if !errors.As(err, &conflict) || len(conflict.Conflicts) != 1 ||
		conflict.Conflicts[0].UserID != "1001" || conflict.Conflicts[0].TestPlan.ID != planA.Detail.Plan.ID ||
		len(conflict.Conflicts[0].ModuleIDs) != 1 || conflict.Conflicts[0].ModuleIDs[0] != f.feature {
		t.Fatalf("expected conflict with A on the feature module, got %v", err)
	}
	detailB, err := f.repository.GetTestPlanDetail(ctx, planB.Detail.Plan.ID)
	if err != nil || len(detailB.Members) != 0 {
		t.Fatalf("expected B unchanged after a rejected conflict, got members=%v err=%v", detailB.Members, err)
	}

	result, err := f.repository.AddTestPlanMembers(ctx, planB.Detail.Plan.ID, []domain.TestPlanMemberInput{{UserID: "1001"}}, true, f.userID)
	if err != nil {
		t.Fatalf("add with resolve: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0].UserID != "1001" || len(result.Detail.Members) != 1 {
		t.Fatalf("unexpected resolve result: %+v", result)
	}
	detailA, err := f.repository.GetTestPlanDetail(ctx, planA.Detail.Plan.ID)
	if err != nil {
		t.Fatalf("detail A: %v", err)
	}
	if len(detailA.Members) != 1 || detailA.Members[0].UserID != "1002" || len(detailA.Items) != 1 {
		t.Fatalf("expected only 1001 removed from A, got members=%+v items=%d", detailA.Members, len(detailA.Items))
	}
	if detailA.Events[0].Kind != domain.TestPlanEventMemberRemovedByConflict ||
		detailA.Events[0].Detail["userId"] != "1001" ||
		detailA.Events[0].Detail["causedByTestPlanId"] != planB.Detail.Plan.ID {
		t.Fatalf("expected removal event on A, got %+v", detailA.Events[0])
	}

	// manifest：1001 现在拿 B 的构建，1002 拿 A 的构建。
	if module := f.manifestBuild(t, "1001", "rt-1", f.feature); module.TestPlanID != planB.Detail.Plan.ID {
		t.Fatalf("expected 1001 to hit B, got %+v", module)
	}
	if module := f.manifestBuild(t, "1002", "rt-1", f.feature); module.TestPlanID != planA.Detail.Plan.ID {
		t.Fatalf("expected 1002 to hit A, got %+v", module)
	}
}

// 加条目同样会触发成员冲突：两张单原本测不同模块，B 加入 A 已有的模块后，共同成员冲突。
func TestIntegrationTestPlanItemCausesMemberConflict(t *testing.T) {
	f := newTestPlanFixture(t)
	f.build(t, f.common, "feature/a", "rt-1")
	f.build(t, f.feature, "feature/b", "rt-1")
	f.build(t, f.common, "feature/c", "rt-1")
	planA := f.createPlan(t, "A", []domain.TestPlanItemInput{follow(f.common, "feature/a")}, []string{"1001"})
	planB := f.createPlan(t, "B", []domain.TestPlanItemInput{follow(f.feature, "feature/b")}, []string{"1001"})

	_, err := f.repository.SetTestPlanItem(integrationCtx(), planB.Detail.Plan.ID, follow(f.common, "feature/c"), false, f.userID)
	var conflict *domain.TestPlanMemberConflictError
	if !errors.As(err, &conflict) || conflict.Conflicts[0].TestPlan.ID != planA.Detail.Plan.ID {
		t.Fatalf("expected member conflict with A, got %v", err)
	}
	detailB, _ := f.repository.GetTestPlanDetail(integrationCtx(), planB.Detail.Plan.ID)
	if len(detailB.Items) != 1 {
		t.Fatalf("expected B items unchanged, got %d", len(detailB.Items))
	}
}

// 并发给同一 userId 加入两张包含同一模块的提测单，只能成功一个。
func TestIntegrationTestPlanConcurrentJoin(t *testing.T) {
	f := newTestPlanFixture(t)
	f.build(t, f.feature, "feature/a", "rt-1")
	f.build(t, f.feature, "feature/b", "rt-1")
	planA := f.createPlan(t, "A", []domain.TestPlanItemInput{follow(f.feature, "feature/a")}, nil)
	planB := f.createPlan(t, "B", []domain.TestPlanItemInput{follow(f.feature, "feature/b")}, nil)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for index, planID := range []string{planA.Detail.Plan.ID, planB.Detail.Plan.ID} {
		wg.Add(1)
		go func(index int, planID string) {
			defer wg.Done()
			_, errs[index] = f.repository.AddTestPlanMembers(integrationCtx(), planID, []domain.TestPlanMemberInput{{UserID: "2001"}}, false, f.userID)
		}(index, planID)
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		var conflict *domain.TestPlanMemberConflictError
		switch {
		case err == nil:
			succeeded++
		case !errors.As(err, &conflict):
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("expected exactly one join to succeed, got %d (%v)", succeeded, errs)
	}
}

// manifest 解析：提测单优先于全量；runtimeVersion 不一致、环境未开启、过期、没有 userId 时不命中。
func TestIntegrationTestPlanManifestResolution(t *testing.T) {
	f := newTestPlanFixture(t)
	ctx := integrationCtx()
	releaseBuild, _ := f.build(t, f.feature, "main", "rt-1")
	planBuild, _ := f.build(t, f.feature, "feature/a", "rt-1")
	if _, _, err := NewReleaseRepository(f.pool).CreateRelease(ctx, domain.ReleaseCreateInput{
		BuildRecordID:   releaseBuild,
		ReleaseType:     domain.ReleaseTypeFull,
		CreatedByUserID: f.userID,
	}); err != nil {
		t.Fatalf("create full release: %v", err)
	}
	plan := f.createPlan(t, "A", []domain.TestPlanItemInput{follow(f.feature, "feature/a")}, []string{"1001"})

	if module := f.manifestBuild(t, "1001", "rt-1", f.feature); module.BuildRecordID != planBuild || module.ReleaseID != "" || module.TestPlanMatches != 1 {
		t.Fatalf("expected test plan build for member, got %+v", module)
	}
	if module := f.manifestBuild(t, "9999", "rt-1", f.feature); module.BuildRecordID != releaseBuild || module.TestPlanID != "" {
		t.Fatalf("expected full release for non member, got %+v", module)
	}
	if module := f.manifestBuild(t, "", "rt-1", f.feature); module.BuildRecordID != releaseBuild {
		t.Fatalf("expected full release without userId, got %+v", module)
	}
	if module := f.manifestBuild(t, "1001", "rt-2", f.feature); module.TestPlanID != "" {
		t.Fatalf("expected no test plan for another runtimeVersion, got %+v", module)
	}

	if _, err := f.pool.Exec(ctx, `update environments set allow_test_plans = false where code = $1`, f.environment); err != nil {
		t.Fatalf("disable test plans: %v", err)
	}
	if module := f.manifestBuild(t, "1001", "rt-1", f.feature); module.TestPlanID != "" {
		t.Fatalf("expected no test plan when the environment disallows them, got %+v", module)
	}
	if _, err := f.pool.Exec(ctx, `update environments set allow_test_plans = true where code = $1`, f.environment); err != nil {
		t.Fatalf("enable test plans: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `update test_plans set expires_at = now() - interval '1 minute' where id = $1`, plan.Detail.Plan.ID); err != nil {
		t.Fatalf("expire plan: %v", err)
	}
	if module := f.manifestBuild(t, "1001", "rt-1", f.feature); module.TestPlanID != "" {
		t.Fatalf("expected no test plan after expiry, got %+v", module)
	}
	if _, err := f.repository.SetTestPlanItem(ctx, plan.Detail.Plan.ID, follow(f.feature, "feature/a"), false, f.userID); !errors.Is(err, domain.ErrTestPlanClosed) {
		t.Fatalf("expected expired plan to reject changes, got %v", err)
	}
}

// 跟随分支：更新的构建替换跟随条目；固定条目和旧构建的延迟回调不替换。
func TestIntegrationTestPlanFollowBuild(t *testing.T) {
	f := newTestPlanFixture(t)
	ctx := integrationCtx()
	firstFeature, _ := f.build(t, f.feature, "feature/a", "rt-1")
	pinnedCommon, _ := f.build(t, f.common, "feature/c", "rt-1")
	plan := f.createPlan(t, "A", []domain.TestPlanItemInput{
		follow(f.feature, "feature/a"),
		{ModuleID: f.common, Branch: "feature/c", Mode: domain.TestPlanItemModePinned, BuildRecordID: pinnedCommon},
	}, nil)
	if plan.Detail.Items[1].BuildRecordID != firstFeature && plan.Detail.Items[0].BuildRecordID != firstFeature {
		t.Fatalf("expected follow item to start from the latest build, got %+v", plan.Detail.Items)
	}

	newerFeature, newerNumber := f.build(t, f.feature, "feature/a", "rt-1")
	planIDs, err := f.repository.FollowBuild(ctx, domain.BuildRecord{ID: newerFeature, ModuleID: f.feature, Environment: f.environment, Branch: "feature/a", Number: newerNumber})
	if err != nil || len(planIDs) != 1 || planIDs[0] != plan.Detail.Plan.ID {
		t.Fatalf("expected follow to update the plan, got %v %v", planIDs, err)
	}
	newerCommon, newerCommonNumber := f.build(t, f.common, "feature/c", "rt-1")
	if planIDs, err := f.repository.FollowBuild(ctx, domain.BuildRecord{ID: newerCommon, ModuleID: f.common, Environment: f.environment, Branch: "feature/c", Number: newerCommonNumber}); err != nil || len(planIDs) != 0 {
		t.Fatalf("expected pinned item to stay, got %v %v", planIDs, err)
	}
	// 旧构建的延迟回调不覆盖。
	if planIDs, err := f.repository.FollowBuild(ctx, domain.BuildRecord{ID: firstFeature, ModuleID: f.feature, Environment: f.environment, Branch: "feature/a", Number: 1}); err != nil || len(planIDs) != 0 {
		t.Fatalf("expected an older build not to replace a newer one, got %v %v", planIDs, err)
	}

	detail, err := f.repository.GetTestPlanDetail(ctx, plan.Detail.Plan.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	builds := map[string]string{}
	for _, item := range detail.Items {
		builds[item.ModuleID] = item.BuildRecordID
	}
	if builds[f.feature] != newerFeature || builds[f.common] != pinnedCommon {
		t.Fatalf("unexpected item builds after follow: %+v", builds)
	}
	if detail.Events[0].Kind != domain.TestPlanEventItemFollowed {
		t.Fatalf("expected a follow event, got %+v", detail.Events[0])
	}
}

// 条目的构建必须属于该模块、该分支、同环境且构建成功；环境未开启提测单时不能新建。
func TestIntegrationTestPlanValidation(t *testing.T) {
	f := newTestPlanFixture(t)
	other, _ := f.build(t, f.feature, "feature/other", "rt-1")
	if _, err := f.createPlanErr("A", []domain.TestPlanItemInput{
		{ModuleID: f.feature, Branch: "feature/a", Mode: domain.TestPlanItemModePinned, BuildRecordID: other},
	}, nil, false); !errors.Is(err, domain.ErrTestPlanBuildNotFound) {
		t.Fatalf("expected build of another branch to be rejected, got %v", err)
	}
	if _, err := f.createPlanErr("A", []domain.TestPlanItemInput{follow(f.feature, "feature/none")}, nil, false); !errors.Is(err, domain.ErrTestPlanBuildNotFound) {
		t.Fatalf("expected a branch without builds to be rejected, got %v", err)
	}
	if _, err := f.pool.Exec(integrationCtx(), `update environments set allow_test_plans = false where code = $1`, f.environment); err != nil {
		t.Fatalf("disable test plans: %v", err)
	}
	if _, err := f.createPlanErr("A", nil, nil, false); !errors.Is(err, domain.ErrTestPlansDisabled) {
		t.Fatalf("expected disabled environment to be rejected, got %v", err)
	}
}
