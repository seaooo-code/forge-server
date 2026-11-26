package postgres

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 这些测试需要一个已执行 migrations/000001_init_schema.sql 的空库，通过 FORGE_TEST_POSTGRES_DSN 指定；
// 未设置时跳过。测试会写入数据，不要指向开发库或线上库。
func newIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("FORGE_TEST_POSTGRES_DSN not set")
	}
	pool, err := pgxpool.New(integrationCtx(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedIntegrationModule(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	ctx := integrationCtx()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := "user-" + suffix
	moduleID := "module-" + suffix
	for _, statement := range []string{
		`insert into tenants (id, name, status, sort_order) select 'tnt-' || $2::text, 't', 'active', 1 where $1::text <> ''`,
		`insert into users (id, tenant_id, name) select $1::text, 'tnt-' || $2::text, 'tester' || $2::text`,
		`insert into storage_configurations (id, tenant_id, name, adapter, bucket, endpoint, region, owner_user_id, access_key_id, secret_access_key, status, sort_order)
			values ('storage-' || $2::text, 'tnt-' || $2, 's', 'S3', 'b', 'http://s3.example.com', 'r', $1, 'k', 's', 'healthy', 1)`,
		`insert into environments (id, tenant_id, code, name, status, tone, description, storage_configuration_id, sort_order)
			select 'envid-' || $2::text, 'tnt-' || $2, 'env-' || $2::text, 'e', 'healthy', 'blue', '', 'storage-' || $2::text, 1 where $1::text <> ''`,
		`insert into applications (id, tenant_id, slug, name, status, platform, owner_user_id, description, sort_order)
			values ('app-' || $2::text, 'tnt-' || $2, 'app-' || $2::text, 'a', 'running', 'rn', $1, '', 1)`,
		`insert into code_source_connections (id, tenant_id, provider, name, web_base_url, api_base_url, status, created_by_user_id)
			values ('conn-' || $2::text, 'tnt-' || $2, 'gitlab', 'c', 'https://git.example.com', 'https://git.example.com/api/v4', 'active', $1)`,
		`insert into modules (id, tenant_id, name, kind, application_id, owner_user_id, code_source_connection_id, external_repository_id, repository_full_name, sort_order)
			values ('module-' || $2::text, 'tnt-' || $2, 'm', 'Common', 'app-' || $2::text, $1, 'conn-' || $2::text, 'repo-' || $2, 'demo/repo', 1)`,
	} {
		if _, err := pool.Exec(ctx, statement, userID, suffix); err != nil {
			t.Fatalf("seed: %v\n%s", err, statement)
		}
	}
	return moduleID, "env-" + suffix
}

func TestIntegrationBuildNumberConcurrentAllocation(t *testing.T) {
	pool := newIntegrationPool(t)
	moduleID, environment := seedIntegrationModule(t, pool)
	repository := NewBuildRepository(pool)

	const workers = 24
	numbers := make([]int64, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			input := domain.BuildRecordInput{ModuleID: moduleID, Environment: environment, TriggerType: domain.BuildTriggerTypeManual}
			var record domain.BuildRecord
			// 一半走 webhook 幂等事务，验证两条路径共用同一套构建号分配。
			if index%2 == 0 {
				input.TriggerType = domain.BuildTriggerTypeWebhook
				input.Branch = "main"
				input.CommitSHA = fmt.Sprintf("%040d", index)
				record, _, errs[index] = repository.CreateBuildRecordUnlessActive(integrationCtx(), input)
			} else {
				record, errs[index] = repository.CreateBuildRecord(integrationCtx(), input)
			}
			numbers[index] = record.Number
		}(index)
	}
	wg.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", index, err)
		}
	}
	sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })
	for index, number := range numbers {
		if number != int64(index+1) {
			t.Fatalf("expected numbers 1..%d without gaps or duplicates, got %v", workers, numbers)
		}
	}
}

func TestIntegrationBuildAdapterLifecycle(t *testing.T) {
	pool := newIntegrationPool(t)
	moduleID, environment := seedIntegrationModule(t, pool)
	ctx := moduleTenantCtx(t, pool, moduleID)
	var userID string
	if err := pool.QueryRow(ctx, `select owner_user_id from modules where id = $1`, moduleID).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	adapters := NewBuildAdapterRepository(pool)
	adapter, err := adapters.CreateBuildAdapter(ctx, domain.BuildAdapterInput{
		Name:               "Node bundle",
		DockerfileTemplate: "FROM node\n",
		Files:              map[string]string{"build.sh": "echo {{ .Params.entry }}"},
		Parameters:         []domain.BuildAdapterParameter{{Key: "entry", Required: true}},
		UserID:             userID,
	})
	if err != nil {
		t.Fatalf("create adapter: %v", err)
	}
	if adapter.Version != 1 || adapter.Status != domain.BuildAdapterStatusActive || adapter.Files["build.sh"] == "" || len(adapter.Parameters) != 1 {
		t.Fatalf("unexpected adapter: %#v", adapter)
	}

	updated, err := adapters.UpdateBuildAdapter(ctx, adapter.ID, 1, domain.BuildAdapterInput{Name: "Node bundle v2", DockerfileTemplate: "FROM node:22\n", UserID: userID})
	if err != nil || updated.Version != 2 || updated.Name != "Node bundle v2" {
		t.Fatalf("unexpected update %#v, %v", updated, err)
	}
	if _, err := adapters.UpdateBuildAdapter(ctx, adapter.ID, 1, domain.BuildAdapterInput{Name: "stale", DockerfileTemplate: "FROM node\n", UserID: userID}); !errors.Is(err, domain.ErrBuildAdapterVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}

	modules := NewModuleRepository(pool)
	if _, err := pool.Exec(ctx, `update modules set build_mode = 'adapter', build_adapter_id = $2, build_adapter_params = '{"entry":"index.js"}' where id = $1`, moduleID, adapter.ID); err != nil {
		t.Fatalf("select adapter: %v", err)
	}
	module, err := modules.GetModule(ctx, moduleID)
	if err != nil || module.BuildConfig.BuildMode != domain.BuildModeAdapter || module.BuildConfig.BuildAdapterName != "Node bundle v2" || module.BuildConfig.BuildAdapterParams["entry"] != "index.js" {
		t.Fatalf("unexpected module build config %#v, %v", module.BuildConfig, err)
	}
	if _, err := pool.Exec(ctx, `update modules set build_mode = 'dockerfile' where id = $1`, moduleID); err == nil {
		t.Fatal("expected check constraint to reject dockerfile mode with adapter id")
	}
	if adapter, _ = adapters.GetBuildAdapter(ctx, adapter.ID); adapter.ModuleCount != 1 {
		t.Fatalf("expected module count 1, got %d", adapter.ModuleCount)
	}
	if _, err := pool.Exec(ctx, `delete from build_adapters where id = $1`, adapter.ID); err == nil {
		t.Fatal("expected referenced adapter delete to fail")
	}

	builds := NewBuildRepository(pool)
	record, err := builds.CreateBuildRecord(ctx, domain.BuildRecordInput{ModuleID: moduleID, Environment: environment, TriggerType: domain.BuildTriggerTypeManual})
	if err != nil || record.BuildMode != domain.BuildModeAdapter || record.ModuleBuildAdapterID != adapter.ID || record.ModuleBuildAdapterParams["entry"] != "index.js" {
		t.Fatalf("unexpected build record %#v, %v", record, err)
	}
	if err := builds.RecordBuildAdapterUsage(ctx, record.ID, domain.BuildAdapterUsage{AdapterID: adapter.ID, AdapterVersion: 2, RenderedDockerfileSHA256: "abc"}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	record, err = builds.GetBuildRecord(ctx, record.ID)
	if err != nil || record.BuildAdapterID != adapter.ID || record.BuildAdapterVersion == nil || *record.BuildAdapterVersion != 2 || record.RenderedDockerfileSHA256 != "abc" {
		t.Fatalf("unexpected usage on record %#v, %v", record, err)
	}

	disabled, err := adapters.SetBuildAdapterStatus(ctx, adapter.ID, domain.BuildAdapterStatusDisabled, userID)
	if err != nil || disabled.Status != domain.BuildAdapterStatusDisabled || disabled.Version != 2 {
		t.Fatalf("unexpected disable %#v, %v", disabled, err)
	}
	if _, err := builds.CreateBuildRecord(ctx, domain.BuildRecordInput{ModuleID: moduleID, Environment: environment, TriggerType: domain.BuildTriggerTypeManual}); !errors.Is(err, domain.ErrBuildAdapterDisabled) {
		t.Fatalf("expected disabled adapter to block manual build, got %v", err)
	}
	if _, _, err := builds.CreateBuildRecordUnlessActive(ctx, domain.BuildRecordInput{ModuleID: moduleID, Environment: environment, TriggerType: domain.BuildTriggerTypeWebhook, Branch: "main", CommitSHA: "deadbeef"}); !errors.Is(err, domain.ErrBuildAdapterDisabled) {
		t.Fatalf("expected disabled adapter to block webhook build, got %v", err)
	}

	list, err := adapters.ListBuildAdapters(ctx, domain.BuildAdapterFilters{Status: domain.BuildAdapterStatusDisabled})
	if err != nil || len(list) == 0 {
		t.Fatalf("expected disabled adapter in list, got %d, %v", len(list), err)
	}
}

// 同一仓库可以用不同适配器参数绑定多个 adapter 模块；dockerfile 模块仍按 Dockerfile 路径唯一。
func TestIntegrationModuleRepositoryUniqueOnlyForDockerfileMode(t *testing.T) {
	pool := newIntegrationPool(t)
	commonModuleID, _ := seedIntegrationModule(t, pool)
	ctx := moduleTenantCtx(t, pool, commonModuleID)

	var applicationID, ownerUserID, connectionID, repositoryID string
	if err := pool.QueryRow(ctx, `
		select application_id, owner_user_id, code_source_connection_id, external_repository_id
		from modules where id = $1
	`, commonModuleID).Scan(&applicationID, &ownerUserID, &connectionID, &repositoryID); err != nil {
		t.Fatal(err)
	}
	adapter, err := NewBuildAdapterRepository(pool).CreateBuildAdapter(ctx, domain.BuildAdapterInput{
		Name:               "Node bundle",
		DockerfileTemplate: "FROM node\n",
		Parameters:         []domain.BuildAdapterParameter{{Key: "entry"}},
		UserID:             ownerUserID,
	})
	if err != nil {
		t.Fatalf("create adapter: %v", err)
	}

	modules := NewModuleRepository(pool)
	featureInput := func(id string, config domain.ModuleBuildConfig) domain.ModuleInput {
		config.DockerfilePath = domain.DefaultModuleDockerfilePath
		config.BuildContext = domain.DefaultModuleBuildContext
		return domain.ModuleInput{
			ApplicationID:  applicationID,
			CommonModuleID: commonModuleID,
			ID:             id,
			Kind:           domain.ModuleKindFeature,
			Name:           id,
			OwnerUserID:    ownerUserID,
			Repository:     domain.ModuleRepository{ConnectionID: connectionID, ExternalRepositoryID: repositoryID, FullName: "demo/repo"},
			BuildConfig:    config,
		}
	}

	// 已种子的 common 模块是 dockerfile 模式、路径 Dockerfile；两个 adapter 模块用同一仓库、同一路径也不冲突。
	for _, item := range []struct{ id, entry string }{{commonModuleID + "-adapter-a", "common.js"}, {commonModuleID + "-adapter-b", "feature.js"}} {
		if _, err := modules.CreateModule(ctx, featureInput(item.id, domain.ModuleBuildConfig{
			BuildMode:          domain.BuildModeAdapter,
			BuildAdapterID:     adapter.ID,
			BuildAdapterParams: map[string]string{"entry": item.entry},
		})); err != nil {
			t.Fatalf("create adapter module %s: %v", item.id, err)
		}
	}

	_, err = modules.CreateModule(ctx, featureInput(commonModuleID+"-dockerfile", domain.ModuleBuildConfig{BuildMode: domain.BuildModeDockerfile}))
	if !errors.Is(err, domain.ErrModuleRepositoryConflict) {
		t.Fatalf("expected dockerfile module with same path to conflict, got %v", err)
	}

	// adapter 模块切回 dockerfile 模式时同样受唯一索引约束。
	_, err = modules.UpdateModule(ctx, commonModuleID+"-adapter-a", featureInput("", domain.ModuleBuildConfig{BuildMode: domain.BuildModeDockerfile}))
	if !errors.Is(err, domain.ErrModuleRepositoryConflict) {
		t.Fatalf("expected switching to dockerfile mode to conflict, got %v", err)
	}
}
