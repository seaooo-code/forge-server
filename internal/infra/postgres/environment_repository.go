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
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		select
			e.code,
			e.name,
			e.status,
			e.tone,
			e.description,
			coalesce(e.storage_configuration_id, '') as storage_configuration_id,
			coalesce(s.name, '') as storage_configuration_name,
			e.allow_test_plans,
			e.updated_at
		from environments e
		left join storage_configurations s on s.id = e.storage_configuration_id
		where $1::text = '' or e.tenant_id = $1
		order by e.sort_order asc, e.code asc
	`, tenantID)
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
	tenantID, err := domain.TenantIDFromContext(ctx)
	if err != nil {
		return domain.Environment{}, err
	}
	environment, err := scanEnvironment(r.pool.QueryRow(ctx, `
		with inserted as (
			insert into environments (
				id,
				tenant_id,
				code,
				name,
				status,
				tone,
				description,
				storage_configuration_id,
				allow_test_plans,
				sort_order
			) values (
				$8,
				$9,
				$1,
				$2,
				$3,
				$4,
				$5,
				$6,
				$7,
				coalesce((select max(sort_order) + 10 from environments where tenant_id = $9), 10)
			)
			returning
				code,
				name,
				status,
				tone,
				description,
				storage_configuration_id,
				allow_test_plans,
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
			i.allow_test_plans,
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
		input.AllowTestPlans,
		newPrefixedID("env"),
		tenantID,
	))
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Environment{}, domain.ErrInvalidEnvironmentInput
		}
		if isForeignKeyViolation(err) {
			// 存储配置不存在，或者属于其他租户。
			return domain.Environment{}, domain.ErrStorageConfigurationNotFound
		}
		return domain.Environment{}, fmt.Errorf("insert environment: %w", err)
	}
	return environment, nil
}

func (r *EnvironmentRepository) GetEnvironmentDetail(ctx context.Context, code string) (domain.Environment, error) {
	tenantID, err := domain.TenantIDFromContext(ctx)
	if err != nil {
		return domain.Environment{}, err
	}
	environment, err := scanEnvironment(r.pool.QueryRow(ctx, `
		select
			e.code,
			e.name,
			e.status,
			e.tone,
			e.description,
			coalesce(e.storage_configuration_id, '') as storage_configuration_id,
			coalesce(s.name, '') as storage_configuration_name,
			e.allow_test_plans,
			e.updated_at
		from environments e
		left join storage_configurations s on s.id = e.storage_configuration_id
		where e.code = $1 and e.tenant_id = $2
	`, code, tenantID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Environment{}, domain.ErrEnvironmentNotFound
		}
		return domain.Environment{}, fmt.Errorf("query environment detail: %w", err)
	}

	return environment, nil
}

func (r *EnvironmentRepository) UpdateEnvironment(ctx context.Context, code string, input domain.EnvironmentInput) (domain.Environment, error) {
	tenantID, err := domain.TenantIDFromContext(ctx)
	if err != nil {
		return domain.Environment{}, err
	}
	environment, err := scanEnvironment(r.pool.QueryRow(ctx, `
		with updated as (
			update environments
			set
				name = $2,
				status = $3,
				tone = $4,
				description = $5,
				storage_configuration_id = $6,
				allow_test_plans = $7,
				updated_at = now()
			where code = $1 and tenant_id = $8
			returning
				code,
				name,
				status,
				tone,
				description,
				storage_configuration_id,
				allow_test_plans,
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
			u.allow_test_plans,
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
		input.AllowTestPlans,
		tenantID,
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
		&environment.AllowTestPlans,
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

type environmentQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// environmentIDByCode 在指定租户内按编码查环境 ID。环境编码只在租户内唯一，所有按编码写入的地方都要先经过这里。
func environmentIDByCode(ctx context.Context, querier environmentQuerier, tenantID string, code string) (string, error) {
	var id string
	if err := querier.QueryRow(ctx, `
		select id from environments where tenant_id = $1 and code = $2
	`, tenantID, code).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", domain.ErrEnvironmentNotFound
		}
		return "", fmt.Errorf("query environment by code: %w", err)
	}
	return id, nil
}

// optionalEnvironmentID 用于可以不指定环境的字段：编码为空时返回 nil。
func optionalEnvironmentID(ctx context.Context, querier environmentQuerier, tenantID string, code string) (*string, error) {
	if code == "" {
		return nil, nil
	}
	id, err := environmentIDByCode(ctx, querier, tenantID, code)
	if err != nil {
		return nil, err
	}
	return &id, nil
}
