package postgres

import (
	"context"
	"errors"
	"fmt"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
			e.code,
			e.name,
			e.status,
			e.tone,
			e.description,
			coalesce(e.storage_configuration_id, '') as storage_configuration_id,
			coalesce(s.name, '') as storage_configuration_name,
			e.updated_at
		from environments e
		left join storage_configurations s on s.id = e.storage_configuration_id
		order by e.sort_order asc, e.code asc
	`)
	if err != nil {
		return nil, fmt.Errorf("query environments: %w", err)
	}
	defer rows.Close()

	environments := make([]domain.Environment, 0)
	for rows.Next() {
		environment, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		environments = append(environments, environment)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate environments: %w", err)
	}

	return environments, nil
}

func (r *EnvironmentRepository) CreateEnvironment(ctx context.Context, input domain.EnvironmentInput) (domain.Environment, error) {
	environment, err := scanEnvironment(r.pool.QueryRow(ctx, `
		with inserted as (
			insert into environments (
				code,
				name,
				status,
				tone,
				description,
				storage_configuration_id,
				sort_order
			) values (
				$1,
				$2,
				$3,
				$4,
				$5,
				$6,
				coalesce((select max(sort_order) + 10 from environments), 10)
			)
			returning
				code,
				name,
				status,
				tone,
				description,
				storage_configuration_id,
				updated_at
		)
		select
			i.code,
			i.name,
			i.status,
			i.tone,
			i.description,
			coalesce(i.storage_configuration_id, '') as storage_configuration_id,
			coalesce(s.name, '') as storage_configuration_name,
			i.updated_at
		from inserted i
		left join storage_configurations s on s.id = i.storage_configuration_id
	`,
		input.Code,
		input.Name,
		input.Status,
		input.Tone,
		input.Description,
		input.StorageConfigurationID,
	))
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Environment{}, domain.ErrInvalidEnvironmentInput
		}
		if isForeignKeyViolation(err) {
			return domain.Environment{}, domain.ErrStorageConfigurationNotFound
		}
		return domain.Environment{}, fmt.Errorf("insert environment: %w", err)
	}
	return environment, nil
}

func (r *EnvironmentRepository) GetEnvironmentDetail(ctx context.Context, code string) (domain.Environment, error) {
	environment, err := scanEnvironment(r.pool.QueryRow(ctx, `
		select
			e.code,
			e.name,
			e.status,
			e.tone,
			e.description,
			coalesce(e.storage_configuration_id, '') as storage_configuration_id,
			coalesce(s.name, '') as storage_configuration_name,
			e.updated_at
		from environments e
		left join storage_configurations s on s.id = e.storage_configuration_id
		where e.code = $1
	`, code))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Environment{}, domain.ErrEnvironmentNotFound
		}
		return domain.Environment{}, fmt.Errorf("query environment detail: %w", err)
	}

	return environment, nil
}

func (r *EnvironmentRepository) UpdateEnvironment(ctx context.Context, code string, input domain.EnvironmentInput) (domain.Environment, error) {
	environment, err := scanEnvironment(r.pool.QueryRow(ctx, `
		with updated as (
			update environments
			set
				name = $2,
				status = $3,
				tone = $4,
				description = $5,
				storage_configuration_id = $6,
				updated_at = now()
			where code = $1
			returning
				code,
				name,
				status,
				tone,
				description,
				storage_configuration_id,
				updated_at
		)
		select
			u.code,
			u.name,
			u.status,
			u.tone,
			u.description,
			coalesce(u.storage_configuration_id, '') as storage_configuration_id,
			coalesce(s.name, '') as storage_configuration_name,
			u.updated_at
		from updated u
		left join storage_configurations s on s.id = u.storage_configuration_id
	`,
		code,
		input.Name,
		input.Status,
		input.Tone,
		input.Description,
		input.StorageConfigurationID,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Environment{}, domain.ErrEnvironmentNotFound
		}
		if isUniqueViolation(err) {
			return domain.Environment{}, domain.ErrInvalidEnvironmentInput
		}
		if isForeignKeyViolation(err) {
			return domain.Environment{}, domain.ErrStorageConfigurationNotFound
		}
		return domain.Environment{}, fmt.Errorf("update environment: %w", err)
	}
	return environment, nil
}

type environmentRow interface {
	Scan(dest ...any) error
}

func scanEnvironment(row environmentRow) (domain.Environment, error) {
	var environment domain.Environment
	if err := row.Scan(
		&environment.Code,
		&environment.Name,
		&environment.Status,
		&environment.Tone,
		&environment.Description,
		&environment.StorageConfigurationID,
		&environment.StorageConfigurationName,
		&environment.UpdatedAt,
	); err != nil {
		return domain.Environment{}, fmt.Errorf("scan environment: %w", err)
	}
	return environment, nil
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
