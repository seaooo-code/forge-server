package postgres

import (
	"context"
	"errors"
	"fmt"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ModuleRepository struct {
	pool *pgxpool.Pool
}

func NewModuleRepository(pool *pgxpool.Pool) *ModuleRepository {
	return &ModuleRepository{pool: pool}
}

func (r *ModuleRepository) ListModules(ctx context.Context, filters domain.ModuleListFilters) ([]domain.Module, error) {
	rows, err := r.pool.Query(ctx, `
		select
			m.id,
			m.name,
			m.kind,
			m.status,
			m.application_id,
			a.name as application_name,
			a.slug as application_slug,
			m.owner_user_id,
			u.name as owner,
			u.email as owner_email,
			u.avatar_url as owner_avatar_url,
			u.department_ids as owner_department_ids,
			m.repository_provider,
			m.provider_repository_id,
			m.provider_installation_id,
			m.repository_full_name,
			m.repository_html_url,
			m.repository_clone_url,
			m.repository_ssh_url,
			m.default_branch,
			m.created_at,
			m.updated_at
		from modules m
		join applications a on a.id = m.application_id
		join users u on u.id = m.owner_user_id
		where
			($1 = '' or m.kind = $1)
			and ($2 = '' or m.status = $2)
			and ($3 = '' or m.owner_user_id = $3)
			and (
				$4 = ''
				or m.id ilike '%' || $4 || '%'
				or m.name ilike '%' || $4 || '%'
				or m.repository_full_name ilike '%' || $4 || '%'
				or a.name ilike '%' || $4 || '%'
			)
		order by m.sort_order asc, m.created_at asc
	`, string(filters.Kind), string(filters.Status), filters.Owner, filters.Keyword)
	if err != nil {
		return nil, fmt.Errorf("query modules: %w", err)
	}
	defer rows.Close()

	modules := make([]domain.Module, 0)
	for rows.Next() {
		module, err := scanModule(rows)
		if err != nil {
			return nil, err
		}
		modules = append(modules, module)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate modules: %w", err)
	}
	return modules, nil
}

func (r *ModuleRepository) GetModule(ctx context.Context, id string) (domain.Module, error) {
	return scanModule(r.pool.QueryRow(ctx, moduleSelectSQL()+` where m.id = $1`, id))
}

func (r *ModuleRepository) CreateModule(ctx context.Context, input domain.ModuleInput) (domain.Module, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Module{}, fmt.Errorf("begin create module: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	if err := upsertUser(ctx, tx, moduleOwnerUser(input)); err != nil {
		return domain.Module{}, err
	}

	if _, err := tx.Exec(ctx, `
		insert into modules (
			id,
			name,
			kind,
			status,
			application_id,
			owner_user_id,
			repository_provider,
			provider_repository_id,
			provider_installation_id,
			repository_full_name,
			repository_html_url,
			repository_clone_url,
			repository_ssh_url,
			default_branch,
			sort_order
		) values (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			$11,
			$12,
			$13,
			$14,
			coalesce((select max(sort_order) + 10 from modules), 10)
		)
	`,
		input.ID,
		input.Name,
		input.Kind,
		input.Status,
		input.ApplicationID,
		input.OwnerUserID,
		input.Repository.Provider,
		input.Repository.RepositoryID,
		input.Repository.InstallationID,
		input.Repository.FullName,
		input.Repository.HTMLURL,
		input.Repository.CloneURL,
		input.Repository.SSHURL,
		input.Repository.DefaultBranch,
	); err != nil {
		if isUniqueViolation(err) {
			return domain.Module{}, domain.ErrModuleIDConflict
		}
		if isForeignKeyViolation(err) {
			return domain.Module{}, domain.ErrInvalidModuleInput
		}
		return domain.Module{}, fmt.Errorf("insert module: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		update applications
		set module_count = module_count + 1, updated_at = now()
		where id = $1
	`, input.ApplicationID); err != nil {
		return domain.Module{}, fmt.Errorf("increment application module count: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Module{}, fmt.Errorf("commit create module: %w", err)
	}

	return r.GetModule(ctx, input.ID)
}

func (r *ModuleRepository) UpdateModule(ctx context.Context, id string, input domain.ModuleInput) (domain.Module, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Module{}, fmt.Errorf("begin update module: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	var previousApplicationID string
	if err := tx.QueryRow(ctx, `
		select application_id
		from modules
		where id = $1
		for update
	`, id).Scan(&previousApplicationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Module{}, domain.ErrModuleNotFound
		}
		return domain.Module{}, fmt.Errorf("query module for update: %w", err)
	}

	if err := upsertUser(ctx, tx, moduleOwnerUser(input)); err != nil {
		return domain.Module{}, err
	}

	if _, err := tx.Exec(ctx, `
		update modules set
			name = $2,
			kind = $3,
			status = $4,
			application_id = $5,
			owner_user_id = $6,
			repository_provider = $7,
			provider_repository_id = $8,
			provider_installation_id = $9,
			repository_full_name = $10,
			repository_html_url = $11,
			repository_clone_url = $12,
			repository_ssh_url = $13,
			default_branch = $14,
			updated_at = now()
		where id = $1
	`,
		id,
		input.Name,
		input.Kind,
		input.Status,
		input.ApplicationID,
		input.OwnerUserID,
		input.Repository.Provider,
		input.Repository.RepositoryID,
		input.Repository.InstallationID,
		input.Repository.FullName,
		input.Repository.HTMLURL,
		input.Repository.CloneURL,
		input.Repository.SSHURL,
		input.Repository.DefaultBranch,
	); err != nil {
		if isUniqueViolation(err) {
			return domain.Module{}, domain.ErrModuleIDConflict
		}
		if isForeignKeyViolation(err) {
			return domain.Module{}, domain.ErrInvalidModuleInput
		}
		return domain.Module{}, fmt.Errorf("update module: %w", err)
	}

	if previousApplicationID != input.ApplicationID {
		if _, err := tx.Exec(ctx, `
			update applications
			set module_count = greatest(module_count - 1, 0), updated_at = now()
			where id = $1
		`, previousApplicationID); err != nil {
			return domain.Module{}, fmt.Errorf("decrement previous application module count: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			update applications
			set module_count = module_count + 1, updated_at = now()
			where id = $1
		`, input.ApplicationID); err != nil {
			return domain.Module{}, fmt.Errorf("increment next application module count: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Module{}, fmt.Errorf("commit update module: %w", err)
	}

	return r.GetModule(ctx, id)
}

func (r *ModuleRepository) DeleteModule(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete module: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	var applicationID string
	if err := tx.QueryRow(ctx, `
		delete from modules
		where id = $1
		returning application_id
	`, id).Scan(&applicationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrModuleNotFound
		}
		return fmt.Errorf("delete module: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		update applications
		set module_count = greatest(module_count - 1, 0), updated_at = now()
		where id = $1
	`, applicationID); err != nil {
		return fmt.Errorf("decrement application module count: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete module: %w", err)
	}
	return nil
}

type moduleRow interface {
	Scan(dest ...any) error
}

func moduleSelectSQL() string {
	return `
		select
			m.id,
			m.name,
			m.kind,
			m.status,
			m.application_id,
			a.name as application_name,
			a.slug as application_slug,
			m.owner_user_id,
			u.name as owner,
			u.email as owner_email,
			u.avatar_url as owner_avatar_url,
			u.department_ids as owner_department_ids,
			m.repository_provider,
			m.provider_repository_id,
			m.provider_installation_id,
			m.repository_full_name,
			m.repository_html_url,
			m.repository_clone_url,
			m.repository_ssh_url,
			m.default_branch,
			m.created_at,
			m.updated_at
		from modules m
		join applications a on a.id = m.application_id
		join users u on u.id = m.owner_user_id`
}

func scanModule(row moduleRow) (domain.Module, error) {
	var module domain.Module
	if err := row.Scan(
		&module.ID,
		&module.Name,
		&module.Kind,
		&module.Status,
		&module.ApplicationID,
		&module.ApplicationName,
		&module.ApplicationSlug,
		&module.OwnerUserID,
		&module.Owner,
		&module.OwnerEmail,
		&module.OwnerAvatarURL,
		&module.OwnerDepartmentIDs,
		&module.RepositoryProvider,
		&module.RepositoryID,
		&module.RepositoryInstallationID,
		&module.RepositoryFullName,
		&module.RepositoryHTMLURL,
		&module.RepositoryCloneURL,
		&module.RepositorySSHURL,
		&module.DefaultBranch,
		&module.CreatedAt,
		&module.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Module{}, domain.ErrModuleNotFound
		}
		return domain.Module{}, fmt.Errorf("scan module: %w", err)
	}
	return module, nil
}

func moduleOwnerUser(input domain.ModuleInput) domain.UserInput {
	return domain.UserInput{
		ID:            input.OwnerUserID,
		Name:          input.Owner,
		Email:         input.OwnerEmail,
		AvatarURL:     input.OwnerAvatarURL,
		DepartmentIDs: input.OwnerDepartmentIDs,
		FeishuUnionID: input.OwnerFeishuUnionID,
		FeishuOpenID:  input.OwnerFeishuOpenID,
		FeishuUserID:  input.OwnerFeishuUserID,
	}
}
