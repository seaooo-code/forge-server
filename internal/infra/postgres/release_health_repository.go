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

// ReleaseHealthRepository 保存启动结果统计、健康策略和判定结果，规则见 docs/release-health.md。
type ReleaseHealthRepository struct {
	pool *pgxpool.Pool
}

func NewReleaseHealthRepository(pool *pgxpool.Pool) *ReleaseHealthRepository {
	return &ReleaseHealthRepository{pool: pool}
}

// ApplicationIDForEnvironment 返回绑定了该环境的应用 id，应用不存在或没有绑定该环境时返回 ErrArtifactNotFound。
func (r *ReleaseHealthRepository) ApplicationIDForEnvironment(ctx context.Context, applicationSlug string, environment string) (string, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return "", err
	}
	var id string
	err = r.pool.QueryRow(ctx, `
		select a.id
		from applications a
		join application_environment_bindings b on b.application_id = a.id
		join environments e on e.id = b.environment_id
		where a.slug = $1 and e.code = $2 and ($3::text = '' or a.tenant_id = $3)
	`, applicationSlug, environment, tenantID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrArtifactNotFound
	}
	if err != nil {
		return "", fmt.Errorf("query application environment: %w", err)
	}
	return id, nil
}

// IncrementReleaseHealthBuckets 用一条 upsert 批量累加，多个 API 实例并发写入时由主键冲突合并。
// 同一批里的维度不能重复（Postgres 不允许一条语句两次更新同一行），服务层从 map 生成批次，天然满足。
func (r *ReleaseHealthRepository) IncrementReleaseHealthBuckets(ctx context.Context, increments []domain.ReleaseHealthBucketIncrement) error {
	if len(increments) == 0 {
		return nil
	}
	buckets := make([]time.Time, 0, len(increments))
	columns := make([][]string, 9)
	users := make([]int64, 0, len(increments))
	events := make([]int64, 0, len(increments))
	for _, item := range increments {
		buckets = append(buckets, item.BucketStart)
		for i, value := range []string{item.ApplicationID, item.Environment, item.ModuleID, item.BuildRecordID, item.ReleaseID, item.Channel, item.Platform, string(item.Outcome), item.Reason} {
			columns[i] = append(columns[i], value)
		}
		users = append(users, item.Users)
		events = append(events, item.Events)
	}
	if _, err := r.pool.Exec(ctx, `
		insert into release_health_buckets (bucket_start, application_id, environment_id, module_id, build_record_id, release_id, channel, platform, outcome, reason, users, events)
		-- 上报里带的是环境编码，按应用所属租户换成环境 ID。
		select u.bucket_start, u.application_id, e.id, u.module_id, u.build_record_id, u.release_id, u.channel, u.platform, u.outcome, u.reason, u.users, u.events
		from unnest($1::timestamptz[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[], $11::bigint[], $12::bigint[])
			as u(bucket_start, application_id, environment_code, module_id, build_record_id, release_id, channel, platform, outcome, reason, users, events)
		join applications a on a.id = u.application_id
		join environments e on e.tenant_id = a.tenant_id and e.code = u.environment_code
		on conflict (bucket_start, application_id, environment_id, module_id, build_record_id, release_id, channel, platform, outcome, reason)
		do update set users = release_health_buckets.users + excluded.users,
			events = release_health_buckets.events + excluded.events,
			updated_at = now()
	`, buckets, columns[0], columns[1], columns[2], columns[3], columns[4], columns[5], columns[6], columns[7], columns[8], users, events); err != nil {
		return fmt.Errorf("increment release health buckets: %w", err)
	}
	return nil
}

func (r *ReleaseHealthRepository) DeleteReleaseHealthBucketsBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `delete from release_health_buckets where bucket_start < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("delete release health buckets: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ListReleaseHealthPolicies 返回应用绑定的每个环境的策略，没有保存过的环境返回默认值。
func (r *ReleaseHealthRepository) ListReleaseHealthPolicies(ctx context.Context, applicationSlug string) ([]domain.ReleaseHealthPolicy, error) {
	applicationID, err := applicationIDBySlug(ctx, r.pool, applicationSlug)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		select e.code, e.name, p.application_id is not null,
			coalesce(p.enabled, false), coalesce(p.window_minutes, $2), coalesce(p.min_users, $3),
			coalesce(p.max_failure_rate, $4), coalesce(p.min_delta, $5), coalesce(p.auto_stop_gray, $6),
			coalesce(p.updated_by_user_id, ''), p.updated_at
		from application_environment_bindings b
		join environments e on e.id = b.environment_id
		left join release_health_policies p on p.application_id = b.application_id and p.environment_id = b.environment_id
		where b.application_id = $1
		order by b.sort_order, e.code
	`, applicationID, defaultPolicy.WindowMinutes, defaultPolicy.MinUsers, defaultPolicy.MaxFailureRate, defaultPolicy.MinDelta, defaultPolicy.AutoStopGray)
	if err != nil {
		return nil, fmt.Errorf("query release health policies: %w", err)
	}
	defer rows.Close()
	policies := []domain.ReleaseHealthPolicy{}
	for rows.Next() {
		policy := domain.ReleaseHealthPolicy{ApplicationID: applicationID}
		var updatedAt sql.NullTime
		if err := rows.Scan(&policy.Environment, &policy.EnvironmentName, &policy.Configured, &policy.Enabled, &policy.WindowMinutes, &policy.MinUsers,
			&policy.MaxFailureRate, &policy.MinDelta, &policy.AutoStopGray, &policy.UpdatedByUserID, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan release health policy: %w", err)
		}
		if updatedAt.Valid {
			policy.UpdatedAt = &updatedAt.Time
		}
		policies = append(policies, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate release health policies: %w", err)
	}
	return policies, nil
}

var defaultPolicy = domain.DefaultReleaseHealthPolicy()

// UpsertReleaseHealthPolicy 保存应用在一个环境的策略，环境必须已绑定到应用。
func (r *ReleaseHealthRepository) UpsertReleaseHealthPolicy(ctx context.Context, applicationSlug string, policy domain.ReleaseHealthPolicy) error {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		insert into release_health_policies (application_id, environment_id, enabled, window_minutes, min_users, max_failure_rate, min_delta, auto_stop_gray, updated_by_user_id)
		select a.id, b.environment_id, $3, $4, $5, $6, $7, $8, nullif($9, '')
		from applications a
		join application_environment_bindings b on b.application_id = a.id
		join environments e on e.id = b.environment_id and e.code = $2
		where a.slug = $1 and ($10::text = '' or a.tenant_id = $10)
		on conflict (application_id, environment_id) do update set
			enabled = excluded.enabled,
			window_minutes = excluded.window_minutes,
			min_users = excluded.min_users,
			max_failure_rate = excluded.max_failure_rate,
			min_delta = excluded.min_delta,
			auto_stop_gray = excluded.auto_stop_gray,
			updated_by_user_id = excluded.updated_by_user_id,
			updated_at = now()
	`, applicationSlug, policy.Environment, policy.Enabled, policy.WindowMinutes, policy.MinUsers, policy.MaxFailureRate, policy.MinDelta, policy.AutoStopGray, policy.UpdatedByUserID, tenantID)
	if err != nil {
		if isCheckViolation(err) || isForeignKeyViolation(err) {
			return domain.ErrInvalidReleaseHealthInput
		}
		return fmt.Errorf("upsert release health policy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := applicationIDBySlug(ctx, r.pool, applicationSlug); err != nil {
			return err
		}
		return domain.ErrEnvironmentNotFound
	}
	return nil
}

// releaseHealthTargetSQL 选出生效中的发布、所属应用和环境的策略（没有时取默认值）、基线全量和上一次判定状态。
// 基线只给比例灰度找：同一模块、环境、渠道、运行时版本下生效中的全量。
const releaseHealthTargetSQL = `
	select rr.id::text, m.application_id, rr.module_id, e.code, route.runtime_version, rr.channel,
		rr.release_type, rr.gray_strategy, rr.created_at,
		p.application_id is not null, coalesce(p.enabled, false), coalesce(p.window_minutes, $1), coalesce(p.min_users, $2),
		coalesce(p.max_failure_rate, $3), coalesce(p.min_delta, $4), coalesce(p.auto_stop_gray, $5),
		coalesce((
			select full_rr.id::text
			from release_routes full_route
			join release_records full_rr on full_rr.id = full_route.release_id
			where rr.release_type = 'gray'
				and full_route.module_id = rr.module_id
				and full_route.environment_id = rr.environment_id
				and full_route.runtime_version = route.runtime_version
				and full_route.channel = rr.channel
				and full_route.route_type = 'default'
				and full_route.status = 'active'
				and full_rr.status = 'active'
			limit 1
		), ''),
		coalesce(h.status, '')
	from release_records rr
	join release_routes route on route.release_id = rr.id
	join modules m on m.id = rr.module_id
	join environments e on e.id = rr.environment_id
	left join release_health_policies p on p.application_id = m.application_id and p.environment_id = rr.environment_id
	left join release_health h on h.release_id = rr.id
`

func (r *ReleaseHealthRepository) releaseHealthTargetArgs() []any {
	return []any{defaultPolicy.WindowMinutes, defaultPolicy.MinUsers, defaultPolicy.MaxFailureRate, defaultPolicy.MinDelta, defaultPolicy.AutoStopGray}
}

// ListReleaseHealthTargets 返回策略开启的环境里所有生效中的发布。
func (r *ReleaseHealthRepository) ListReleaseHealthTargets(ctx context.Context) ([]domain.ReleaseHealthTarget, error) {
	rows, err := r.pool.Query(ctx, releaseHealthTargetSQL+`
		where rr.status = 'active' and route.status = 'active' and p.enabled
		order by rr.created_at
	`, r.releaseHealthTargetArgs()...)
	if err != nil {
		return nil, fmt.Errorf("query release health targets: %w", err)
	}
	defer rows.Close()
	targets := []domain.ReleaseHealthTarget{}
	for rows.Next() {
		target, err := scanReleaseHealthTarget(rows)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate release health targets: %w", err)
	}
	return targets, nil
}

// GetReleaseHealthTarget 返回一条发布的判定输入，不论策略是否开启、发布是否生效。
func (r *ReleaseHealthRepository) GetReleaseHealthTarget(ctx context.Context, releaseID string) (domain.ReleaseHealthTarget, error) {
	args := append(r.releaseHealthTargetArgs(), releaseID)
	return scanReleaseHealthTarget(r.pool.QueryRow(ctx, releaseHealthTargetSQL+` where rr.id = $6::uuid`, args...))
}

func scanReleaseHealthTarget(row pgx.Row) (domain.ReleaseHealthTarget, error) {
	var target domain.ReleaseHealthTarget
	var previous string
	if err := row.Scan(&target.ReleaseID, &target.ApplicationID, &target.ModuleID, &target.Environment, &target.RuntimeVersion, &target.Channel,
		&target.ReleaseType, &target.GrayStrategy, &target.CreatedAt,
		&target.Policy.Configured, &target.Policy.Enabled, &target.Policy.WindowMinutes, &target.Policy.MinUsers,
		&target.Policy.MaxFailureRate, &target.Policy.MinDelta, &target.Policy.AutoStopGray,
		&target.BaselineReleaseID, &previous); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ReleaseHealthTarget{}, domain.ErrReleaseNotFound
		}
		return domain.ReleaseHealthTarget{}, fmt.Errorf("scan release health target: %w", err)
	}
	target.Policy.ApplicationID, target.Policy.Environment = target.ApplicationID, target.Environment
	target.PreviousStatus = domain.ReleaseHealthStatus(previous)
	return target, nil
}

// LaunchCounts 汇总发布在 since 之后（含 since 所在的桶）的去重用户数。
func (r *ReleaseHealthRepository) LaunchCounts(ctx context.Context, releaseID string, since time.Time) (domain.LaunchCounts, error) {
	var counts domain.LaunchCounts
	if err := r.pool.QueryRow(ctx, `
		select
			coalesce(sum(users) filter (where outcome = 'launched'), 0)::bigint,
			coalesce(sum(users) filter (where outcome = 'launch_failed'), 0)::bigint,
			coalesce(sum(users) filter (where outcome = 'verify_failed'), 0)::bigint,
			coalesce(sum(users) filter (where outcome = 'download_failed'), 0)::bigint
		from release_health_buckets
		where release_id = $1 and bucket_start >= $2
	`, releaseID, since.Truncate(domain.ReleaseHealthBucketSize)).Scan(&counts.Launched, &counts.LaunchFailed, &counts.VerifyFailed, &counts.DownloadFailed); err != nil {
		return domain.LaunchCounts{}, fmt.Errorf("query launch counts: %w", err)
	}
	return counts, nil
}

// HasLaunchReports 判断发布创建以来是否收到过 launched 或 launch_failed 上报（不论有没有 userId）。
func (r *ReleaseHealthRepository) HasLaunchReports(ctx context.Context, releaseID string) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `
		select exists(select 1 from release_health_buckets where release_id = $1 and outcome in ('launched', 'launch_failed') and events > 0)
	`, releaseID).Scan(&exists); err != nil {
		return false, fmt.Errorf("query launch reports: %w", err)
	}
	return exists, nil
}

func (r *ReleaseHealthRepository) SaveReleaseHealth(ctx context.Context, health domain.ReleaseHealth) error {
	if _, err := r.pool.Exec(ctx, `
		insert into release_health (release_id, status, launched_users, failed_users, failure_rate, baseline_failure_rate, evaluated_at)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (release_id) do update set
			status = excluded.status,
			launched_users = excluded.launched_users,
			failed_users = excluded.failed_users,
			failure_rate = excluded.failure_rate,
			baseline_failure_rate = excluded.baseline_failure_rate,
			evaluated_at = excluded.evaluated_at
	`, health.ReleaseID, health.Status, health.Counts.Launched, health.Counts.LaunchFailed, health.FailureRate, health.BaselineFailureRate, health.EvaluatedAt); err != nil {
		return fmt.Errorf("save release health: %w", err)
	}
	return nil
}

// RecordReleaseHealthEvent 写一条系统操作记录（操作人为空）。
func (r *ReleaseHealthRepository) RecordReleaseHealthEvent(ctx context.Context, releaseID string, kind domain.ReleaseEventKind, detail map[string]any) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin release health event: %w", err)
	}
	defer rollbackMigration(ctx, tx)
	if err := insertReleaseEvent(ctx, tx, releaseID, kind, detail, ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// WithEvaluationLock 用事务级 advisory lock 保证多个 API 实例里同一时间只有一个在判定；拿不到锁时不执行 fn，返回 false。
func (r *ReleaseHealthRepository) WithEvaluationLock(ctx context.Context, fn func(context.Context) error) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin release health lock: %w", err)
	}
	defer rollbackMigration(ctx, tx)
	var locked bool
	if err := tx.QueryRow(ctx, `select pg_try_advisory_xact_lock(hashtextextended('forge:release_health:evaluate', 0))`).Scan(&locked); err != nil {
		return false, fmt.Errorf("acquire release health lock: %w", err)
	}
	if !locked {
		return false, nil
	}
	if err := fn(ctx); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}
