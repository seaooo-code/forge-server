package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CodeSourceRepository struct {
	pool *pgxpool.Pool
}

func NewCodeSourceRepository(pool *pgxpool.Pool) *CodeSourceRepository {
	return &CodeSourceRepository{pool: pool}
}

func (r *CodeSourceRepository) GetCodeSourceAppConfig(ctx context.Context, provider domain.CodeSourceProvider) (domain.CodeSourceAppConfig, error) {
	return scanCodeSourceAppConfig(r.pool.QueryRow(ctx, `
		select
			provider,
			provider_app_id,
			app_slug,
			name,
			html_url,
			private_key,
			webhook_secret
		from code_source_app_configs
		where provider = $1
	`, provider))
}

func (r *CodeSourceRepository) ListCodeSourceInstallations(ctx context.Context, provider domain.CodeSourceProvider, ownerUnionID string) ([]domain.CodeSourceInstallation, error) {
	rows, err := r.pool.Query(ctx, `
		select
			provider,
			owner_union_id,
			provider_installation_id,
			account_login,
			account_type,
			repository_selection,
			installed_at,
			updated_at,
			deleted_at
		from code_source_installations
		where provider = $1 and owner_union_id = $2 and status = 'active'
		order by installed_at desc, id desc
	`, provider, ownerUnionID)
	if err != nil {
		return nil, fmt.Errorf("query code source installations: %w", err)
	}
	defer rows.Close()

	installations := make([]domain.CodeSourceInstallation, 0)
	for rows.Next() {
		installation, err := scanCodeSourceInstallation(rows)
		if err != nil {
			return nil, err
		}
		installations = append(installations, installation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate code source installations: %w", err)
	}
	return installations, nil
}

func (r *CodeSourceRepository) UpsertCodeSourceInstallation(ctx context.Context, installation domain.CodeSourceInstallation) (domain.CodeSourceInstallation, error) {
	saved, err := scanCodeSourceInstallation(r.pool.QueryRow(ctx, `
		insert into code_source_installations (
			provider,
			owner_union_id,
			provider_installation_id,
			account_login,
			account_type,
			repository_selection,
			status,
			installed_at
		) values (
			$1, $2, $3, $4, $5, $6, 'active', $7
		)
		on conflict (provider, owner_union_id, provider_installation_id) do update set
			account_login = excluded.account_login,
			account_type = excluded.account_type,
			repository_selection = excluded.repository_selection,
			status = 'active',
			installed_at = excluded.installed_at,
			deleted_at = null,
			updated_at = now()
		returning
			provider,
			owner_union_id,
			provider_installation_id,
			account_login,
			account_type,
			repository_selection,
			installed_at,
			updated_at,
			deleted_at
	`,
		installation.Provider,
		installation.OwnerUnionID,
		installation.InstallationID,
		installation.AccountLogin,
		installation.AccountType,
		installation.RepositorySelection,
		installation.InstalledAt,
	))
	if err != nil {
		return domain.CodeSourceInstallation{}, fmt.Errorf("upsert code source installation: %w", err)
	}
	return saved, nil
}

func (r *CodeSourceRepository) UpsertCodeSourceAppConfig(ctx context.Context, config domain.CodeSourceAppConfig) (domain.CodeSourceAppConfig, error) {
	saved, err := scanCodeSourceAppConfig(r.pool.QueryRow(ctx, `
		insert into code_source_app_configs (
			provider,
			provider_app_id,
			app_slug,
			name,
			html_url,
			private_key,
			webhook_secret
		) values (
			$1, $2, $3, $4, $5, $6, $7
		)
		on conflict (provider) do update set
			provider_app_id = excluded.provider_app_id,
			app_slug = excluded.app_slug,
			name = excluded.name,
			html_url = excluded.html_url,
			private_key = excluded.private_key,
			webhook_secret = excluded.webhook_secret,
			updated_at = now()
		returning
			provider,
			provider_app_id,
			app_slug,
			name,
			html_url,
			private_key,
			webhook_secret
	`,
		config.Provider,
		config.AppID,
		config.AppSlug,
		config.Name,
		config.HTMLURL,
		config.PrivateKey,
		config.WebhookSecret,
	))
	if err != nil {
		return domain.CodeSourceAppConfig{}, fmt.Errorf("upsert code source app config: %w", err)
	}
	return saved, nil
}

func (r *CodeSourceRepository) DeleteCodeSourceAppConfig(ctx context.Context, provider domain.CodeSourceProvider) error {
	commandTag, err := r.pool.Exec(ctx, `
		delete from code_source_app_configs
		where provider = $1
	`, provider)
	if err != nil {
		return fmt.Errorf("delete code source app config: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return domain.ErrCodeSourceAuthNotConfigured
	}
	return nil
}

func (r *CodeSourceRepository) HasOtherCodeSourceInstallationOwner(ctx context.Context, provider domain.CodeSourceProvider, ownerUnionID string, installationID int64) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `
		select exists(
			select 1
			from code_source_installations
			where provider = $1
			  and provider_installation_id = $2
			  and owner_union_id <> $3
			  and status = 'active'
		)
	`, provider, installationID, ownerUnionID).Scan(&exists); err != nil {
		return false, fmt.Errorf("query other code source installation owner: %w", err)
	}
	return exists, nil
}

func (r *CodeSourceRepository) DeleteCodeSourceInstallations(ctx context.Context, provider domain.CodeSourceProvider, ownerUnionID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete code source installations: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	if _, err := tx.Exec(ctx, `
		delete from code_source_repositories
		where provider = $1 and owner_union_id = $2
	`, provider, ownerUnionID); err != nil {
		return fmt.Errorf("delete code source repositories: %w", err)
	}

	installationCommandTag, err := tx.Exec(ctx, `
		delete from code_source_installations
		where provider = $1 and owner_union_id = $2
	`, provider, ownerUnionID)
	if err != nil {
		return fmt.Errorf("delete code source installations: %w", err)
	}

	if installationCommandTag.RowsAffected() == 0 {
		return domain.ErrCodeSourceInstallationNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete code source installations: %w", err)
	}
	return nil
}

func (r *CodeSourceRepository) UpsertCodeSourceRepositories(ctx context.Context, ownerUnionID string, repositories []domain.CodeSourceRepository) error {
	if len(repositories) == 0 {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin upsert code source repositories: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	for _, repository := range repositories {
		if _, err := tx.Exec(ctx, `
			insert into code_source_repositories (
				provider,
				owner_union_id,
				provider_repository_id,
				provider_installation_id,
				owner,
				name,
				full_name,
				private,
				html_url,
				clone_url,
				ssh_url,
				default_branch,
				description,
				repository_updated_at
			) values (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
			)
			on conflict (provider, owner_union_id, provider_repository_id) do update set
				provider_installation_id = excluded.provider_installation_id,
				owner = excluded.owner,
				name = excluded.name,
				full_name = excluded.full_name,
				private = excluded.private,
				html_url = excluded.html_url,
				clone_url = excluded.clone_url,
				ssh_url = excluded.ssh_url,
				default_branch = excluded.default_branch,
				description = excluded.description,
				repository_updated_at = excluded.repository_updated_at,
				last_synced_at = now(),
				updated_at = now()
		`,
			repository.Provider,
			ownerUnionID,
			repository.RepositoryID,
			repository.InstallationID,
			repository.Owner,
			repository.Name,
			repository.FullName,
			repository.Private,
			repository.HTMLURL,
			repository.CloneURL,
			repository.SSHURL,
			repository.DefaultBranch,
			repository.Description,
			repository.UpdatedAt,
		); err != nil {
			return fmt.Errorf("upsert code source repository %s: %w", repository.FullName, err)
		}

		if _, err := tx.Exec(ctx, `
			update modules
			set provider_installation_id = $3,
			    updated_at = now()
			where repository_provider = $1
			  and provider_repository_id = $2
		`, repository.Provider, repository.RepositoryID, repository.InstallationID); err != nil {
			return fmt.Errorf("refresh module repository installation %s: %w", repository.FullName, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit upsert code source repositories: %w", err)
	}
	return nil
}

type codeSourceInstallationRow interface {
	Scan(dest ...any) error
}

type codeSourceAppConfigRow interface {
	Scan(dest ...any) error
}

func scanCodeSourceAppConfig(row codeSourceAppConfigRow) (domain.CodeSourceAppConfig, error) {
	var config domain.CodeSourceAppConfig
	if err := row.Scan(
		&config.Provider,
		&config.AppID,
		&config.AppSlug,
		&config.Name,
		&config.HTMLURL,
		&config.PrivateKey,
		&config.WebhookSecret,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CodeSourceAppConfig{}, domain.ErrCodeSourceAuthNotConfigured
		}
		return domain.CodeSourceAppConfig{}, fmt.Errorf("scan code source app config: %w", err)
	}
	return config, nil
}

func scanCodeSourceInstallation(row codeSourceInstallationRow) (domain.CodeSourceInstallation, error) {
	var installation domain.CodeSourceInstallation
	var deletedAt sql.NullTime
	if err := row.Scan(
		&installation.Provider,
		&installation.OwnerUnionID,
		&installation.InstallationID,
		&installation.AccountLogin,
		&installation.AccountType,
		&installation.RepositorySelection,
		&installation.InstalledAt,
		&installation.UpdatedAt,
		&deletedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CodeSourceInstallation{}, domain.ErrCodeSourceInstallationNotFound
		}
		return domain.CodeSourceInstallation{}, fmt.Errorf("scan code source installation: %w", err)
	}
	if deletedAt.Valid {
		installation.DeletedAt = &deletedAt.Time
	}
	return installation, nil
}
