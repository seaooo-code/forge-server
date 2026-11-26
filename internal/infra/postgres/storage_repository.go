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

type StorageRepository struct {
	pool *pgxpool.Pool
}

func NewStorageRepository(pool *pgxpool.Pool) *StorageRepository {
	return &StorageRepository{pool: pool}
}

func (r *StorageRepository) ListStorageConfigurations(ctx context.Context, pagination domain.Pagination) ([]domain.StorageConfiguration, int, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return nil, 0, err
	}
	var total int
	if err := r.pool.QueryRow(ctx, `
		select count(*)::integer from storage_configurations where $1::text = '' or tenant_id = $1
	`, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count storage configurations: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		select
			c.id,
			c.name,
			c.adapter,
			c.bucket,
			c.endpoint,
			c.region,
			c.owner_user_id,
			u.name as owner,
			u.email as owner_email,
			u.avatar_url as owner_avatar_url,
			c.public_base_url,
			c.force_path_style,
			c.status,
			c.last_verified_at,
			c.created_at,
			c.updated_at
		from storage_configurations c
		join users u on u.id = c.owner_user_id
		where $3::text = '' or c.tenant_id = $3
		order by c.sort_order asc, c.created_at asc
		limit nullif($1, 0) offset $2
	`, pagination.Limit(), pagination.Offset(), tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("query storage configurations: %w", err)
	}
	defer rows.Close()

	configurations := make([]domain.StorageConfiguration, 0)
	for rows.Next() {
		configuration, err := scanStorageConfiguration(rows)
		if err != nil {
			return nil, 0, err
		}
		configurations = append(configurations, configuration)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate storage configurations: %w", err)
	}

	return configurations, total, nil
}

func (r *StorageRepository) GetStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return domain.StorageConfiguration{}, err
	}
	configuration, err := scanStorageConfiguration(r.pool.QueryRow(ctx, `
		select
			c.id,
			c.name,
			c.adapter,
			c.bucket,
			c.endpoint,
			c.region,
			c.owner_user_id,
			u.name as owner,
			u.email as owner_email,
			u.avatar_url as owner_avatar_url,
			c.public_base_url,
			c.force_path_style,
			c.status,
			c.last_verified_at,
			c.created_at,
			c.updated_at
		from storage_configurations c
		join users u on u.id = c.owner_user_id
		where c.id = $1 and ($2::text = '' or c.tenant_id = $2)
	`, id, tenantID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.StorageConfiguration{}, domain.ErrStorageConfigurationNotFound
		}
		return domain.StorageConfiguration{}, err
	}
	return configuration, nil
}

func (r *StorageRepository) GetStorageCredentials(ctx context.Context, id string) (domain.StorageCredentials, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return domain.StorageCredentials{}, err
	}
	var credentials domain.StorageCredentials
	if err := r.pool.QueryRow(ctx, `
		select access_key_id, secret_access_key
		from storage_configurations
		where id = $1 and ($2::text = '' or tenant_id = $2)
	`, id, tenantID).Scan(&credentials.AccessKeyID, &credentials.SecretAccessKey); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.StorageCredentials{}, domain.ErrStorageConfigurationNotFound
		}
		return domain.StorageCredentials{}, fmt.Errorf("get storage credentials: %w", err)
	}
	return credentials, nil
}

func (r *StorageRepository) CreateStorageConfiguration(ctx context.Context, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error) {
	for attempt := 0; attempt < 3; attempt++ {
		configuration, err := r.createStorageConfiguration(ctx, newStorageConfigurationID(), input)
		if err == nil {
			return configuration, nil
		}
		if isUniqueViolation(err) {
			continue
		}
		return domain.StorageConfiguration{}, err
	}

	return domain.StorageConfiguration{}, fmt.Errorf("insert storage configuration: id conflict")
}

func (r *StorageRepository) createStorageConfiguration(ctx context.Context, id string, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.StorageConfiguration{}, fmt.Errorf("begin create storage configuration: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	tenantID, err := domain.TenantIDFromContext(ctx)
	if err != nil {
		return domain.StorageConfiguration{}, err
	}
	var configurationID string
	if err := tx.QueryRow(ctx, `
		insert into storage_configurations (
			tenant_id,
			id,
			name,
			adapter,
			bucket,
			endpoint,
			region,
			owner_user_id,
			public_base_url,
			access_key_id,
			secret_access_key,
			status,
			force_path_style,
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
			coalesce((select max(sort_order) + 10 from storage_configurations where tenant_id = $1), 10)
		)
		returning id
	`,
		tenantID,
		id,
		input.Name,
		input.Adapter,
		input.Bucket,
		input.Endpoint,
		input.Region,
		input.OwnerUserID,
		input.PublicBaseURL,
		input.AccessKeyID,
		input.SecretAccessKey,
		input.Status,
		input.ForcePathStyle,
	).Scan(&configurationID); err != nil {
		if isUniqueViolation(err) {
			return domain.StorageConfiguration{}, err
		}
		if isForeignKeyViolation(err) {
			// 负责人不存在或不属于当前租户。
			return domain.StorageConfiguration{}, domain.ErrInvalidStorageConfigurationInput
		}
		return domain.StorageConfiguration{}, fmt.Errorf("insert storage configuration: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.StorageConfiguration{}, fmt.Errorf("commit create storage configuration: %w", err)
	}
	return r.GetStorageConfiguration(ctx, configurationID)
}

func (r *StorageRepository) UpdateStorageConfiguration(ctx context.Context, id string, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.StorageConfiguration{}, fmt.Errorf("begin update storage configuration: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return domain.StorageConfiguration{}, err
	}
	var configurationID string
	if err := tx.QueryRow(ctx, `
		update storage_configurations set
			name = $2,
			adapter = $3,
			bucket = $4,
			endpoint = $5,
			region = $6,
			owner_user_id = $7,
			public_base_url = $8,
			access_key_id = $9,
			secret_access_key = $10,
			status = $11,
			force_path_style = $12,
			updated_at = now()
		where id = $1 and ($13::text = '' or tenant_id = $13)
		returning id
	`,
		id,
		input.Name,
		input.Adapter,
		input.Bucket,
		input.Endpoint,
		input.Region,
		input.OwnerUserID,
		input.PublicBaseURL,
		input.AccessKeyID,
		input.SecretAccessKey,
		input.Status,
		input.ForcePathStyle,
		tenantID,
	).Scan(&configurationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.StorageConfiguration{}, domain.ErrStorageConfigurationNotFound
		}
		if isForeignKeyViolation(err) {
			return domain.StorageConfiguration{}, domain.ErrInvalidStorageConfigurationInput
		}
		return domain.StorageConfiguration{}, fmt.Errorf("update storage configuration: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.StorageConfiguration{}, fmt.Errorf("commit update storage configuration: %w", err)
	}
	return r.GetStorageConfiguration(ctx, configurationID)
}

func (r *StorageRepository) DeleteStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return domain.StorageConfiguration{}, err
	}
	var hasEnvironmentDependencies bool
	if err := r.pool.QueryRow(ctx, `
		select exists (
			select 1
			from environments
			where storage_configuration_id = $1
		)
	`, id).Scan(&hasEnvironmentDependencies); err != nil {
		return domain.StorageConfiguration{}, fmt.Errorf("query storage configuration dependencies: %w", err)
	}
	if hasEnvironmentDependencies {
		return domain.StorageConfiguration{}, domain.ErrStorageConfigurationInUse
	}

	configuration, err := scanStorageConfiguration(r.pool.QueryRow(ctx, `
		with deleted as (
			delete from storage_configurations
			where id = $1 and ($2::text = '' or tenant_id = $2)
			returning
				id,
				name,
				adapter,
				bucket,
				endpoint,
				region,
				owner_user_id,
				public_base_url,
				force_path_style,
				status,
				last_verified_at,
				created_at,
				updated_at
		)
		select
			d.id,
			d.name,
			d.adapter,
			d.bucket,
			d.endpoint,
			d.region,
			d.owner_user_id,
			u.name as owner,
			u.email as owner_email,
			u.avatar_url as owner_avatar_url,
			d.public_base_url,
			d.force_path_style,
			d.status,
			d.last_verified_at,
			d.created_at,
			d.updated_at
		from deleted d
		join users u on u.id = d.owner_user_id
	`, id, tenantID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.StorageConfiguration{}, domain.ErrStorageConfigurationNotFound
		}
		if isForeignKeyViolation(err) {
			return domain.StorageConfiguration{}, domain.ErrStorageConfigurationInUse
		}
		return domain.StorageConfiguration{}, fmt.Errorf("delete storage configuration: %w", err)
	}
	return configuration, nil
}

type storageConfigurationRow interface {
	Scan(dest ...any) error
}

func scanStorageConfiguration(row storageConfigurationRow) (domain.StorageConfiguration, error) {
	var configuration domain.StorageConfiguration
	var lastVerifiedAt sql.NullTime
	if err := row.Scan(
		&configuration.ID,
		&configuration.Name,
		&configuration.Adapter,
		&configuration.Bucket,
		&configuration.Endpoint,
		&configuration.Region,
		&configuration.OwnerUserID,
		&configuration.Owner,
		&configuration.OwnerEmail,
		&configuration.OwnerAvatarURL,
		&configuration.PublicBaseURL,
		&configuration.ForcePathStyle,
		&configuration.Status,
		&lastVerifiedAt,
		&configuration.CreatedAt,
		&configuration.UpdatedAt,
	); err != nil {
		return domain.StorageConfiguration{}, fmt.Errorf("scan storage configuration: %w", err)
	}
	if lastVerifiedAt.Valid {
		configuration.LastVerifiedAt = &lastVerifiedAt.Time
	}
	return configuration, nil
}

func newStorageConfigurationID() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err == nil {
		return "storage-" + hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("storage-%d", time.Now().UnixNano())
}
