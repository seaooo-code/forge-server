package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"forge-server/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type BuildRepository struct {
	pool *pgxpool.Pool
}

func NewBuildRepository(pool *pgxpool.Pool) *BuildRepository {
	return &BuildRepository{pool: pool}
}

func (r *BuildRepository) CreateBuildRecord(ctx context.Context, input domain.BuildRecordInput) (domain.BuildRecord, error) {
	for attempt := 0; attempt < 3; attempt++ {
		record, err := r.createBuildRecordInTx(ctx, newBuildRecordID(), input)
		if err == nil {
			return record, nil
		}
		if isUniqueViolation(err) {
			continue
		}
		return domain.BuildRecord{}, err
	}
	return domain.BuildRecord{}, fmt.Errorf("insert build record: id conflict")
}

// createBuildRecordInTx 在独立事务里创建构建记录：构建号分配用的 advisory lock 是事务级的，必须包在事务里。
func (r *BuildRepository) createBuildRecordInTx(ctx context.Context, id string, input domain.BuildRecordInput) (domain.BuildRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.BuildRecord{}, fmt.Errorf("begin create build record: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	record, err := r.createBuildRecord(ctx, tx, id, input)
	if err != nil {
		return domain.BuildRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.BuildRecord{}, fmt.Errorf("commit create build record: %w", err)
	}
	return record, nil
}

// RecordBuildAdapterUsage 由 worker 在渲染适配器后写回所用适配器版本和渲染后 Dockerfile 的 sha256。
func (r *BuildRepository) RecordBuildAdapterUsage(ctx context.Context, id string, usage domain.BuildAdapterUsage) error {
	tag, err := r.pool.Exec(ctx, `
		update build_records set
			build_adapter_id = $2,
			build_adapter_version = $3,
			rendered_dockerfile_sha256 = $4,
			updated_at = now()
		where id = $1
	`, id, usage.AdapterID, usage.AdapterVersion, usage.RenderedDockerfileSHA256)
	if err != nil {
		return fmt.Errorf("record build adapter usage: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrBuildRecordNotFound
	}
	return nil
}

// buildDependencyRow 是 dependency_builds 的 JSON 形态。落库只写 moduleId、runtimeVersion、buildRecordId，
// 读取时 select 语句补上 moduleName 和 buildNumber。
type buildDependencyRow struct {
	ModuleID       string `json:"moduleId"`
	ModuleName     string `json:"moduleName,omitempty"`
	RuntimeVersion string `json:"runtimeVersion"`
	BuildRecordID  string `json:"buildRecordId"`
	BuildNumber    int64  `json:"buildNumber,omitempty"`
}

// RecordBuildDependencies 由 worker 在依赖产物注入完成后写回本次注入的依赖构建，重复执行时整体覆盖。
func (r *BuildRepository) RecordBuildDependencies(ctx context.Context, id string, dependencies []domain.BuildDependency) error {
	rows := make([]buildDependencyRow, 0, len(dependencies))
	for _, dependency := range dependencies {
		rows = append(rows, buildDependencyRow{
			ModuleID:       dependency.ModuleID,
			RuntimeVersion: dependency.RuntimeVersion,
			BuildRecordID:  dependency.BuildRecordID,
		})
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("encode build dependencies: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
		update build_records set
			dependency_builds = $2::jsonb,
			updated_at = now()
		where id = $1
	`, id, string(payload))
	if err != nil {
		return fmt.Errorf("record build dependencies: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrBuildRecordNotFound
	}
	return nil
}

// RecordBuildCIPipeline 把 gitlab_ci 构建创建的 pipeline 写回构建记录，只有构建仍由 workerID 持有时才写入，
// 构建已被接管或已结束时返回 ErrBuildLeaseLost。
func (r *BuildRepository) RecordBuildCIPipeline(ctx context.Context, id string, workerID string, pipelineID int64, pipelineURL string) error {
	tag, err := r.pool.Exec(ctx, `
		update build_records
		set ci_pipeline_id = $3, ci_pipeline_url = $4, updated_at = now()
		where id = $1 and worker_id = $2 and status = 'running'
	`, id, workerID, pipelineID, pipelineURL)
	if err != nil {
		return fmt.Errorf("record build ci pipeline: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrBuildLeaseLost
	}
	return nil
}

// ListActiveDefaultReleaseBuilds 返回模块在环境下默认渠道所有生效中的默认路由（route_type = 'default'、status = 'active'、channel = ”）
// 指向的构建，按 runtime_version 排序。唯一索引保证每个渠道、每个运行时版本最多一条生效默认路由。
// 渠道发布不参与依赖注入，见 docs/channels.md 第 5.2 节。
// 构建记录的存储配置取构建自己的 storage_configuration_id。
func (r *BuildRepository) ListActiveDefaultReleaseBuilds(ctx context.Context, moduleID string, environment string) ([]domain.BuildRecord, error) {
	rows, err := r.pool.Query(ctx, `
		with routed as (
			select distinct on (rr.runtime_version) br.*
			from release_routes rr
			join release_records rel on rel.id = rr.release_id
			join build_records br on br.id = rel.build_record_id
			join modules rm on rm.id = rr.module_id
			join environments re on re.id = rr.environment_id and re.tenant_id = rm.tenant_id
			where rr.module_id = $1
				and re.code = $2
				and rr.route_type = 'default'
				and rr.channel = ''
				and rr.status = 'active'
			order by rr.runtime_version, rr.updated_at desc
		)
	`+buildRecordSelectSQL("routed", `
		join modules m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join code_source_connections csc on csc.id = m.code_source_connection_id
		join environments e on e.id = br.environment_id
		join storage_configurations s on s.id = br.storage_configuration_id
		order by br.runtime_version
	`), moduleID, environment)
	if err != nil {
		return nil, fmt.Errorf("query active default release builds: %w", err)
	}
	defer rows.Close()

	records := make([]domain.BuildRecord, 0)
	for rows.Next() {
		record, err := scanBuildRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active default release builds: %w", err)
	}
	return records, nil
}

// CreateBuildRecordUnlessActive 用于 webhook 幂等创建构建：同一 (module_id, 环境, commit_sha)
// 已有 pending/running/succeeded 构建时不再创建，返回已有构建且 created=false。
// 并发重投通过事务级 advisory lock 串行化，锁在事务提交或回滚时自动释放。
func (r *BuildRepository) CreateBuildRecordUnlessActive(ctx context.Context, input domain.BuildRecordInput) (domain.BuildRecord, bool, error) {
	if input.CommitSHA == "" {
		return domain.BuildRecord{}, false, domain.ErrInvalidBuildInput
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.BuildRecord{}, false, fmt.Errorf("begin create build record unless active: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	if _, err := tx.Exec(ctx, `
		select pg_advisory_xact_lock(hashtextextended('build_records:commit:' || $1 || ':' || $2 || ':' || $3, 0))
	`, input.ModuleID, input.Environment, input.CommitSHA); err != nil {
		return domain.BuildRecord{}, false, fmt.Errorf("lock build record commit: %w", err)
	}

	var existingID string
	err = tx.QueryRow(ctx, `
		select br.id::text
		from build_records br
		join environments e on e.id = br.environment_id
		where br.module_id = $1
			and e.code = $2
			and br.commit_sha = $3
			and br.status in ('pending', 'running', 'succeeded')
		order by br.created_at desc
		limit 1
	`, input.ModuleID, input.Environment, input.CommitSHA).Scan(&existingID)
	switch {
	case err == nil:
		if err := tx.Commit(ctx); err != nil {
			return domain.BuildRecord{}, false, fmt.Errorf("commit build record lookup: %w", err)
		}
		record, err := r.GetBuildRecord(ctx, existingID)
		return record, false, err
	case !errors.Is(err, pgx.ErrNoRows):
		return domain.BuildRecord{}, false, fmt.Errorf("query active build record by commit: %w", err)
	}

	record, err := r.createBuildRecord(ctx, tx, newBuildRecordID(), input)
	if err != nil {
		return domain.BuildRecord{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.BuildRecord{}, false, fmt.Errorf("commit create build record unless active: %w", err)
	}
	return record, true, nil
}

// createBuildRecord 必须在事务内调用：
//  1. 模块使用已停用的构建适配器时拒绝创建（ErrBuildAdapterDisabled），覆盖手动与 webhook 构建；
//  2. 以模块为粒度取事务级 advisory lock，再用 max(number)+1 分配构建号。锁在提交后释放，
//     下一个事务的 insert 语句快照晚于上一个事务提交，能看到已分配的构建号；(module_id, number) 唯一约束兜底。
//
// 与 CreateBuildRecordUnlessActive 一起使用时，先取 commit 锁再取构建号锁，所有路径加锁顺序一致，不会死锁。
func (r *BuildRepository) createBuildRecord(ctx context.Context, tx pgx.Tx, id string, input domain.BuildRecordInput) (domain.BuildRecord, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return domain.BuildRecord{}, err
	}
	var buildMode domain.BuildMode
	var adapterStatus string
	err = tx.QueryRow(ctx, `
		select m.build_mode, coalesce(ba.status, '')
		from modules m
		left join build_adapters ba on ba.id = m.build_adapter_id
		where m.id = $1
	`, input.ModuleID).Scan(&buildMode, &adapterStatus)
	switch {
	case err == nil:
		if buildMode == domain.BuildModeAdapter && adapterStatus != string(domain.BuildAdapterStatusActive) {
			return domain.BuildRecord{}, domain.ErrBuildAdapterDisabled
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return domain.BuildRecord{}, fmt.Errorf("query module build mode: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		select pg_advisory_xact_lock(hashtextextended('build_records:number:' || $1, 0))
	`, input.ModuleID); err != nil {
		return domain.BuildRecord{}, fmt.Errorf("lock build record number: %w", err)
	}

	record, err := scanBuildRecord(tx.QueryRow(ctx, `
		with selected_module as (
			select m.*
			from modules m
			where m.id = $2 and ($9::text = '' or m.tenant_id = $9)
		),
		selected_environment as (
			-- 环境编码只在租户内唯一，按模块所属租户解析。
			select e.id, e.code, e.name, e.storage_configuration_id
			from environments e
			join selected_module m on m.tenant_id = e.tenant_id
			where e.code = $3
		),
		inserted as (
			insert into build_records (
				id,
				module_id,
				number,
				environment_id,
				storage_configuration_id,
				branch,
				commit_sha,
				status,
				trigger_type,
				trigger_id,
				created_by_user_id
			)
			select
				$1,
				m.id,
				(select coalesce(max(existing.number), 0) + 1 from build_records existing where existing.module_id = m.id),
				e.id,
				e.storage_configuration_id,
				coalesce(nullif($4, ''), m.default_branch),
				$7,
				'pending',
				$5,
				$6,
				-- 创建人允许为空：用户不存在时写 null，避免外键错误被误报为模块不存在。
				(select u.id from users u where u.id = nullif($8, ''))
			from selected_module m
			cross join selected_environment e
			returning *
		)
	`+buildRecordSelectSQL("inserted", `
		join selected_module m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join code_source_connections csc on csc.id = m.code_source_connection_id
		join selected_environment e on e.id = br.environment_id
		join storage_configurations s on s.id = br.storage_configuration_id
	`), id, input.ModuleID, input.Environment, input.Branch, input.TriggerType, input.TriggerID, input.CommitSHA, input.CreatedByUserID, tenantID))
	if err != nil {
		if errors.Is(err, domain.ErrBuildRecordNotFound) {
			return domain.BuildRecord{}, r.createBuildRecordMissingDependency(ctx, input)
		}
		if isForeignKeyViolation(err) {
			return domain.BuildRecord{}, domain.ErrModuleNotFound
		}
		return domain.BuildRecord{}, err
	}
	return record, nil
}

func (r *BuildRepository) createBuildRecordMissingDependency(ctx context.Context, input domain.BuildRecordInput) error {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return err
	}
	var moduleExists bool
	if err := r.pool.QueryRow(ctx, `
		select exists(select 1 from modules where id = $1 and ($2::text = '' or tenant_id = $2))
	`, input.ModuleID, tenantID).Scan(&moduleExists); err != nil {
		return fmt.Errorf("query build module dependency: %w", err)
	}
	if !moduleExists {
		return domain.ErrModuleNotFound
	}

	var environmentExists bool
	if err := r.pool.QueryRow(ctx, `
		select exists(
			select 1 from environments e join modules m on m.tenant_id = e.tenant_id
			where m.id = $1 and e.code = $2
		)
	`, input.ModuleID, input.Environment).Scan(&environmentExists); err != nil {
		return fmt.Errorf("query build environment dependency: %w", err)
	}
	if !environmentExists {
		return domain.ErrEnvironmentNotFound
	}

	return domain.ErrInvalidBuildInput
}

func (r *BuildRepository) ListBuildRecords(ctx context.Context, filters domain.BuildRecordFilters) ([]domain.BuildRecord, domain.BuildRecordSummary, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return nil, domain.BuildRecordSummary{}, err
	}
	summary, err := r.buildRecordSummary(ctx, tenantID, filters)
	if err != nil {
		return nil, domain.BuildRecordSummary{}, err
	}

	rows, err := r.pool.Query(ctx, buildRecordSelectSQL("build_records", `
		join modules m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join code_source_connections csc on csc.id = m.code_source_connection_id
		join environments e on e.id = br.environment_id
		join storage_configurations s on s.id = br.storage_configuration_id
		where ($1 = '' or br.status = $1)
			and ($2 = '' or br.module_id = $2)
			and ($3 = '' or e.code = $3)
			and ($4 = '' or br.trigger_type = $4)
			and ($5 = '' or m.application_id = $5)
			and ($8::text = '' or m.tenant_id = $8)
		order by br.created_at desc, br.id desc
		limit nullif($6, 0) offset $7
	`), string(filters.Status), filters.ModuleID, filters.Environment, string(filters.TriggerType), filters.ApplicationID, filters.Pagination.Limit(), filters.Pagination.Offset(), tenantID)
	if err != nil {
		return nil, domain.BuildRecordSummary{}, fmt.Errorf("query build records: %w", err)
	}
	defer rows.Close()

	records := make([]domain.BuildRecord, 0)
	for rows.Next() {
		record, err := scanBuildRecord(rows)
		if err != nil {
			return nil, domain.BuildRecordSummary{}, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, domain.BuildRecordSummary{}, fmt.Errorf("iterate build records: %w", err)
	}
	return records, summary, nil
}

func (r *BuildRepository) GetBuildRecord(ctx context.Context, id string) (domain.BuildRecord, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return domain.BuildRecord{}, err
	}
	return scanBuildRecord(r.pool.QueryRow(ctx, buildRecordSelectSQL("build_records", `
		join modules m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join code_source_connections csc on csc.id = m.code_source_connection_id
		join environments e on e.id = br.environment_id
		join storage_configurations s on s.id = br.storage_configuration_id
		where br.id = $1 and ($2::text = '' or m.tenant_id = $2)
	`), id, tenantID))
}

func (r *BuildRepository) buildRecordSummary(ctx context.Context, tenantID string, filters domain.BuildRecordFilters) (domain.BuildRecordSummary, error) {
	var summary domain.BuildRecordSummary
	if err := r.pool.QueryRow(ctx, `
		select
			count(*)::int,
			count(*) filter (where br.status = 'pending')::int,
			count(*) filter (where br.status = 'running')::int,
			count(*) filter (where br.status = 'succeeded')::int,
			count(*) filter (where br.status = 'failed')::int
		from build_records br
		join modules m on m.id = br.module_id
		join environments e on e.id = br.environment_id
		where ($1 = '' or br.status = $1)
			and ($2 = '' or br.module_id = $2)
			and ($3 = '' or e.code = $3)
			and ($4 = '' or br.trigger_type = $4)
			and ($5 = '' or m.application_id = $5)
			and ($6::text = '' or m.tenant_id = $6)
	`, string(filters.Status), filters.ModuleID, filters.Environment, string(filters.TriggerType), filters.ApplicationID, tenantID).Scan(
		&summary.Total,
		&summary.Pending,
		&summary.Running,
		&summary.Succeeded,
		&summary.Failed,
	); err != nil {
		return domain.BuildRecordSummary{}, fmt.Errorf("query build record summary: %w", err)
	}
	return summary, nil
}

func (r *BuildRepository) AcquireNextBuildRecord(ctx context.Context, workerID string, leaseDuration time.Duration, modes []domain.BuildMode) (domain.BuildRecord, error) {
	modeValues := make([]string, 0, len(modes))
	for _, mode := range modes {
		modeValues = append(modeValues, string(mode))
	}
	record, err := scanBuildRecord(r.pool.QueryRow(ctx, `
		with picked as (
			select br.id
			from build_records br
			join modules m on m.id = br.module_id
			where (br.status = 'pending'
			   or (br.status = 'running' and br.lease_expires_at < now()))
			  and m.build_mode = any($3::text[])
			order by br.queued_at asc, br.created_at asc
			for update of br skip locked
			limit 1
		),
		updated as (
			update build_records br
			set
				status = 'running',
				worker_id = $1,
				lease_expires_at = now() + $2::interval,
				started_at = coalesce(br.started_at, now()),
				updated_at = now()
			from picked
			where br.id = picked.id
			returning br.*
		)
	`+buildRecordSelectSQL("updated", `
		join modules m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join code_source_connections csc on csc.id = m.code_source_connection_id
		join environments e on e.id = br.environment_id
		join storage_configurations s on s.id = br.storage_configuration_id
	`), workerID, intervalString(leaseDuration), modeValues))
	if err != nil {
		if errors.Is(err, domain.ErrBuildRecordNotFound) {
			return domain.BuildRecord{}, domain.ErrNoBuildRecord
		}
		return domain.BuildRecord{}, err
	}
	return record, nil
}

// CompleteBuildRecord 只在构建仍由 workerID 持有（status = 'running' 且 worker_id 一致）时写入结果，否则返回 ErrBuildLeaseLost，
// 避免租约过期被接管后，原 worker 覆盖新一次执行的结果。
func (r *BuildRepository) CompleteBuildRecord(ctx context.Context, id string, workerID string, completion domain.BuildRecordCompletion) (domain.BuildRecord, error) {
	record, err := scanBuildRecord(r.pool.QueryRow(ctx, `
		with updated as (
			update build_records br
			set
				status = 'succeeded',
				lease_expires_at = null,
				artifact_key = $2,
				artifact_url = $3,
				manifest_key = $4,
				file_count = $5,
				total_size_bytes = $6,
				error_message = $7,
				commit_sha = coalesce(nullif($8, ''), br.commit_sha),
				commit_title = $17,
				commit_author_name = $18,
				committed_at = $19,
				runtime_version = $9,
				platforms = $10,
				provided_capabilities = $12::jsonb,
				required_capabilities = $13::jsonb,
				signing_key_id = $14,
				signed_payload = $15,
				signature = $16,
				finished_at = now(),
				updated_at = now()
			where id = $1 and worker_id = $11 and status = 'running'
			returning br.*
		)
	`+buildRecordSelectSQL("updated", `
		join modules m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join code_source_connections csc on csc.id = m.code_source_connection_id
		join environments e on e.id = br.environment_id
		join storage_configurations s on s.id = br.storage_configuration_id
	`), id, completion.ArtifactKey, completion.ArtifactURL, completion.ManifestKey, completion.FileCount, completion.TotalSizeBytes, completion.ErrorMessage, completion.CommitSHA, completion.RuntimeVersion, buildPlatforms(completion.Platforms), workerID,
		capabilitiesJSON(completion.ProvidedCapabilities), capabilitiesJSON(completion.RequiredCapabilities),
		completion.Signature.KeyID, completion.Signature.Payload, completion.Signature.Signature,
		completion.CommitTitle, completion.CommitAuthorName, completion.CommittedAt))
	if errors.Is(err, domain.ErrBuildRecordNotFound) {
		return domain.BuildRecord{}, domain.ErrBuildLeaseLost
	}
	if err != nil {
		return domain.BuildRecord{}, err
	}
	return record, nil
}

// FailBuildRecord 与 CompleteBuildRecord 一样按 workerID 做归属校验。
func (r *BuildRepository) FailBuildRecord(ctx context.Context, id string, workerID string, message string) (domain.BuildRecord, error) {
	record, err := scanBuildRecord(r.pool.QueryRow(ctx, `
		with updated as (
			update build_records br
			set
				status = 'failed',
				lease_expires_at = null,
				error_message = $2,
				finished_at = now(),
				updated_at = now()
			where id = $1 and worker_id = $3 and status = 'running'
			returning br.*
		)
	`+buildRecordSelectSQL("updated", `
		join modules m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join code_source_connections csc on csc.id = m.code_source_connection_id
		join environments e on e.id = br.environment_id
		join storage_configurations s on s.id = br.storage_configuration_id
	`), id, message, workerID))
	if errors.Is(err, domain.ErrBuildRecordNotFound) {
		return domain.BuildRecord{}, domain.ErrBuildLeaseLost
	}
	if err != nil {
		return domain.BuildRecord{}, err
	}
	return record, nil
}

// RenewBuildLease 把仍由 workerID 持有的构建租约延长到 now() + leaseDuration。
// 构建已被其他 worker 接管或已结束时返回 ErrBuildLeaseLost，worker 据此停止执行。
func (r *BuildRepository) RenewBuildLease(ctx context.Context, id string, workerID string, leaseDuration time.Duration) error {
	tag, err := r.pool.Exec(ctx, `
		update build_records
		set lease_expires_at = now() + $3::interval, updated_at = now()
		where id = $1 and worker_id = $2 and status = 'running'
	`, id, workerID, intervalString(leaseDuration))
	if err != nil {
		return fmt.Errorf("renew build lease: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrBuildLeaseLost
	}
	return nil
}

// RequeueBuildRecord 把仍由 workerID 持有的构建放回队列（worker 退出时中断的构建）。queued_at 不变，所以它仍排在队首。
func (r *BuildRepository) RequeueBuildRecord(ctx context.Context, id string, workerID string) error {
	tag, err := r.pool.Exec(ctx, `
		update build_records
		set status = 'pending', worker_id = '', lease_expires_at = null, started_at = null, updated_at = now()
		where id = $1 and worker_id = $2 and status = 'running'
	`, id, workerID)
	if err != nil {
		return fmt.Errorf("requeue build record: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrBuildLeaseLost
	}
	return nil
}

// GetBuildQueueStats 统计等待中和执行中的构建。租约过期的 running 构建会被下一个 worker 重新领取，计入等待。
// 并发槽位的 worker_id 形如 {workerID}#{序号}，统计 worker 数时按 # 前的部分去重。
func (r *BuildRepository) GetBuildQueueStats(ctx context.Context) (domain.BuildQueueStats, error) {
	var stats domain.BuildQueueStats
	err := r.pool.QueryRow(ctx, `
		with queue as (
			select
				worker_id,
				queued_at,
				status = 'pending' or lease_expires_at < now() as waiting
			from build_records
			where status in ('pending', 'running')
		)
		select
			count(*) filter (where waiting),
			coalesce(extract(epoch from now() - min(queued_at) filter (where waiting)), 0)::float8,
			count(*) filter (where not waiting),
			count(distinct split_part(worker_id, '#', 1)) filter (where not waiting)
		from queue
	`).Scan(&stats.Waiting, &stats.OldestWaitSeconds, &stats.Running, &stats.ActiveWorkers)
	if err != nil {
		return domain.BuildQueueStats{}, fmt.Errorf("query build queue stats: %w", err)
	}
	return stats, nil
}

type buildRecordRow interface {
	Scan(dest ...any) error
}

func buildRecordSelectSQL(buildRecordSourceSQL string, extraJoinSQL string) string {
	return `
		select
			br.id,
			br.number,
			a.slug as application_slug,
			a.name as application_name,
			br.module_id,
			m.name as module_name,
			m.kind as module_kind,
			coalesce(m.common_module_id, '') as common_module_id,
			e.code as environment_code,
			e.name as environment_name,
			csc.provider as code_source_provider,
			m.code_source_connection_id,
			m.external_repository_id,
			m.repository_full_name,
			m.repository_web_url,
			m.repository_clone_url,
			m.dockerfile_path,
			m.build_context,
			m.build_mode,
			coalesce(m.build_adapter_id, ''),
			m.build_adapter_params,
			m.ci_job,
			m.ci_artifact_path,
			coalesce(br.ci_pipeline_id, 0),
			br.ci_pipeline_url,
			coalesce(br.build_adapter_id, ''),
			br.build_adapter_version,
			br.rendered_dockerfile_sha256,
			-- 依赖构建只落库 id，构建号和模块名在读取时关联，依赖构建或模块不存在时为空。
			coalesce((
				select jsonb_agg(jsonb_build_object(
					'moduleId', dependency.item->>'moduleId',
					'moduleName', coalesce(dependency_module.name, ''),
					'runtimeVersion', dependency.item->>'runtimeVersion',
					'buildRecordId', dependency.item->>'buildRecordId',
					'buildNumber', coalesce(dependency_build.number, 0)
				) order by dependency.position)
				from jsonb_array_elements(br.dependency_builds) with ordinality as dependency(item, position)
				left join build_records dependency_build on dependency_build.id = (dependency.item->>'buildRecordId')::uuid
				left join modules dependency_module on dependency_module.id = dependency.item->>'moduleId'
			), '[]'::jsonb) as dependency_builds,
			br.branch,
			br.commit_sha,
			br.commit_title,
			br.commit_author_name,
			br.committed_at,
			br.runtime_version,
			br.platforms,
			br.provided_capabilities,
			br.required_capabilities,
			m.default_branch,
			br.storage_configuration_id,
			s.name as storage_configuration_name,
			s.bucket as storage_bucket,
			s.endpoint as storage_endpoint,
			s.region as storage_region,
			s.public_base_url as storage_public_base_url,
			s.access_key_id as storage_access_key_id,
			s.secret_access_key as storage_secret_access_key,
			s.force_path_style as storage_force_path_style,
			br.artifact_key,
			br.artifact_url,
			br.manifest_key,
			br.signing_key_id,
			br.signed_payload,
			br.signature,
			br.file_count,
			br.total_size_bytes,
			br.status,
			br.trigger_type,
			br.trigger_id,
			br.error_message,
			coalesce(br.created_by_user_id, ''),
			coalesce(cu.name, ''),
			coalesce(cu.avatar_url, ''),
			br.worker_id,
			br.queued_at,
			br.started_at,
			br.finished_at,
			br.created_at,
			br.updated_at
		from ` + buildRecordSourceSQL + ` br
		left join users cu on cu.id = br.created_by_user_id
		` + extraJoinSQL
}

func scanBuildRecord(row buildRecordRow) (domain.BuildRecord, error) {
	var record domain.BuildRecord
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	var committedAt sql.NullTime
	var createdByName, createdByAvatarURL string
	var moduleAdapterParams []byte
	var dependencyBuilds []byte
	var providedCapabilities, requiredCapabilities []byte
	if err := row.Scan(
		&record.ID,
		&record.Number,
		&record.ApplicationSlug,
		&record.ApplicationName,
		&record.ModuleID,
		&record.ModuleName,
		&record.ModuleKind,
		&record.CommonModuleID,
		&record.Environment,
		&record.EnvironmentName,
		&record.CodeSourceProvider,
		&record.Repository.ConnectionID,
		&record.Repository.ExternalRepositoryID,
		&record.Repository.FullName,
		&record.Repository.WebURL,
		&record.Repository.CloneURL,
		&record.DockerfilePath,
		&record.BuildContext,
		&record.BuildMode,
		&record.ModuleBuildAdapterID,
		&moduleAdapterParams,
		&record.CIJob,
		&record.CIArtifactPath,
		&record.CIPipelineID,
		&record.CIPipelineURL,
		&record.BuildAdapterID,
		&record.BuildAdapterVersion,
		&record.RenderedDockerfileSHA256,
		&dependencyBuilds,
		&record.Branch,
		&record.CommitSHA,
		&record.CommitTitle,
		&record.CommitAuthorName,
		&committedAt,
		&record.RuntimeVersion,
		&record.Platforms,
		&providedCapabilities,
		&requiredCapabilities,
		&record.DefaultBranch,
		&record.StorageConfigurationID,
		&record.StorageConfigurationName,
		&record.StorageBucket,
		&record.StorageEndpoint,
		&record.StorageRegion,
		&record.StoragePublicBaseURL,
		&record.StorageAccessKeyID,
		&record.StorageSecretAccessKey,
		&record.StorageForcePathStyle,
		&record.ArtifactKey,
		&record.ArtifactURL,
		&record.ManifestKey,
		&record.Signature.KeyID,
		&record.Signature.Payload,
		&record.Signature.Signature,
		&record.FileCount,
		&record.TotalSizeBytes,
		&record.Status,
		&record.TriggerType,
		&record.TriggerID,
		&record.ErrorMessage,
		&record.CreatedByUserID,
		&createdByName,
		&createdByAvatarURL,
		&record.WorkerID,
		&record.QueuedAt,
		&startedAt,
		&finishedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.BuildRecord{}, domain.ErrBuildRecordNotFound
		}
		return domain.BuildRecord{}, fmt.Errorf("scan build record: %w", err)
	}
	record.Repository.DefaultBranch = record.DefaultBranch
	if err := json.Unmarshal(providedCapabilities, &record.ProvidedCapabilities); err != nil {
		return domain.BuildRecord{}, fmt.Errorf("decode provided capabilities: %w", err)
	}
	if err := json.Unmarshal(requiredCapabilities, &record.RequiredCapabilities); err != nil {
		return domain.BuildRecord{}, fmt.Errorf("decode required capabilities: %w", err)
	}
	record.ModuleBuildAdapterParams = map[string]string{}
	if err := json.Unmarshal(moduleAdapterParams, &record.ModuleBuildAdapterParams); err != nil {
		return domain.BuildRecord{}, fmt.Errorf("decode module build adapter params: %w", err)
	}
	var dependencies []buildDependencyRow
	if err := json.Unmarshal(dependencyBuilds, &dependencies); err != nil {
		return domain.BuildRecord{}, fmt.Errorf("decode build dependencies: %w", err)
	}
	record.DependencyBuilds = make([]domain.BuildDependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		record.DependencyBuilds = append(record.DependencyBuilds, domain.BuildDependency{
			ModuleID:       dependency.ModuleID,
			ModuleName:     dependency.ModuleName,
			RuntimeVersion: dependency.RuntimeVersion,
			BuildRecordID:  dependency.BuildRecordID,
			BuildNumber:    dependency.BuildNumber,
		})
	}
	if record.CreatedByUserID != "" {
		record.CreatedBy = &domain.UserSummary{ID: record.CreatedByUserID, Name: createdByName, AvatarURL: createdByAvatarURL}
	}
	if startedAt.Valid {
		record.StartedAt = &startedAt.Time
	}
	if committedAt.Valid {
		record.CommittedAt = &committedAt.Time
	}
	if finishedAt.Valid {
		record.FinishedAt = &finishedAt.Time
	}
	return record, nil
}

func buildPlatforms(platforms []string) []string {
	if platforms == nil {
		return []string{}
	}
	return platforms
}

func intervalString(duration time.Duration) string {
	return fmt.Sprintf("%f seconds", duration.Seconds())
}

func newBuildRecordID() string {
	return uuid.NewString()
}

// capabilitiesJSON 把能力声明编码成 jsonb 参数，nil 写成空对象，和列默认值一致。
func capabilitiesJSON(capabilities domain.Capabilities) []byte {
	if capabilities == nil {
		return []byte("{}")
	}
	encoded, _ := json.Marshal(capabilities)
	return encoded
}
