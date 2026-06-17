package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"forge-server/internal/domain"

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
		record, err := r.createBuildRecord(ctx, newBuildRecordID(), input)
		if err == nil {
			return record, nil
		}
		if isUniqueViolation(err) {
			waitUntilNextSecond()
			continue
		}
		return domain.BuildRecord{}, err
	}
	return domain.BuildRecord{}, fmt.Errorf("insert build record: id conflict")
}

func (r *BuildRepository) createBuildRecord(ctx context.Context, id string, input domain.BuildRecordInput) (domain.BuildRecord, error) {
	record, err := scanBuildRecord(r.pool.QueryRow(ctx, `
		with selected_module as (
			select
				m.id,
				m.name,
				m.application_id,
				m.repository_provider,
				m.provider_repository_id,
				m.provider_installation_id,
				m.repository_full_name,
				m.repository_clone_url,
				m.default_branch
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
				status,
				trigger_type,
				trigger_id
			)
			select
				$1,
				m.id,
				e.code,
				e.storage_configuration_id,
				coalesce(nullif($4, ''), m.default_branch),
				'pending',
				$5,
				$6
			from selected_module m
			cross join selected_environment e
			returning *
		)
	`+buildRecordSelectSQL("inserted", `
		join selected_module m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join selected_environment e on e.code = br.environment_code
		join storage_configurations s on s.id = br.storage_configuration_id
	`), id, input.ModuleID, input.Environment, input.Branch, input.TriggerType, input.TriggerID))
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

func (r *BuildRepository) ListBuildRecords(ctx context.Context) ([]domain.BuildRecord, domain.BuildRecordSummary, error) {
	summary, err := r.buildRecordSummary(ctx)
	if err != nil {
		return nil, domain.BuildRecordSummary{}, err
	}

	rows, err := r.pool.Query(ctx, buildRecordSelectSQL("build_records", `
		join modules m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join environments e on e.code = br.environment_code
		join storage_configurations s on s.id = br.storage_configuration_id
		order by br.created_at desc, br.id desc
	`))
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
		join environments e on e.code = br.environment_code
		join storage_configurations s on s.id = br.storage_configuration_id
		where br.id = $1
	`), id))
}

func (r *BuildRepository) buildRecordSummary(ctx context.Context) (domain.BuildRecordSummary, error) {
	var summary domain.BuildRecordSummary
	if err := r.pool.QueryRow(ctx, `
		select
			count(*)::int,
			count(*) filter (where status = 'pending')::int,
			count(*) filter (where status = 'running')::int,
			count(*) filter (where status = 'succeeded')::int,
			count(*) filter (where status = 'failed')::int
		from build_records
	`).Scan(
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
				error_message = $4,
				finished_at = now(),
				updated_at = now()
			where id = $1
			returning br.*
		)
	`+buildRecordSelectSQL("updated", `
		join modules m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join environments e on e.code = br.environment_code
		join storage_configurations s on s.id = br.storage_configuration_id
	`), id, completion.ArtifactKey, completion.ArtifactURL, completion.ErrorMessage))
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
			m.repository_provider,
			m.provider_repository_id,
			m.provider_installation_id,
			m.repository_full_name,
			m.repository_clone_url,
			br.branch,
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
			br.status,
			br.trigger_type,
			br.trigger_id,
			br.error_message,
			br.worker_id,
			br.queued_at,
			br.started_at,
			br.finished_at,
			br.created_at,
			br.updated_at
		from ` + buildRecordSourceSQL + ` br
		` + extraJoinSQL
}

func scanBuildRecord(row buildRecordRow) (domain.BuildRecord, error) {
	var record domain.BuildRecord
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	if err := row.Scan(
		&record.ID,
		&record.ApplicationName,
		&record.ModuleID,
		&record.ModuleName,
		&record.Environment,
		&record.EnvironmentName,
		&record.RepositoryProvider,
		&record.RepositoryID,
		&record.RepositoryInstallationID,
		&record.RepositoryFullName,
		&record.RepositoryCloneURL,
		&record.Branch,
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
		&record.Status,
		&record.TriggerType,
		&record.TriggerID,
		&record.ErrorMessage,
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
	if startedAt.Valid {
		record.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		record.FinishedAt = &finishedAt.Time
	}
	return record, nil
}

func intervalString(duration time.Duration) string {
	return fmt.Sprintf("%f seconds", duration.Seconds())
}

func newBuildRecordID() string {
	return time.Now().Format("20060102150405")
}

func waitUntilNextSecond() {
	now := time.Now()
	time.Sleep(now.Truncate(time.Second).Add(time.Second).Sub(now))
}
