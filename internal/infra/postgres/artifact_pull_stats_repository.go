package postgres

import (
	"context"
	"fmt"
	"time"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ArtifactPullStatsRepository struct {
	pool *pgxpool.Pool
}

func NewArtifactPullStatsRepository(pool *pgxpool.Pool) *ArtifactPullStatsRepository {
	return &ArtifactPullStatsRepository{pool: pool}
}

// IncrementArtifactPullStats 用一条 upsert 批量累加计数，多个 API 实例并发写入时由主键冲突合并。
func (r *ArtifactPullStatsRepository) IncrementArtifactPullStats(ctx context.Context, stats []domain.ArtifactPullStat) error {
	if len(stats) == 0 {
		return nil
	}
	days := make([]time.Time, 0, len(stats))
	columns := make([][]string, 7)
	requests := make([]int64, 0, len(stats))
	for _, stat := range stats {
		days = append(days, stat.Day)
		for i, value := range []string{stat.ApplicationID, stat.Environment, stat.ModuleID, stat.RuntimeVersion, stat.Platform, stat.Outcome, stat.BuildRecordID} {
			columns[i] = append(columns[i], value)
		}
		requests = append(requests, stat.Requests)
	}
	if _, err := r.pool.Exec(ctx, `
		insert into artifact_pull_daily_stats (day, application_id, environment_code, module_id, runtime_version, platform, outcome, build_record_id, requests)
		select * from unnest($1::date[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::bigint[])
		on conflict (day, application_id, environment_code, module_id, runtime_version, platform, outcome, build_record_id)
		do update set requests = artifact_pull_daily_stats.requests + excluded.requests, updated_at = now()
	`, days, columns[0], columns[1], columns[2], columns[3], columns[4], columns[5], columns[6], requests); err != nil {
		return fmt.Errorf("increment artifact pull stats: %w", err)
	}
	return nil
}

func (r *ArtifactPullStatsRepository) ListArtifactPullStats(ctx context.Context, filters domain.ArtifactPullStatsFilters) ([]domain.ArtifactPullStatsRow, error) {
	rows, err := r.pool.Query(ctx, `
		select s.day, s.application_id, s.environment_code, s.module_id, s.runtime_version, s.platform, s.outcome, s.build_record_id, s.requests,
			coalesce(m.name, ''), coalesce(br.number, 0)
		from artifact_pull_daily_stats s
		left join modules m on m.id = s.module_id
		-- 按文本比较，避免对缺包行的空字符串做 uuid 转换。
		left join build_records br on br.id::text = s.build_record_id
		where ($1 = '' or s.application_id = $1)
			and s.day >= $2
			and ($3 = '' or s.environment_code = $3)
			and ($4 = '' or s.module_id = $4)
			and ($5 = '' or s.build_record_id = $5)
		order by s.day desc, m.sort_order nulls last, s.module_id, s.environment_code, s.runtime_version, s.platform, s.outcome, s.build_record_id
		limit 5000
	`, filters.ApplicationID, filters.From, filters.Environment, filters.ModuleID, filters.BuildRecordID)
	if err != nil {
		return nil, fmt.Errorf("query artifact pull stats: %w", err)
	}
	defer rows.Close()

	result := []domain.ArtifactPullStatsRow{}
	for rows.Next() {
		var row domain.ArtifactPullStatsRow
		if err := rows.Scan(&row.Day, &row.ApplicationID, &row.Environment, &row.ModuleID, &row.RuntimeVersion, &row.Platform, &row.Outcome,
			&row.BuildRecordID, &row.Requests, &row.ModuleName, &row.BuildNumber); err != nil {
			return nil, fmt.Errorf("scan artifact pull stats: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifact pull stats: %w", err)
	}
	return result, nil
}

func (r *ArtifactPullStatsRepository) DeleteArtifactPullStatsBefore(ctx context.Context, day time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `delete from artifact_pull_daily_stats where day < $1`, day)
	if err != nil {
		return 0, fmt.Errorf("delete artifact pull stats: %w", err)
	}
	return tag.RowsAffected(), nil
}
