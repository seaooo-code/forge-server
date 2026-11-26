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

type ArtifactAccessTokenRepository struct {
	pool *pgxpool.Pool
}

func NewArtifactAccessTokenRepository(pool *pgxpool.Pool) *ArtifactAccessTokenRepository {
	return &ArtifactAccessTokenRepository{pool: pool}
}

func (r *ArtifactAccessTokenRepository) ListArtifactAccessTokens(ctx context.Context) ([]domain.ArtifactAccessToken, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		select
			t.id,
			t.tenant_id,
			t.name,
			t.token_hash,
			t.token_preview,
			t.status,
			t.created_by_user_id,
			u.name as created_by,
			t.created_at,
			t.updated_at,
			t.last_used_at,
			t.expires_at
		from artifact_access_tokens t
		join users u on u.id = t.created_by_user_id
		where $1::text = '' or t.tenant_id = $1
		order by t.created_at desc
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query artifact access tokens: %w", err)
	}
	defer rows.Close()

	tokens := make([]domain.ArtifactAccessToken, 0)
	for rows.Next() {
		token, err := scanArtifactAccessToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifact access tokens: %w", err)
	}
	return tokens, nil
}

func (r *ArtifactAccessTokenRepository) CreateArtifactAccessToken(ctx context.Context, input domain.ArtifactAccessTokenStoredInput) (domain.ArtifactAccessToken, error) {
	for attempt := 0; attempt < 3; attempt++ {
		token, err := r.createArtifactAccessToken(ctx, newArtifactAccessTokenID(), input)
		if err == nil {
			return token, nil
		}
		if isUniqueViolation(err) {
			if tokenHashExists(ctx, r.pool, input.TokenHash) {
				return domain.ArtifactAccessToken{}, domain.ErrArtifactAccessTokenAlreadyExists
			}
			continue
		}
		return domain.ArtifactAccessToken{}, err
	}
	return domain.ArtifactAccessToken{}, fmt.Errorf("insert artifact access token: id conflict")
}

func (r *ArtifactAccessTokenRepository) createArtifactAccessToken(ctx context.Context, id string, input domain.ArtifactAccessTokenStoredInput) (domain.ArtifactAccessToken, error) {
	tenantID, err := domain.TenantIDFromContext(ctx)
	if err != nil {
		return domain.ArtifactAccessToken{}, err
	}
	var tokenID string
	if err := r.pool.QueryRow(ctx, `
		insert into artifact_access_tokens (
			tenant_id,
			id,
			name,
			token_hash,
			token_preview,
			status,
			created_by_user_id,
			expires_at
		) values ($8, $1, $2, $3, $4, $5, $6, $7)
		returning id
	`, id, input.Name, input.TokenHash, input.TokenPreview, input.Status, input.CreatedByUserID, input.ExpiresAt, tenantID).Scan(&tokenID); err != nil {
		if isUniqueViolation(err) {
			return domain.ArtifactAccessToken{}, err
		}
		return domain.ArtifactAccessToken{}, fmt.Errorf("insert artifact access token: %w", err)
	}
	return r.getArtifactAccessToken(ctx, tokenID)
}

func (r *ArtifactAccessTokenRepository) UpdateArtifactAccessToken(ctx context.Context, id string, input domain.ArtifactAccessTokenUpdateInput) (domain.ArtifactAccessToken, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return domain.ArtifactAccessToken{}, err
	}
	var tokenID string
	if err := r.pool.QueryRow(ctx, `
		update artifact_access_tokens set
			status = $2,
			updated_at = now()
		where id = $1 and ($3::text = '' or tenant_id = $3)
		returning id
	`, id, input.Status, tenantID).Scan(&tokenID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ArtifactAccessToken{}, domain.ErrArtifactAccessTokenNotFound
		}
		return domain.ArtifactAccessToken{}, fmt.Errorf("update artifact access token: %w", err)
	}
	return r.getArtifactAccessToken(ctx, tokenID)
}

func (r *ArtifactAccessTokenRepository) DeleteArtifactAccessToken(ctx context.Context, id string) error {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		delete from artifact_access_tokens where id = $1 and ($2::text = '' or tenant_id = $2)
	`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete artifact access token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrArtifactAccessTokenNotFound
	}
	return nil
}

func (r *ArtifactAccessTokenRepository) FindArtifactAccessTokenByHash(ctx context.Context, tokenHash string) (domain.ArtifactAccessToken, error) {
	token, err := scanArtifactAccessToken(r.pool.QueryRow(ctx, `
		select
			t.id,
			t.tenant_id,
			t.name,
			t.token_hash,
			t.token_preview,
			t.status,
			t.created_by_user_id,
			u.name as created_by,
			t.created_at,
			t.updated_at,
			t.last_used_at,
			t.expires_at
		from artifact_access_tokens t
		join users u on u.id = t.created_by_user_id
		where t.token_hash = $1
	`, tokenHash))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ArtifactAccessToken{}, domain.ErrArtifactAccessTokenNotFound
		}
		return domain.ArtifactAccessToken{}, err
	}
	return token, nil
}

func (r *ArtifactAccessTokenRepository) TouchArtifactAccessToken(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `update artifact_access_tokens set last_used_at = now(), updated_at = now() where id = $1`, id)
	if err != nil {
		return fmt.Errorf("touch artifact access token: %w", err)
	}
	return nil
}

func (r *ArtifactAccessTokenRepository) getArtifactAccessToken(ctx context.Context, id string) (domain.ArtifactAccessToken, error) {
	token, err := scanArtifactAccessToken(r.pool.QueryRow(ctx, `
		select
			t.id,
			t.tenant_id,
			t.name,
			t.token_hash,
			t.token_preview,
			t.status,
			t.created_by_user_id,
			u.name as created_by,
			t.created_at,
			t.updated_at,
			t.last_used_at,
			t.expires_at
		from artifact_access_tokens t
		join users u on u.id = t.created_by_user_id
		where t.id = $1
	`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ArtifactAccessToken{}, domain.ErrArtifactAccessTokenNotFound
		}
		return domain.ArtifactAccessToken{}, err
	}
	return token, nil
}

type artifactAccessTokenScanner interface {
	Scan(dest ...any) error
}

func scanArtifactAccessToken(row artifactAccessTokenScanner) (domain.ArtifactAccessToken, error) {
	var token domain.ArtifactAccessToken
	var lastUsedAt sql.NullTime
	var expiresAt sql.NullTime
	if err := row.Scan(
		&token.ID,
		&token.TenantID,
		&token.Name,
		&token.TokenHash,
		&token.TokenPreview,
		&token.Status,
		&token.CreatedByUserID,
		&token.CreatedBy,
		&token.CreatedAt,
		&token.UpdatedAt,
		&lastUsedAt,
		&expiresAt,
	); err != nil {
		return domain.ArtifactAccessToken{}, err
	}
	if lastUsedAt.Valid {
		token.LastUsedAt = &lastUsedAt.Time
	}
	if expiresAt.Valid {
		token.ExpiresAt = &expiresAt.Time
	}
	return token, nil
}

func tokenHashExists(ctx context.Context, pool *pgxpool.Pool, tokenHash string) bool {
	var exists bool
	if err := pool.QueryRow(ctx, `select exists(select 1 from artifact_access_tokens where token_hash = $1)`, tokenHash).Scan(&exists); err != nil {
		return false
	}
	return exists
}

func newArtifactAccessTokenID() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err == nil {
		return "artifact-token-" + hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("artifact-token-%d", time.Now().UnixNano())
}
