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

type BuildVariableRepository struct {
	pool *pgxpool.Pool
}

func NewBuildVariableRepository(pool *pgxpool.Pool) *BuildVariableRepository {
	return &BuildVariableRepository{pool: pool}
}

const buildVariableSelectColumns = `
	v.module_id,
	v.key,
	v.value_encrypted,
	v.is_secret,
	coalesce(e.code, ''),
	v.updated_by_user_id,
	coalesce(u.name, ''),
	v.updated_at
`

func (r *BuildVariableRepository) ListModuleBuildVariables(ctx context.Context, moduleID string) ([]domain.ModuleBuildVariable, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return nil, err
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `
		select exists(select 1 from modules where id = $1 and ($2::text = '' or tenant_id = $2))
	`, moduleID, tenantID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check module: %w", err)
	}
	if !exists {
		return nil, domain.ErrModuleNotFound
	}
	return r.queryBuildVariables(ctx, `
		select `+buildVariableSelectColumns+`
		from module_build_variables v
		left join users u on u.id = v.updated_by_user_id
		left join environments e on e.id = v.environment_id
		where v.module_id = $1
		order by v.key asc, e.code asc nulls first
	`, moduleID)
}

func (r *BuildVariableRepository) ListApplicableBuildVariables(ctx context.Context, moduleID string, environmentCode string) ([]domain.ModuleBuildVariable, error) {
	return r.queryBuildVariables(ctx, `
		select `+buildVariableSelectColumns+`
		from module_build_variables v
		left join users u on u.id = v.updated_by_user_id
		left join environments e on e.id = v.environment_id
		where v.module_id = $1
			and (v.environment_id is null or e.code = $2)
		order by v.key asc, e.code asc nulls first
	`, moduleID, environmentCode)
}

func (r *BuildVariableRepository) UpsertModuleBuildVariable(ctx context.Context, variable domain.ModuleBuildVariable) (domain.ModuleBuildVariable, error) {
	moduleTenantID, err := r.moduleTenantID(ctx, variable.ModuleID)
	if err != nil {
		return domain.ModuleBuildVariable{}, err
	}
	environmentID, err := optionalEnvironmentID(ctx, r.pool, moduleTenantID, variable.EnvironmentCode)
	if err != nil {
		return domain.ModuleBuildVariable{}, err
	}
	saved, err := scanBuildVariable(r.pool.QueryRow(ctx, `
		with upserted as (
			insert into module_build_variables (
				module_id,
				key,
				value_encrypted,
				is_secret,
				environment_id,
				updated_by_user_id,
				updated_at
			) values (
				$1, $2, $3, $4, $5, $6, now()
			)
			on conflict (module_id, key, environment_id) do update set
				value_encrypted = excluded.value_encrypted,
				is_secret = excluded.is_secret,
				updated_by_user_id = excluded.updated_by_user_id,
				updated_at = now()
			returning *
		)
		select `+buildVariableSelectColumns+`
		from upserted v
		left join users u on u.id = v.updated_by_user_id
		left join environments e on e.id = v.environment_id
	`,
		variable.ModuleID,
		variable.Key,
		variable.ValueEncrypted,
		variable.IsSecret,
		environmentID,
		variable.UpdatedByUserID,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		switch {
		case errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "module_build_variables_module_id_fkey":
			return domain.ModuleBuildVariable{}, domain.ErrModuleNotFound
		case errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "module_build_variables_environment_id_fkey":
			return domain.ModuleBuildVariable{}, domain.ErrEnvironmentNotFound
		case isForeignKeyViolation(err), isCheckViolation(err):
			return domain.ModuleBuildVariable{}, domain.ErrInvalidBuildVariableInput
		}
		return domain.ModuleBuildVariable{}, fmt.Errorf("upsert build variable: %w", err)
	}
	return saved, nil
}

func (r *BuildVariableRepository) DeleteModuleBuildVariable(ctx context.Context, moduleID string, key string, environmentCode string) error {
	moduleTenantID, err := r.moduleTenantID(ctx, moduleID)
	if err != nil {
		return err
	}
	environmentID, err := optionalEnvironmentID(ctx, r.pool, moduleTenantID, environmentCode)
	if errors.Is(err, domain.ErrEnvironmentNotFound) {
		return domain.ErrBuildVariableNotFound
	}
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		delete from module_build_variables
		where module_id = $1
			and key = $2
			and environment_id is not distinct from $3
	`, moduleID, key, environmentID)
	if err != nil {
		return fmt.Errorf("delete build variable: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrBuildVariableNotFound
	}
	return nil
}

func (r *BuildVariableRepository) queryBuildVariables(ctx context.Context, query string, args ...any) ([]domain.ModuleBuildVariable, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list build variables: %w", err)
	}
	defer rows.Close()

	variables := []domain.ModuleBuildVariable{}
	for rows.Next() {
		variable, err := scanBuildVariable(rows)
		if err != nil {
			return nil, err
		}
		variables = append(variables, variable)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list build variables: %w", err)
	}
	return variables, nil
}

func scanBuildVariable(row pgx.Row) (domain.ModuleBuildVariable, error) {
	var variable domain.ModuleBuildVariable
	if err := row.Scan(
		&variable.ModuleID,
		&variable.Key,
		&variable.ValueEncrypted,
		&variable.IsSecret,
		&variable.EnvironmentCode,
		&variable.UpdatedByUserID,
		&variable.UpdatedBy,
		&variable.UpdatedAt,
	); err != nil {
		return domain.ModuleBuildVariable{}, err
	}
	return variable, nil
}

// moduleTenantID 返回模块所属租户，模块不存在或不在当前范围内时返回 ErrModuleNotFound。
func (r *BuildVariableRepository) moduleTenantID(ctx context.Context, moduleID string) (string, error) {
	scopeTenantID, err := tenantFilter(ctx)
	if err != nil {
		return "", err
	}
	var tenantID string
	if err := r.pool.QueryRow(ctx, `
		select tenant_id from modules where id = $1 and ($2::text = '' or tenant_id = $2)
	`, moduleID, scopeTenantID).Scan(&tenantID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", domain.ErrModuleNotFound
		}
		return "", fmt.Errorf("query module tenant: %w", err)
	}
	return tenantID, nil
}
