package postgres

import (
	"context"
	"database/sql"
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
		record, err := r.createBuildRecord(ctx, r.pool, newBuildRecordID(), input)
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

// CreateBuildRecordUnlessActive 用于 webhook 幂等创建构建：同一 (module_id, environment_code, commit_sha)
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
		select id::text
		from build_records
		where module_id = $1
			and environment_code = $2
			and commit_sha = $3
			and status in ('pending', 'running', 'succeeded')
		order by created_at desc
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

type buildRecordQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (r *BuildRepository) createBuildRecord(ctx context.Context, querier buildRecordQuerier, id string, input domain.BuildRecordInput) (domain.BuildRecord, error) {
	record, err := scanBuildRecord(querier.QueryRow(ctx, `
		with selected_module as (
			select m.*
			from modules m
			where m.id = $2
		),
		selected_environment as (
			select code, name, storage_configuration_id
			from environments
			where code = $3
		),
		inserted as (
			insert into build_records (
				id,
				module_id,
				environment_code,
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
				e.code,
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
		join selected_environment e on e.code = br.environment_code
		join storage_configurations s on s.id = br.storage_configuration_id
	`), id, input.ModuleID, input.Environment, input.Branch, input.TriggerType, input.TriggerID, input.CommitSHA, input.CreatedByUserID))
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
	var moduleExists bool
	if err := r.pool.QueryRow(ctx, `
		select exists(select 1 from modules where id = $1)
	`, input.ModuleID).Scan(&moduleExists); err != nil {
		return fmt.Errorf("query build module dependency: %w", err)
	}
	if !moduleExists {
		return domain.ErrModuleNotFound
	}

	var environmentExists bool
	if err := r.pool.QueryRow(ctx, `
		select exists(select 1 from environments where code = $1)
	`, input.Environment).Scan(&environmentExists); err != nil {
		return fmt.Errorf("query build environment dependency: %w", err)
	}
	if !environmentExists {
		return domain.ErrEnvironmentNotFound
	}

	return domain.ErrInvalidBuildInput
}

func (r *BuildRepository) ListBuildRecords(ctx context.Context, filters domain.BuildRecordFilters) ([]domain.BuildRecord, domain.BuildRecordSummary, error) {
	summary, err := r.buildRecordSummary(ctx, filters)
	if err != nil {
		return nil, domain.BuildRecordSummary{}, err
	}

	rows, err := r.pool.Query(ctx, buildRecordSelectSQL("build_records", `
		join modules m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join code_source_connections csc on csc.id = m.code_source_connection_id
		join environments e on e.code = br.environment_code
		join storage_configurations s on s.id = br.storage_configuration_id
		where ($1 = '' or br.status = $1)
			and ($2 = '' or br.module_id = $2)
			and ($3 = '' or br.environment_code = $3)
				and ($4 = '' or br.trigger_type = $4)
				and ($5 = '' or m.application_id = $5)
			order by br.created_at desc, br.id desc
			limit nullif($6, 0) offset $7
		`), string(filters.Status), filters.ModuleID, filters.Environment, string(filters.TriggerType), filters.ApplicationID, filters.Pagination.Limit(), filters.Pagination.Offset())
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
	return scanBuildRecord(r.pool.QueryRow(ctx, buildRecordSelectSQL("build_records", `
		join modules m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join code_source_connections csc on csc.id = m.code_source_connection_id
		join environments e on e.code = br.environment_code
		join storage_configurations s on s.id = br.storage_configuration_id
		where br.id = $1
	`), id))
}

func (r *BuildRepository) buildRecordSummary(ctx context.Context, filters domain.BuildRecordFilters) (domain.BuildRecordSummary, error) {
	var summary domain.BuildRecordSummary
	if err := r.pool.QueryRow(ctx, `
		select
			count(*)::int,
			count(*) filter (where status = 'pending')::int,
			count(*) filter (where status = 'running')::int,
			count(*) filter (where status = 'succeeded')::int,
			count(*) filter (where status = 'failed')::int
		from build_records br
		join modules m on m.id = br.module_id
		where ($1 = '' or br.status = $1)
			and ($2 = '' or br.module_id = $2)
			and ($3 = '' or br.environment_code = $3)
			and ($4 = '' or br.trigger_type = $4)
			and ($5 = '' or m.application_id = $5)
	`, string(filters.Status), filters.ModuleID, filters.Environment, string(filters.TriggerType), filters.ApplicationID).Scan(
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

func (r *BuildRepository) AcquireNextBuildRecord(ctx context.Context, workerID string, leaseDuration time.Duration) (domain.BuildRecord, error) {
	record, err := scanBuildRecord(r.pool.QueryRow(ctx, `
		with picked as (
			select id
			from build_records
			where status = 'pending'
			   or (status = 'running' and lease_expires_at < now())
			order by queued_at asc, created_at asc
			for update skip locked
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
		join environments e on e.code = br.environment_code
		join storage_configurations s on s.id = br.storage_configuration_id
	`), workerID, intervalString(leaseDuration)))
	if err != nil {
		if errors.Is(err, domain.ErrBuildRecordNotFound) {
			return domain.BuildRecord{}, domain.ErrNoBuildRecord
		}
		return domain.BuildRecord{}, err
	}
	return record, nil
}

func (r *BuildRepository) CompleteBuildRecord(ctx context.Context, id string, completion domain.BuildRecordCompletion) (domain.BuildRecord, error) {
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
				runtime_version = $9,
				platforms = $10,
				finished_at = now(),
				updated_at = now()
			where id = $1
			returning br.*
		)
	`+buildRecordSelectSQL("updated", `
		join modules m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join code_source_connections csc on csc.id = m.code_source_connection_id
		join environments e on e.code = br.environment_code
		join storage_configurations s on s.id = br.storage_configuration_id
	`), id, completion.ArtifactKey, completion.ArtifactURL, completion.ManifestKey, completion.FileCount, completion.TotalSizeBytes, completion.ErrorMessage, completion.CommitSHA, completion.RuntimeVersion, buildPlatforms(completion.Platforms)))
	if err != nil {
		return domain.BuildRecord{}, err
	}
	return record, nil
}

func (r *BuildRepository) FailBuildRecord(ctx context.Context, id string, message string) (domain.BuildRecord, error) {
	record, err := scanBuildRecord(r.pool.QueryRow(ctx, `
		with updated as (
			update build_records br
			set
				status = 'failed',
				lease_expires_at = null,
				error_message = $2,
				finished_at = now(),
				updated_at = now()
			where id = $1
			returning br.*
		)
	`+buildRecordSelectSQL("updated", `
		join modules m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join code_source_connections csc on csc.id = m.code_source_connection_id
		join environments e on e.code = br.environment_code
		join storage_configurations s on s.id = br.storage_configuration_id
	`), id, message))
	if err != nil {
		return domain.BuildRecord{}, err
	}
	return record, nil
}

type buildRecordRow interface {
	Scan(dest ...any) error
}

func buildRecordSelectSQL(buildRecordSourceSQL string, extraJoinSQL string) string {
	return `
		select
			br.id,
			a.name as application_name,
			br.module_id,
			m.name as module_name,
			br.environment_code,
			e.name as environment_name,
			csc.provider as code_source_provider,
			m.code_source_connection_id,
			m.external_repository_id,
			m.repository_full_name,
			m.repository_web_url,
			m.repository_clone_url,
			m.dockerfile_path,
			m.build_context,
			br.branch,
			br.commit_sha,
			br.runtime_version,
			br.platforms,
			m.default_branch,
			br.storage_configuration_id,
			s.name as storage_configuration_name,
			s.bucket as storage_bucket,
			s.endpoint as storage_endpoint,
			s.region as storage_region,
			s.public_base_url as storage_public_base_url,
			s.access_key_id as storage_access_key_id,
			s.secret_access_key as storage_secret_access_key,
			br.artifact_key,
			br.artifact_url,
			br.manifest_key,
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
	var createdByName, createdByAvatarURL string
	if err := row.Scan(
		&record.ID,
		&record.ApplicationName,
		&record.ModuleID,
		&record.ModuleName,
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
		&record.Branch,
		&record.CommitSHA,
		&record.RuntimeVersion,
		&record.Platforms,
		&record.DefaultBranch,
		&record.StorageConfigurationID,
		&record.StorageConfigurationName,
		&record.StorageBucket,
		&record.StorageEndpoint,
		&record.StorageRegion,
		&record.StoragePublicBaseURL,
		&record.StorageAccessKeyID,
		&record.StorageSecretAccessKey,
		&record.ArtifactKey,
		&record.ArtifactURL,
		&record.ManifestKey,
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
	if record.CreatedByUserID != "" {
		record.CreatedBy = &domain.UserSummary{ID: record.CreatedByUserID, Name: createdByName, AvatarURL: createdByAvatarURL}
	}
	if startedAt.Valid {
		record.StartedAt = &startedAt.Time
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
