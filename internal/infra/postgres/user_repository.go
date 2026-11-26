package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserRepository 读写用户、登录身份、会话和用户角色。
type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// ResolveExternalUser 按登录身份查找用户，找不到就在 tenantID 下新建用户和身份。
// 找到时用最新资料更新用户的姓名、邮箱、头像和身份资料；login 为 true 时同时记录登录时间。
// 身份已属于其他租户的用户时返回 ErrUserTenantMismatch。
func (r *UserRepository) ResolveExternalUser(ctx context.Context, tenantID string, external domain.ExternalUser, login bool) (domain.User, domain.UserIdentity, error) {
	profile, err := json.Marshal(nonNilProfile(external.Profile))
	if err != nil {
		return domain.User{}, domain.UserIdentity{}, fmt.Errorf("encode identity profile: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, domain.UserIdentity{}, fmt.Errorf("begin resolve user: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	identity := domain.UserIdentity{
		Provider: external.Provider,
		Issuer:   external.Issuer,
		Subject:  external.Subject,
		Profile:  nonNilProfile(external.Profile),
	}
	var userTenantID string
	err = tx.QueryRow(ctx, `
		select i.id, i.user_id, u.tenant_id
		from user_identities i
		join users u on u.id = i.user_id
		where i.provider = $1 and i.issuer = $2 and i.subject = $3
		for update of i
	`, external.Provider, external.Issuer, external.Subject).Scan(&identity.ID, &identity.UserID, &userTenantID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		identity.ID = newPrefixedID("identity")
		identity.UserID = newPrefixedID("user")
		if _, err := tx.Exec(ctx, `
			insert into users (id, tenant_id, name, email, avatar_url) values ($1, $2, $3, $4, $5)
		`, identity.UserID, tenantID, external.Name, external.Email, external.AvatarURL); err != nil {
			return domain.User{}, domain.UserIdentity{}, fmt.Errorf("insert user: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			insert into user_identities (id, user_id, provider, issuer, subject, profile, last_login_at)
			values ($1, $2, $3, $4, $5, $6, case when $7 then now() end)
		`, identity.ID, identity.UserID, external.Provider, external.Issuer, external.Subject, profile, login); err != nil {
			return domain.User{}, domain.UserIdentity{}, fmt.Errorf("insert user identity: %w", err)
		}
	case err != nil:
		return domain.User{}, domain.UserIdentity{}, fmt.Errorf("find user identity: %w", err)
	default:
		if userTenantID != tenantID {
			return domain.User{}, domain.UserIdentity{}, domain.ErrUserTenantMismatch
		}
		if _, err := tx.Exec(ctx, `
			update users set name = $2, email = $3, avatar_url = $4, updated_at = now() where id = $1
		`, identity.UserID, external.Name, external.Email, external.AvatarURL); err != nil {
			return domain.User{}, domain.UserIdentity{}, fmt.Errorf("update user: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			update user_identities
			set profile = $2, updated_at = now(), last_login_at = case when $3 then now() else last_login_at end
			where id = $1
		`, identity.ID, profile, login); err != nil {
			return domain.User{}, domain.UserIdentity{}, fmt.Errorf("update user identity: %w", err)
		}
	}

	user, err := scanUser(tx.QueryRow(ctx, userSelect+` where id = $1`, identity.UserID))
	if err != nil {
		return domain.User{}, domain.UserIdentity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, domain.UserIdentity{}, fmt.Errorf("commit resolve user: %w", err)
	}
	return user, identity, nil
}

const userSelect = `select id, tenant_id, name, email, avatar_url, created_at, updated_at from users`

// SearchUsers 在当前租户里按姓名或邮箱搜索用户，最多返回 limit 个。
func (r *UserRepository) SearchUsers(ctx context.Context, query string, limit int) ([]domain.User, error) {
	tenantID, err := domain.TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
	rows, err := r.pool.Query(ctx, userSelect+`
		where tenant_id = $1 and ($2 = '%%' or name ilike $2 or email ilike $2)
		order by name asc, id asc
		limit $3
	`, tenantID, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}
	defer rows.Close()

	users := []domain.User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

func (r *UserRepository) CreateSession(ctx context.Context, session domain.AuthSession) error {
	if _, err := r.pool.Exec(ctx, `
		insert into auth_sessions (
			session_token_hash,
			user_id,
			identity_id,
			active_tenant_id,
			provider_access_token,
			provider_access_token_expires_at,
			provider_refresh_token,
			provider_refresh_token_expires_at,
			provider_token_type,
			provider_scope
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		session.TokenHash,
		session.UserID,
		session.IdentityID,
		session.ActiveTenantID,
		session.ProviderAccessToken,
		session.ProviderAccessTokenExpiresAt,
		session.ProviderRefreshToken,
		nullableTime(session.ProviderRefreshTokenExpiresAt),
		session.ProviderTokenType,
		session.ProviderScope,
	); err != nil {
		return fmt.Errorf("create auth session: %w", err)
	}
	return nil
}

func (r *UserRepository) FindSession(ctx context.Context, tokenHash string) (domain.AuthSession, error) {
	var session domain.AuthSession
	var refreshTokenExpiresAt *time.Time
	var profile []byte
	err := r.pool.QueryRow(ctx, `
		update auth_sessions s
		set last_seen_at = now()
		from users u, user_identities i, tenants home, tenants active
		where s.session_token_hash = $1
			and u.id = s.user_id
			and i.id = s.identity_id
			and home.id = u.tenant_id
			and active.id = s.active_tenant_id
		returning
			s.session_token_hash,
			s.user_id,
			s.identity_id,
			s.active_tenant_id,
			s.provider_access_token,
			s.provider_access_token_expires_at,
			s.provider_refresh_token,
			s.provider_refresh_token_expires_at,
			s.provider_token_type,
			s.provider_scope,
			u.tenant_id,
			u.name,
			u.email,
			u.avatar_url,
			u.created_at,
			u.updated_at,
			i.provider,
			i.issuer,
			i.subject,
			i.profile,
			home.name,
			home.status,
			active.name,
			active.status
	`, tokenHash).Scan(
		&session.TokenHash,
		&session.UserID,
		&session.IdentityID,
		&session.ActiveTenantID,
		&session.ProviderAccessToken,
		&session.ProviderAccessTokenExpiresAt,
		&session.ProviderRefreshToken,
		&refreshTokenExpiresAt,
		&session.ProviderTokenType,
		&session.ProviderScope,
		&session.User.TenantID,
		&session.User.Name,
		&session.User.Email,
		&session.User.AvatarURL,
		&session.User.CreatedAt,
		&session.User.UpdatedAt,
		&session.Identity.Provider,
		&session.Identity.Issuer,
		&session.Identity.Subject,
		&profile,
		&session.HomeTenant.Name,
		&session.HomeTenantStatus,
		&session.ActiveTenant.Name,
		&session.ActiveTenantStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AuthSession{}, domain.ErrSessionNotFound
	}
	if err != nil {
		return domain.AuthSession{}, fmt.Errorf("find auth session: %w", err)
	}
	if refreshTokenExpiresAt != nil {
		session.ProviderRefreshTokenExpiresAt = *refreshTokenExpiresAt
	}
	if err := json.Unmarshal(profile, &session.Identity.Profile); err != nil {
		return domain.AuthSession{}, fmt.Errorf("decode identity profile: %w", err)
	}
	session.User.ID = session.UserID
	session.Identity.ID = session.IdentityID
	session.Identity.UserID = session.UserID
	session.HomeTenant.ID = session.User.TenantID
	session.ActiveTenant.ID = session.ActiveTenantID
	return session, nil
}

func (r *UserRepository) UpdateSessionToken(ctx context.Context, session domain.AuthSession) error {
	if _, err := r.pool.Exec(ctx, `
		update auth_sessions
		set
			provider_access_token = $2,
			provider_access_token_expires_at = $3,
			provider_refresh_token = $4,
			provider_refresh_token_expires_at = $5,
			provider_token_type = $6,
			provider_scope = $7,
			updated_at = now()
		where session_token_hash = $1
	`,
		session.TokenHash,
		session.ProviderAccessToken,
		session.ProviderAccessTokenExpiresAt,
		session.ProviderRefreshToken,
		nullableTime(session.ProviderRefreshTokenExpiresAt),
		session.ProviderTokenType,
		session.ProviderScope,
	); err != nil {
		return fmt.Errorf("update auth session token: %w", err)
	}
	return nil
}

// SetSessionActiveTenant 切换会话当前所在的租户，租户必须存在且启用中。
func (r *UserRepository) SetSessionActiveTenant(ctx context.Context, tokenHash string, tenantID string) error {
	tag, err := r.pool.Exec(ctx, `
		update auth_sessions s
		set active_tenant_id = t.id, updated_at = now()
		from tenants t
		where s.session_token_hash = $1 and t.id = $2 and t.status = 'active'
	`, tokenHash, tenantID)
	if err != nil {
		return fmt.Errorf("switch session tenant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrTenantNotFound
	}
	return nil
}

func (r *UserRepository) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := r.pool.Exec(ctx, `delete from auth_sessions where session_token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("delete auth session: %w", err)
	}
	return nil
}

// BootstrapUserRoles 在登录时补齐初始角色：平台还没有 SuperAdmin 时授予 SuperAdmin；
// 用户所属租户还没有 TenantAdmin 时授予 TenantAdmin。用事务级 advisory lock 串行化，并发登录不会重复授予。
func (r *UserRepository) BootstrapUserRoles(ctx context.Context, user domain.User) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin bootstrap roles: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('forge_bootstrap_user_roles'))`); err != nil {
		return fmt.Errorf("lock bootstrap roles: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into user_roles (user_id, role, created_by_user_id)
		select $1, 'SuperAdmin', $1
		where not exists (select 1 from user_roles where role = 'SuperAdmin')
	`, user.ID); err != nil {
		return fmt.Errorf("bootstrap super admin: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into user_roles (user_id, role, created_by_user_id)
		select $1, 'TenantAdmin', $1
		where not exists (
			select 1 from user_roles r join users u on u.id = r.user_id
			where r.role = 'TenantAdmin' and u.tenant_id = $2
		)
	`, user.ID, user.TenantID); err != nil {
		return fmt.Errorf("bootstrap tenant admin: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit bootstrap roles: %w", err)
	}
	return nil
}

func (r *UserRepository) ListUserRoles(ctx context.Context, userID string) ([]domain.Role, error) {
	return listUserRoles(ctx, r.pool, userID)
}

func listUserRoles(ctx context.Context, pool *pgxpool.Pool, userID string) ([]domain.Role, error) {
	rows, err := pool.Query(ctx, `select role from user_roles where user_id = $1 order by role asc`, userID)
	if err != nil {
		return nil, fmt.Errorf("query user roles: %w", err)
	}
	defer rows.Close()

	roles := []domain.Role{}
	for rows.Next() {
		var role domain.Role
		if err := rows.Scan(&role); err != nil {
			return nil, fmt.Errorf("scan user role: %w", err)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user roles: %w", err)
	}
	return roles, nil
}

func scanUser(row pgx.Row) (domain.User, error) {
	var user domain.User
	if err := row.Scan(&user.ID, &user.TenantID, &user.Name, &user.Email, &user.AvatarURL, &user.CreatedAt, &user.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, domain.ErrUserNotFound
		}
		return domain.User{}, fmt.Errorf("scan user: %w", err)
	}
	return user, nil
}

func nonNilProfile(profile map[string]string) map[string]string {
	if profile == nil {
		return map[string]string{}
	}
	return profile
}

func nullableTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

// newPrefixedID 生成 "<prefix>-<16 位十六进制>" 格式的 ID。
func newPrefixedID(prefix string) string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return prefix + "-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return prefix + "-" + hex.EncodeToString(value[:])
}
