package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

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

// GitHub App 等平台应用配置归租户，读写都要求当前 context 里有具体租户。
func (r *CodeSourceRepository) GetCodeSourceAppConfig(ctx context.Context, provider domain.CodeSourceProvider) (domain.CodeSourceAppConfig, error) {
	tenantID, err := domain.TenantIDFromContext(ctx)
	if err != nil {
		return domain.CodeSourceAppConfig{}, err
	}
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
		where provider = $1 and tenant_id = $2
	`, provider, tenantID))
}

func (r *CodeSourceRepository) UpsertCodeSourceAppConfig(ctx context.Context, config domain.CodeSourceAppConfig) (domain.CodeSourceAppConfig, error) {
	tenantID, err := domain.TenantIDFromContext(ctx)
	if err != nil {
		return domain.CodeSourceAppConfig{}, err
	}
	saved, err := scanCodeSourceAppConfig(r.pool.QueryRow(ctx, `
		insert into code_source_app_configs (
			tenant_id,
			provider,
			provider_app_id,
			app_slug,
			name,
			html_url,
			private_key,
			webhook_secret
		) values (
			$8, $1, $2, $3, $4, $5, $6, $7
		)
		on conflict (tenant_id, provider) do update set
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
		tenantID,
	))
	if err != nil {
		return domain.CodeSourceAppConfig{}, fmt.Errorf("upsert code source app config: %w", err)
	}
	return saved, nil
}

func (r *CodeSourceRepository) DeleteCodeSourceAppConfig(ctx context.Context, provider domain.CodeSourceProvider) error {
	tenantID, err := domain.TenantIDFromContext(ctx)
	if err != nil {
		return err
	}
	commandTag, err := r.pool.Exec(ctx, `
		delete from code_source_app_configs
		where provider = $1 and tenant_id = $2
	`, provider, tenantID)
	if err != nil {
		return fmt.Errorf("delete code source app config: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return domain.ErrCodeSourceAuthNotConfigured
	}
	return nil
}

func (r *CodeSourceRepository) ListCodeSourceConnections(ctx context.Context, filters domain.CodeSourceConnectionFilters) ([]domain.CodeSourceConnection, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, codeSourceConnectionSelectSQL()+`
		where c.status <> 'deleted'
			and ($1 = '' or c.provider = $1)
			and ($2 = '' or c.status = $2)
			and ($3::text = '' or c.tenant_id = $3)
		order by c.created_at desc, c.id desc
	`, string(filters.Provider), string(filters.Status), tenantID)
	if err != nil {
		return nil, fmt.Errorf("query code source connections: %w", err)
	}
	defer rows.Close()

	connections := make([]domain.CodeSourceConnection, 0)
	for rows.Next() {
		conn, err := scanCodeSourceConnection(rows)
		if err != nil {
			return nil, err
		}
		connections = append(connections, conn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate code source connections: %w", err)
	}
	return connections, nil
}

func (r *CodeSourceRepository) GetCodeSourceConnection(ctx context.Context, id string) (domain.CodeSourceConnection, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return domain.CodeSourceConnection{}, err
	}
	return scanCodeSourceConnection(r.pool.QueryRow(ctx, codeSourceConnectionSelectSQL()+`
		where c.id = $1 and c.status <> 'deleted' and ($2::text = '' or c.tenant_id = $2)
	`, id, tenantID))
}

func (r *CodeSourceRepository) FindCodeSourceConnectionByExternalID(ctx context.Context, provider domain.CodeSourceProvider, externalID string) (domain.CodeSourceConnection, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return domain.CodeSourceConnection{}, err
	}
	return scanCodeSourceConnection(r.pool.QueryRow(ctx, codeSourceConnectionSelectSQL()+`
		where c.provider = $1
			and c.external_id = $2
			and c.external_id <> ''
			and c.status <> 'deleted'
			and ($3::text = '' or c.tenant_id = $3)
		order by c.created_at desc
		limit 1
	`, provider, externalID, tenantID))
}

func (r *CodeSourceRepository) CreateCodeSourceConnection(ctx context.Context, conn domain.CodeSourceConnection) (domain.CodeSourceConnection, error) {
	tenantID, err := domain.TenantIDFromContext(ctx)
	if err != nil {
		return domain.CodeSourceConnection{}, err
	}
	id := newCodeSourceConnectionID()
	if _, err := r.pool.Exec(ctx, `
		insert into code_source_connections (
			tenant_id,
			id,
			provider,
			name,
			web_base_url,
			api_base_url,
			external_account,
			external_id,
			credential_encrypted,
			credential_preview,
			credential_expires_at,
			webhook_secret_encrypted,
			status,
			created_by_user_id,
			last_verified_at
		) values (
			$15, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)
	`,
		id,
		conn.Provider,
		conn.Name,
		conn.WebBaseURL,
		conn.APIBaseURL,
		conn.ExternalAccount,
		conn.ExternalID,
		conn.CredentialEncrypted,
		conn.CredentialPreview,
		conn.CredentialExpiresAt,
		conn.WebhookSecretEncrypted,
		conn.Status,
		conn.CreatedByUserID,
		conn.LastVerifiedAt,
		tenantID,
	); err != nil {
		switch {
		case isUniqueViolation(err):
			return domain.CodeSourceConnection{}, domain.ErrCodeSourceConnectionConflict
		case isForeignKeyViolation(err), isCheckViolation(err):
			return domain.CodeSourceConnection{}, domain.ErrInvalidCodeSourceInput
		}
		return domain.CodeSourceConnection{}, fmt.Errorf("insert code source connection: %w", err)
	}
	return r.GetCodeSourceConnection(ctx, id)
}

func (r *CodeSourceRepository) UpdateCodeSourceConnection(ctx context.Context, conn domain.CodeSourceConnection) (domain.CodeSourceConnection, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return domain.CodeSourceConnection{}, err
	}
	commandTag, err := r.pool.Exec(ctx, `
		update code_source_connections set
			name = $2,
			external_account = $3,
			credential_encrypted = $4,
			credential_preview = $5,
			credential_expires_at = $6,
			webhook_secret_encrypted = $7,
			status = $8,
			last_verified_at = $9,
			updated_at = now()
		where id = $1 and status <> 'deleted' and ($10::text = '' or tenant_id = $10)
	`,
		conn.ID,
		conn.Name,
		conn.ExternalAccount,
		conn.CredentialEncrypted,
		conn.CredentialPreview,
		conn.CredentialExpiresAt,
		conn.WebhookSecretEncrypted,
		conn.Status,
		conn.LastVerifiedAt,
		tenantID,
	)
	if err != nil {
		if isCheckViolation(err) {
			return domain.CodeSourceConnection{}, domain.ErrInvalidCodeSourceInput
		}
		return domain.CodeSourceConnection{}, fmt.Errorf("update code source connection: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return domain.CodeSourceConnection{}, domain.ErrCodeSourceConnectionNotFound
	}
	return r.GetCodeSourceConnection(ctx, conn.ID)
}

func (r *CodeSourceRepository) DeleteCodeSourceConnection(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete code source connection: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return err
	}
	var status string
	if err := tx.QueryRow(ctx, `
		select status from code_source_connections where id = $1 and ($2::text = '' or tenant_id = $2) for update
	`, id, tenantID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrCodeSourceConnectionNotFound
		}
		return fmt.Errorf("query code source connection for delete: %w", err)
	}
	if status == string(domain.CodeSourceConnectionStatusDeleted) {
		return domain.ErrCodeSourceConnectionNotFound
	}

	var inUse bool
	if err := tx.QueryRow(ctx, `
		select exists(select 1 from modules where code_source_connection_id = $1)
	`, id).Scan(&inUse); err != nil {
		return fmt.Errorf("query code source connection modules: %w", err)
	}
	if inUse {
		return domain.ErrCodeSourceConnectionInUse
	}

	// 软删除保留创建人等审计信息，同时清空凭证密文和仓库缓存。
	if _, err := tx.Exec(ctx, `
		update code_source_connections set
			status = 'deleted',
			credential_encrypted = '',
			webhook_secret_encrypted = '',
			updated_at = now()
		where id = $1
	`, id); err != nil {
		return fmt.Errorf("delete code source connection: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		delete from code_source_repositories where connection_id = $1
	`, id); err != nil {
		return fmt.Errorf("delete code source repositories: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete code source connection: %w", err)
	}
	return nil
}

func (r *CodeSourceRepository) UpsertCodeSourceRepositories(ctx context.Context, repositories []domain.CodeSourceRepository) error {
	if len(repositories) == 0 {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin upsert code source repositories: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	for _, repository := range repositories {
		syncedAt := repository.LastSyncedAt
		if syncedAt.IsZero() {
			syncedAt = time.Now().UTC()
		}
		if _, err := tx.Exec(ctx, `
			insert into code_source_repositories (
				connection_id,
				external_repository_id,
				full_name,
				web_url,
				clone_url,
				default_branch,
				private,
				description,
				last_synced_at
			) values (
				$1, $2, $3, $4, $5, $6, $7, $8, $9
			)
			on conflict (connection_id, external_repository_id) do update set
				full_name = excluded.full_name,
				web_url = excluded.web_url,
				clone_url = excluded.clone_url,
				default_branch = excluded.default_branch,
				private = excluded.private,
				description = excluded.description,
				last_synced_at = excluded.last_synced_at
		`,
			repository.ConnectionID,
			repository.ExternalRepositoryID,
			repository.FullName,
			repository.WebURL,
			repository.CloneURL,
			repository.DefaultBranch,
			repository.Private,
			repository.Description,
			syncedAt,
		); err != nil {
			if isForeignKeyViolation(err) {
				return domain.ErrCodeSourceConnectionNotFound
			}
			return fmt.Errorf("upsert code source repository %s: %w", repository.FullName, err)
		}

		// 仓库改名或迁移后，同步刷新已绑定模块上的仓库快照，保证 worker 使用最新的 clone 地址。
		if _, err := tx.Exec(ctx, `
			update modules set
				repository_full_name = $3,
				repository_web_url = $4,
				repository_clone_url = $5,
				updated_at = now()
			where code_source_connection_id = $1
				and external_repository_id = $2
				and (repository_full_name, repository_web_url, repository_clone_url) is distinct from ($3, $4, $5)
		`, repository.ConnectionID, repository.ExternalRepositoryID, repository.FullName, repository.WebURL, repository.CloneURL); err != nil {
			return fmt.Errorf("refresh module repository %s: %w", repository.FullName, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit upsert code source repositories: %w", err)
	}
	return nil
}

func (r *CodeSourceRepository) GetCodeSourceRepository(ctx context.Context, connectionID string, externalRepositoryID string) (domain.CodeSourceRepository, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return domain.CodeSourceRepository{}, err
	}
	var repository domain.CodeSourceRepository
	if err := r.pool.QueryRow(ctx, `
		select
			r.connection_id,
			r.external_repository_id,
			r.full_name,
			r.web_url,
			r.clone_url,
			r.default_branch,
			r.private,
			r.description,
			r.last_synced_at
		from code_source_repositories r
		join code_source_connections c on c.id = r.connection_id
		where r.connection_id = $1 and r.external_repository_id = $2 and ($3::text = '' or c.tenant_id = $3)
	`, connectionID, externalRepositoryID, tenantID).Scan(
		&repository.ConnectionID,
		&repository.ExternalRepositoryID,
		&repository.FullName,
		&repository.WebURL,
		&repository.CloneURL,
		&repository.DefaultBranch,
		&repository.Private,
		&repository.Description,
		&repository.LastSyncedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CodeSourceRepository{}, domain.ErrCodeSourceRepositoryNotFound
		}
		return domain.CodeSourceRepository{}, fmt.Errorf("query code source repository: %w", err)
	}
	return repository, nil
}

type codeSourceRow interface {
	Scan(dest ...any) error
}

func codeSourceConnectionSelectSQL() string {
	return `
		select
			c.id,
			c.provider,
			c.name,
			c.web_base_url,
			c.api_base_url,
			c.external_account,
			c.external_id,
			c.credential_encrypted,
			c.credential_preview,
			c.credential_expires_at,
			c.webhook_secret_encrypted,
			c.status,
			c.created_by_user_id,
			u.name as created_by,
			c.last_verified_at,
			c.created_at,
			c.updated_at
		from code_source_connections c
		join users u on u.id = c.created_by_user_id`
}

func scanCodeSourceConnection(row codeSourceRow) (domain.CodeSourceConnection, error) {
	var conn domain.CodeSourceConnection
	var credentialExpiresAt sql.NullTime
	var lastVerifiedAt sql.NullTime
	if err := row.Scan(
		&conn.ID,
		&conn.Provider,
		&conn.Name,
		&conn.WebBaseURL,
		&conn.APIBaseURL,
		&conn.ExternalAccount,
		&conn.ExternalID,
		&conn.CredentialEncrypted,
		&conn.CredentialPreview,
		&credentialExpiresAt,
		&conn.WebhookSecretEncrypted,
		&conn.Status,
		&conn.CreatedByUserID,
		&conn.CreatedBy,
		&lastVerifiedAt,
		&conn.CreatedAt,
		&conn.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CodeSourceConnection{}, domain.ErrCodeSourceConnectionNotFound
		}
		return domain.CodeSourceConnection{}, fmt.Errorf("scan code source connection: %w", err)
	}
	if credentialExpiresAt.Valid {
		conn.CredentialExpiresAt = &credentialExpiresAt.Time
	}
	if lastVerifiedAt.Valid {
		conn.LastVerifiedAt = &lastVerifiedAt.Time
	}
	return conn, nil
}

func scanCodeSourceAppConfig(row codeSourceRow) (domain.CodeSourceAppConfig, error) {
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

func newCodeSourceConnectionID() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err == nil {
		return "code-source-" + hex.EncodeToString(value[:])
	}
	return "code-source-" + time.Now().UTC().Format("20060102150405.000000000")
}
