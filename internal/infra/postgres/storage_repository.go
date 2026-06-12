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

func (r *StorageRepository) ListStorageConfigurations(ctx context.Context) ([]domain.StorageConfiguration, error) {
	rows, err := r.pool.Query(ctx, `
		select
			id,
			name,
			adapter,
			bucket,
			endpoint,
			region,
			owner,
			public_base_url,
			status,
			last_verified_at,
			created_at,
			updated_at
		from storage_configurations
		order by sort_order asc, created_at asc
	`)
	if err != nil {
		return nil, fmt.Errorf("query storage configurations: %w", err)
	}
	defer rows.Close()

	configurations := make([]domain.StorageConfiguration, 0)
	for rows.Next() {
		configuration, err := scanStorageConfiguration(rows)
		if err != nil {
			return nil, err
		}
		configurations = append(configurations, configuration)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate storage configurations: %w", err)
	}

	return configurations, nil
}

func (r *StorageRepository) GetStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error) {
	configuration, err := scanStorageConfiguration(r.pool.QueryRow(ctx, `
		select
			id,
			name,
			adapter,
			bucket,
			endpoint,
			region,
			owner,
			public_base_url,
			status,
			last_verified_at,
			created_at,
			updated_at
		from storage_configurations
		where id = $1
	`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.StorageConfiguration{}, domain.ErrStorageConfigurationNotFound
		}
		return domain.StorageConfiguration{}, err
	}
	return configuration, nil
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
	configuration, err := scanStorageConfiguration(r.pool.QueryRow(ctx, `
		insert into storage_configurations (
			id,
			name,
			adapter,
			bucket,
			endpoint,
			region,
			owner,
			public_base_url,
			access_key_id,
			secret_access_key,
			status,
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
			coalesce((select max(sort_order) + 10 from storage_configurations), 10)
		)
		returning
			id,
			name,
			adapter,
			bucket,
			endpoint,
			region,
			owner,
			public_base_url,
			status,
			last_verified_at,
			created_at,
			updated_at
	`,
		id,
		input.Name,
		input.Adapter,
		input.Bucket,
		input.Endpoint,
		input.Region,
		input.Owner,
		input.PublicBaseURL,
		input.AccessKeyID,
		input.SecretAccessKey,
		input.Status,
	))
	if err != nil {
		if isUniqueViolation(err) {
			return domain.StorageConfiguration{}, err
		}
		return domain.StorageConfiguration{}, fmt.Errorf("insert storage configuration: %w", err)
	}
	return configuration, nil
}

func (r *StorageRepository) UpdateStorageConfiguration(ctx context.Context, id string, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error) {
	configuration, err := scanStorageConfiguration(r.pool.QueryRow(ctx, `
		update storage_configurations set
			name = $2,
			adapter = $3,
			bucket = $4,
			endpoint = $5,
			region = $6,
			owner = $7,
			public_base_url = $8,
			access_key_id = case when $9 <> '' then $9 else access_key_id end,
			secret_access_key = case when $10 <> '' then $10 else secret_access_key end,
			status = $11,
			updated_at = now()
		where id = $1
		returning
			id,
			name,
			adapter,
			bucket,
			endpoint,
			region,
			owner,
			public_base_url,
			status,
			last_verified_at,
			created_at,
			updated_at
	`,
		id,
		input.Name,
		input.Adapter,
		input.Bucket,
		input.Endpoint,
		input.Region,
		input.Owner,
		input.PublicBaseURL,
		input.AccessKeyID,
		input.SecretAccessKey,
		input.Status,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.StorageConfiguration{}, domain.ErrStorageConfigurationNotFound
		}
		return domain.StorageConfiguration{}, fmt.Errorf("update storage configuration: %w", err)
	}
	return configuration, nil
}

func (r *StorageRepository) DeleteStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error) {
	configuration, err := scanStorageConfiguration(r.pool.QueryRow(ctx, `
		delete from storage_configurations
		where id = $1
		returning
			id,
			name,
			adapter,
			bucket,
			endpoint,
			region,
			owner,
			public_base_url,
			status,
			last_verified_at,
			created_at,
			updated_at
	`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.StorageConfiguration{}, domain.ErrStorageConfigurationNotFound
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
		&configuration.Owner,
		&configuration.PublicBaseURL,
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
