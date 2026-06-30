package postgres

import (
	"context"
	"fmt"
	"time"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FeishuUserRepository struct {
	pool *pgxpool.Pool
}

func NewFeishuUserRepository(pool *pgxpool.Pool) *FeishuUserRepository {
	return &FeishuUserRepository{pool: pool}
}

func (r *FeishuUserRepository) UpsertFeishuUser(ctx context.Context, user domain.FeishuUser) (domain.FeishuUser, error) {
	if err := r.pool.QueryRow(ctx, `
		insert into feishu_users (
			union_id,
			open_id,
			user_id,
			name,
			en_name,
			avatar_url,
			avatar_thumb,
			avatar_middle,
			avatar_big,
			email,
			enterprise_email,
			mobile,
			tenant_key,
			employee_no
		) values (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14
		)
		on conflict (union_id) do update set
			open_id = excluded.open_id,
			user_id = excluded.user_id,
			name = excluded.name,
			en_name = excluded.en_name,
			avatar_url = excluded.avatar_url,
			avatar_thumb = excluded.avatar_thumb,
			avatar_middle = excluded.avatar_middle,
			avatar_big = excluded.avatar_big,
			email = excluded.email,
			enterprise_email = excluded.enterprise_email,
			mobile = excluded.mobile,
			tenant_key = excluded.tenant_key,
			employee_no = excluded.employee_no,
			updated_at = now(),
			last_login_at = now()
		returning created_at, updated_at, last_login_at
	`,
		user.UnionID,
		user.OpenID,
		user.UserID,
		user.Name,
		user.EnName,
		user.AvatarURL,
		user.AvatarThumb,
		user.AvatarMiddle,
		user.AvatarBig,
		user.Email,
		user.EnterpriseEmail,
		user.Mobile,
		user.TenantKey,
		user.EmployeeNo,
	).Scan(&user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt); err != nil {
		return domain.FeishuUser{}, fmt.Errorf("upsert feishu user: %w", err)
	}

	return user, nil
}

func (r *FeishuUserRepository) UpsertUser(ctx context.Context, user domain.UserInput) error {
	return upsertUser(ctx, r.pool, user)
}

func (r *FeishuUserRepository) BootstrapFirstSuperAdmin(ctx context.Context, userID string) error {
	return NewAuthorizationRepository(r.pool).BootstrapFirstSuperAdmin(ctx, userID)
}

func (r *FeishuUserRepository) ListGlobalRoles(ctx context.Context, userID string) ([]domain.GlobalRole, error) {
	return NewAuthorizationRepository(r.pool).ListGlobalRoles(ctx, userID)
}

func (r *FeishuUserRepository) CreateFeishuAuthSession(ctx context.Context, session domain.FeishuAuthSession) error {
	if _, err := r.pool.Exec(ctx, `
		insert into feishu_auth_sessions (
			session_token_hash,
			union_id,
			access_token,
			access_token_expires_at,
			refresh_token,
			refresh_token_expires_at,
			token_type,
			scope
		) values (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
	`,
		session.SessionTokenHash,
		session.UnionID,
		session.AccessToken,
		session.AccessTokenExpiresAt,
		session.RefreshToken,
		nullableTime(session.RefreshTokenExpiresAt),
		session.TokenType,
		session.Scope,
	); err != nil {
		return fmt.Errorf("create feishu auth session: %w", err)
	}

	return nil
}

func (r *FeishuUserRepository) FindFeishuAuthSession(ctx context.Context, sessionTokenHash string) (domain.FeishuAuthSession, error) {
	var session domain.FeishuAuthSession
	var refreshTokenExpiresAt *time.Time

	err := r.pool.QueryRow(ctx, `
		select
			s.session_token_hash,
			s.union_id,
			s.access_token,
			s.access_token_expires_at,
			s.refresh_token,
			s.refresh_token_expires_at,
			s.token_type,
			s.scope,
			s.created_at,
			s.updated_at,
			s.last_seen_at,
			u.union_id,
			u.open_id,
			u.user_id,
			u.name,
			u.en_name,
			u.avatar_url,
			u.avatar_thumb,
			u.avatar_middle,
			u.avatar_big,
			u.email,
			u.enterprise_email,
			u.mobile,
			u.tenant_key,
			u.employee_no,
			u.created_at,
			u.updated_at,
			u.last_login_at
		from feishu_auth_sessions s
		join feishu_users u on u.union_id = s.union_id
		where s.session_token_hash = $1
	`, sessionTokenHash).Scan(
		&session.SessionTokenHash,
		&session.UnionID,
		&session.AccessToken,
		&session.AccessTokenExpiresAt,
		&session.RefreshToken,
		&refreshTokenExpiresAt,
		&session.TokenType,
		&session.Scope,
		&session.CreatedAt,
		&session.UpdatedAt,
		&session.LastSeenAt,
		&session.User.UnionID,
		&session.User.OpenID,
		&session.User.UserID,
		&session.User.Name,
		&session.User.EnName,
		&session.User.AvatarURL,
		&session.User.AvatarThumb,
		&session.User.AvatarMiddle,
		&session.User.AvatarBig,
		&session.User.Email,
		&session.User.EnterpriseEmail,
		&session.User.Mobile,
		&session.User.TenantKey,
		&session.User.EmployeeNo,
		&session.User.CreatedAt,
		&session.User.UpdatedAt,
		&session.User.LastLoginAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.FeishuAuthSession{}, domain.ErrFeishuSessionNotFound
		}
		return domain.FeishuAuthSession{}, fmt.Errorf("find feishu auth session: %w", err)
	}

	if refreshTokenExpiresAt != nil {
		session.RefreshTokenExpiresAt = *refreshTokenExpiresAt
	}

	return session, nil
}

func (r *FeishuUserRepository) UpdateFeishuAuthSessionToken(ctx context.Context, session domain.FeishuAuthSession) error {
	if _, err := r.pool.Exec(ctx, `
		update feishu_auth_sessions
		set
			access_token = $2,
			access_token_expires_at = $3,
			refresh_token = $4,
			refresh_token_expires_at = $5,
			token_type = $6,
			scope = $7,
			updated_at = now(),
			last_seen_at = now()
		where session_token_hash = $1
	`,
		session.SessionTokenHash,
		session.AccessToken,
		session.AccessTokenExpiresAt,
		session.RefreshToken,
		nullableTime(session.RefreshTokenExpiresAt),
		session.TokenType,
		session.Scope,
	); err != nil {
		return fmt.Errorf("update feishu auth session token: %w", err)
	}

	return nil
}

func (r *FeishuUserRepository) DeleteFeishuAuthSession(ctx context.Context, sessionTokenHash string) error {
	if _, err := r.pool.Exec(ctx, `
		delete from feishu_auth_sessions
		where session_token_hash = $1
	`, sessionTokenHash); err != nil {
		return fmt.Errorf("delete feishu auth session: %w", err)
	}

	return nil
}

func nullableTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
