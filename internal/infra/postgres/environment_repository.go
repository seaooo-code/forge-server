package postgres

import (
	"context"
	"errors"
	"fmt"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EnvironmentRepository struct {
	pool *pgxpool.Pool
}

func NewEnvironmentRepository(pool *pgxpool.Pool) *EnvironmentRepository {
	return &EnvironmentRepository{pool: pool}
}

func (r *EnvironmentRepository) ListEnvironments(ctx context.Context) ([]domain.Environment, error) {
	rows, err := r.pool.Query(ctx, `
		select
			code,
			short_code,
			name,
			status,
			tone,
			description,
			current_combination,
			traffic_label,
			updated_at
		from environments
		order by sort_order asc
	`)
	if err != nil {
		return nil, fmt.Errorf("query environments: %w", err)
	}
	defer rows.Close()

	environments := make([]domain.Environment, 0)
	for rows.Next() {
		var environment domain.Environment
		if err := rows.Scan(
			&environment.Code,
			&environment.ShortCode,
			&environment.Name,
			&environment.Status,
			&environment.Tone,
			&environment.Description,
			&environment.CurrentCombination,
			&environment.TrafficLabel,
			&environment.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan environment: %w", err)
		}
		environments = append(environments, environment)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate environments: %w", err)
	}

	return environments, nil
}

func (r *EnvironmentRepository) GetEnvironmentDetail(ctx context.Context, code string) (domain.EnvironmentDetail, error) {
	var detail domain.EnvironmentDetail
	if err := r.pool.QueryRow(ctx, `
		select
			code,
			short_code,
			name,
			status,
			tone,
			description,
			current_combination,
			traffic_label,
			updated_at,
			endpoint,
			cdn_domain,
			branch_policy,
			release_policy,
			current_release,
			bundle_count,
			total_size,
			health_score
		from environments
		where code = $1
	`, code).Scan(
		&detail.Environment.Code,
		&detail.Environment.ShortCode,
		&detail.Environment.Name,
		&detail.Environment.Status,
		&detail.Environment.Tone,
		&detail.Environment.Description,
		&detail.Environment.CurrentCombination,
		&detail.Environment.TrafficLabel,
		&detail.Environment.UpdatedAt,
		&detail.Endpoint,
		&detail.CDNDomain,
		&detail.BranchPolicy,
		&detail.ReleasePolicy,
		&detail.CurrentRelease,
		&detail.BundleCount,
		&detail.TotalSize,
		&detail.HealthScore,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.EnvironmentDetail{}, domain.ErrEnvironmentNotFound
		}
		return domain.EnvironmentDetail{}, fmt.Errorf("query environment detail: %w", err)
	}

	detail.ID = detail.Environment.Code

	metrics, err := r.listEnvironmentMetrics(ctx, code)
	if err != nil {
		return domain.EnvironmentDetail{}, err
	}
	detail.Metrics = metrics

	guardrails, err := r.listEnvironmentGuardrails(ctx, code)
	if err != nil {
		return domain.EnvironmentDetail{}, err
	}
	detail.Guardrails = guardrails

	timeline, err := r.listEnvironmentTimeline(ctx, code)
	if err != nil {
		return domain.EnvironmentDetail{}, err
	}
	detail.Timeline = timeline

	return detail, nil
}

func (r *EnvironmentRepository) listEnvironmentMetrics(ctx context.Context, code string) ([]domain.EnvironmentMetric, error) {
	rows, err := r.pool.Query(ctx, `
		select metric_key, label, value, detail, tone
		from environment_metrics
		where environment_code = $1
		order by sort_order asc
	`, code)
	if err != nil {
		return nil, fmt.Errorf("query environment metrics: %w", err)
	}
	defer rows.Close()

	metrics := make([]domain.EnvironmentMetric, 0)
	for rows.Next() {
		var metric domain.EnvironmentMetric
		if err := rows.Scan(&metric.ID, &metric.Label, &metric.Value, &metric.Detail, &metric.Tone); err != nil {
			return nil, fmt.Errorf("scan environment metric: %w", err)
		}
		metrics = append(metrics, metric)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate environment metrics: %w", err)
	}

	return metrics, nil
}

func (r *EnvironmentRepository) listEnvironmentGuardrails(ctx context.Context, code string) ([]domain.EnvironmentGuardrail, error) {
	rows, err := r.pool.Query(ctx, `
		select guardrail_key, label, value, status
		from environment_guardrails
		where environment_code = $1
		order by sort_order asc
	`, code)
	if err != nil {
		return nil, fmt.Errorf("query environment guardrails: %w", err)
	}
	defer rows.Close()

	guardrails := make([]domain.EnvironmentGuardrail, 0)
	for rows.Next() {
		var guardrail domain.EnvironmentGuardrail
		if err := rows.Scan(&guardrail.ID, &guardrail.Label, &guardrail.Value, &guardrail.Status); err != nil {
			return nil, fmt.Errorf("scan environment guardrail: %w", err)
		}
		guardrails = append(guardrails, guardrail)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate environment guardrails: %w", err)
	}

	return guardrails, nil
}

func (r *EnvironmentRepository) listEnvironmentTimeline(ctx context.Context, code string) ([]domain.EnvironmentTimelineEvent, error) {
	rows, err := r.pool.Query(ctx, `
		select event_key, display_time, action, operator, status, detail
		from environment_timeline_events
		where environment_code = $1
		order by sort_order asc
	`, code)
	if err != nil {
		return nil, fmt.Errorf("query environment timeline: %w", err)
	}
	defer rows.Close()

	events := make([]domain.EnvironmentTimelineEvent, 0)
	for rows.Next() {
		var event domain.EnvironmentTimelineEvent
		if err := rows.Scan(
			&event.ID,
			&event.DisplayTime,
			&event.Action,
			&event.Operator,
			&event.Status,
			&event.Detail,
		); err != nil {
			return nil, fmt.Errorf("scan environment timeline: %w", err)
		}
		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate environment timeline: %w", err)
	}

	return events, nil
}
