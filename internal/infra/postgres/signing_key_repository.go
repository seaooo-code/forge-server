package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"forge-server/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SigningKeyRepository 保存应用的包签名密钥，规则见 docs/artifact-signing.md。私钥密文只在 ActiveSigningKey 里返回。
type SigningKeyRepository struct {
	pool *pgxpool.Pool
}

func NewSigningKeyRepository(pool *pgxpool.Pool) *SigningKeyRepository {
	return &SigningKeyRepository{pool: pool}
}

func (r *SigningKeyRepository) GetApplicationSigning(ctx context.Context, applicationSlug string) (domain.ApplicationSigning, error) {
	var applicationID string
	var signing domain.ApplicationSigning
	if err := r.pool.QueryRow(ctx, `select id, require_signed_releases from applications where slug = $1`, applicationSlug).
		Scan(&applicationID, &signing.RequireSignedReleases); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ApplicationSigning{}, domain.ErrApplicationNotFound
		}
		return domain.ApplicationSigning{}, fmt.Errorf("query application signing: %w", err)
	}
	rows, err := r.pool.Query(ctx, signingKeySelectSQL+`
		where k.application_id = $1
		order by case when k.status = 'active' then 0 else 1 end, k.created_at desc
	`, applicationID)
	if err != nil {
		return domain.ApplicationSigning{}, fmt.Errorf("query signing keys: %w", err)
	}
	defer rows.Close()
	signing.Keys = []domain.SigningKey{}
	for rows.Next() {
		key, err := scanSigningKey(rows)
		if err != nil {
			return domain.ApplicationSigning{}, err
		}
		signing.Keys = append(signing.Keys, key)
	}
	if err := rows.Err(); err != nil {
		return domain.ApplicationSigning{}, fmt.Errorf("iterate signing keys: %w", err)
	}
	return signing, nil
}

// CreateSigningKey 保存新的 active 密钥，并在同一个事务里把原 active 密钥转为 retired。
func (r *SigningKeyRepository) CreateSigningKey(ctx context.Context, input domain.NewSigningKeyInput) (domain.SigningKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.SigningKey{}, fmt.Errorf("begin create signing key: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	var applicationID string
	// 锁住应用行，两个人同时生成密钥时串行执行，保证只剩一把 active。
	if err := tx.QueryRow(ctx, `select id from applications where slug = $1 for update`, input.ApplicationSlug).Scan(&applicationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.SigningKey{}, domain.ErrApplicationNotFound
		}
		return domain.SigningKey{}, fmt.Errorf("lock application for signing key: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update application_signing_keys
		set status = 'retired', retired_at = now()
		where application_id = $1 and status = 'active'
	`, applicationID); err != nil {
		return domain.SigningKey{}, fmt.Errorf("retire active signing key: %w", err)
	}
	id := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		insert into application_signing_keys (id, application_id, key_id, algorithm, public_key, private_key_ciphertext, status, created_by_user_id)
		values ($1, $2, $3, $4, $5, $6, 'active', nullif($7, ''))
	`, id, applicationID, input.KeyID, domain.ArtifactSigningAlgorithm, input.PublicKey, input.PrivateKeyCiphertext, input.CreatedByUserID); err != nil {
		if isUniqueViolation(err) || isCheckViolation(err) || isForeignKeyViolation(err) {
			return domain.SigningKey{}, domain.ErrInvalidSigningInput
		}
		return domain.SigningKey{}, fmt.Errorf("insert signing key: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SigningKey{}, fmt.Errorf("commit create signing key: %w", err)
	}
	return scanSigningKey(r.pool.QueryRow(ctx, signingKeySelectSQL+` where k.id = $1`, id))
}

// DeleteSigningKey 只删除 retired 密钥。
func (r *SigningKeyRepository) DeleteSigningKey(ctx context.Context, applicationSlug string, keyID string) error {
	var status domain.SigningKeyStatus
	if err := r.pool.QueryRow(ctx, `
		select k.status
		from application_signing_keys k
		join applications a on a.id = k.application_id
		where a.slug = $1 and k.key_id = $2
	`, applicationSlug, keyID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := applicationIDBySlug(ctx, r.pool, applicationSlug); err != nil {
				return err
			}
			return domain.ErrSigningKeyNotFound
		}
		return fmt.Errorf("query signing key: %w", err)
	}
	if status == domain.SigningKeyStatusActive {
		return domain.ErrSigningKeyActive
	}
	if _, err := r.pool.Exec(ctx, `delete from application_signing_keys where key_id = $1 and status = 'retired'`, keyID); err != nil {
		return fmt.Errorf("delete signing key: %w", err)
	}
	return nil
}

// SetRequireSignedReleases 修改「只允许发布已签名的构建」。打开时要求应用有 active 密钥；锁住应用行，避免和生成密钥并发。
func (r *SigningKeyRepository) SetRequireSignedReleases(ctx context.Context, applicationSlug string, require bool) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin update require signed releases: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	var applicationID string
	var hasActiveKey bool
	if err := tx.QueryRow(ctx, `
		select a.id, exists(select 1 from application_signing_keys k where k.application_id = a.id and k.status = 'active')
		from applications a
		where a.slug = $1
		for update of a
	`, applicationSlug).Scan(&applicationID, &hasActiveKey); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrApplicationNotFound
		}
		return fmt.Errorf("query application for require signed releases: %w", err)
	}
	if require && !hasActiveKey {
		return domain.ErrSigningKeyRequired
	}
	if _, err := tx.Exec(ctx, `update applications set require_signed_releases = $2, updated_at = now() where id = $1`, applicationID, require); err != nil {
		return fmt.Errorf("update require signed releases: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit require signed releases: %w", err)
	}
	return nil
}

// ActiveSigningKey 返回应用的 active 密钥和私钥密文；应用没有 active 密钥时 ok 为 false。
func (r *SigningKeyRepository) ActiveSigningKey(ctx context.Context, applicationSlug string) (domain.ActiveSigningKey, bool, error) {
	var key domain.ActiveSigningKey
	err := r.pool.QueryRow(ctx, `
		select k.key_id, k.private_key_ciphertext
		from application_signing_keys k
		join applications a on a.id = k.application_id
		where a.slug = $1 and k.status = 'active'
	`, applicationSlug).Scan(&key.KeyID, &key.PrivateKeyCiphertext)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ActiveSigningKey{}, false, nil
	}
	if err != nil {
		return domain.ActiveSigningKey{}, false, fmt.Errorf("query active signing key: %w", err)
	}
	return key, true, nil
}

const signingKeySelectSQL = `
	select
		k.id,
		k.application_id,
		k.key_id,
		k.algorithm,
		k.public_key,
		k.status,
		coalesce(k.created_by_user_id, ''),
		coalesce(u.name, ''),
		k.created_at,
		k.retired_at,
		(select count(*)::integer from build_records br where br.signing_key_id = k.key_id)
	from application_signing_keys k
	left join users u on u.id = k.created_by_user_id
`

func scanSigningKey(row pgx.Row) (domain.SigningKey, error) {
	var key domain.SigningKey
	var retiredAt sql.NullTime
	if err := row.Scan(&key.ID, &key.ApplicationID, &key.KeyID, &key.Algorithm, &key.PublicKey, &key.Status,
		&key.CreatedByUserID, &key.CreatedBy, &key.CreatedAt, &retiredAt, &key.SignedBuilds); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.SigningKey{}, domain.ErrSigningKeyNotFound
		}
		return domain.SigningKey{}, fmt.Errorf("scan signing key: %w", err)
	}
	if retiredAt.Valid {
		key.RetiredAt = &retiredAt.Time
	}
	return key, nil
}
