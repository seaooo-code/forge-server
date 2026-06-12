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

func (r *EnvironmentRepository) GetEnvironmentDetail(ctx context.Context, code string) (domain.Environment, error) {
	var environment domain.Environment
	if err := r.pool.QueryRow(ctx, `
		select
			code,
			short_code,
			name,
			status,
			tone,
			description,
			traffic_label,
			updated_at
		from environments
		where code = $1
	`, code).Scan(
		&environment.Code,
		&environment.ShortCode,
		&environment.Name,
		&environment.Status,
		&environment.Tone,
		&environment.Description,
		&environment.TrafficLabel,
		&environment.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Environment{}, domain.ErrEnvironmentNotFound
		}
		return domain.Environment{}, fmt.Errorf("query environment detail: %w", err)
	}

	return environment, nil
}
