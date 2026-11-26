package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"slices"
	"strings"
	"time"

	"forge-server/internal/domain"
)

const feishuSessionRefreshWindow = 5 * time.Minute

type FeishuOAuthClient interface {
	AuthorizationURL(app domain.FeishuApp, redirectURI string, state string) (string, error)
	ExchangeCode(ctx context.Context, app domain.FeishuApp, redirectURI string, code string) (domain.FeishuToken, error)
	GetUserInfo(ctx context.Context, accessToken string) (domain.FeishuUser, error)
	RefreshAccessToken(ctx context.Context, app domain.FeishuApp, refreshToken string) (domain.FeishuToken, error)
}

// FeishuTenantResolver 解析登录租户与其飞书应用凭证，实现见 TenantService。
type FeishuTenantResolver interface {
	ResolveApp(ctx context.Context, id string) (domain.Tenant, domain.FeishuApp, error)
	ResolveLoginTenant(ctx context.Context, id string) (domain.Tenant, domain.FeishuApp, error)
}

type AuthUserRepository interface {
	BootstrapUserRoles(ctx context.Context, user domain.User) error
	CreateSession(ctx context.Context, session domain.AuthSession) error
	DeleteSession(ctx context.Context, tokenHash string) error
	FindSession(ctx context.Context, tokenHash string) (domain.AuthSession, error)
	ListUserRoles(ctx context.Context, userID string) ([]domain.Role, error)
	ResolveExternalUser(ctx context.Context, tenantID string, external domain.ExternalUser, login bool) (domain.User, domain.UserIdentity, error)
	SetSessionActiveTenant(ctx context.Context, tokenHash string, tenantID string) error
	UpdateSessionToken(ctx context.Context, session domain.AuthSession) error
}

// FeishuAuthService 负责飞书登录和控制台会话：登录时按飞书身份找到或新建用户，会话记录当前所在租户。
type FeishuAuthService struct {
	client     FeishuOAuthClient
	repository AuthUserRepository
	tenants    FeishuTenantResolver
}

func NewFeishuAuthService(client FeishuOAuthClient, repository AuthUserRepository, tenants FeishuTenantResolver) *FeishuAuthService {
	return &FeishuAuthService{
		client:     client,
		repository: repository,
		tenants:    tenants,
	}
}

// AuthorizationURL 返回指定租户的授权地址，以及实际使用的租户 ID（tenantID 为空且只有一个启用租户时自动选中）。
// redirectURI 是前端按自身域名拼出的回调地址，必须是 {origin}/login/feishu/callback。
func (s *FeishuAuthService) AuthorizationURL(ctx context.Context, tenantID string, redirectURI string, state string) (string, string, error) {
	if !domain.IsValidFeishuRedirectURI(redirectURI) {
		return "", "", domain.ErrInvalidFeishuRedirectURI
	}
	tenant, app, err := s.tenants.ResolveLoginTenant(ctx, tenantID)
	if err != nil {
		return "", "", err
	}
	authURL, err := s.client.AuthorizationURL(app, redirectURI, state)
	if err != nil {
		return "", "", err
	}
	return authURL, tenant.ID, nil
}

// CompleteAuthorization 用发起授权时的租户应用和回调地址换取 token，找到或新建该租户的用户，并创建会话。
func (s *FeishuAuthService) CompleteAuthorization(ctx context.Context, tenantID string, redirectURI string, code string) (domain.ConsoleSession, error) {
	if !domain.IsValidFeishuRedirectURI(redirectURI) {
		return domain.ConsoleSession{}, domain.ErrInvalidFeishuRedirectURI
	}
	tenant, app, err := s.tenants.ResolveApp(ctx, tenantID)
	if err != nil {
		return domain.ConsoleSession{}, err
	}
	token, err := s.client.ExchangeCode(ctx, app, redirectURI, code)
	if err != nil {
		return domain.ConsoleSession{}, err
	}
	feishuUser, err := s.client.GetUserInfo(ctx, token.AccessToken)
	if err != nil {
		return domain.ConsoleSession{}, err
	}
	if strings.TrimSpace(feishuUser.OpenID) == "" {
		return domain.ConsoleSession{}, domain.ErrFeishuAuthorization
	}

	user, identity, err := s.repository.ResolveExternalUser(ctx, tenant.ID, feishuUser.ExternalUser(app.AppID), true)
	if err != nil {
		return domain.ConsoleSession{}, err
	}
	if err := s.repository.BootstrapUserRoles(ctx, user); err != nil {
		return domain.ConsoleSession{}, err
	}

	sessionToken, err := randomSessionToken()
	if err != nil {
		return domain.ConsoleSession{}, err
	}
	now := time.Now()
	session := domain.AuthSession{
		TokenHash:      sessionTokenHash(sessionToken),
		UserID:         user.ID,
		IdentityID:     identity.ID,
		ActiveTenantID: tenant.ID,
	}
	applyProviderToken(&session, token, now)
	if err := s.repository.CreateSession(ctx, session); err != nil {
		return domain.ConsoleSession{}, err
	}

	session.User = user
	session.Identity = identity
	session.HomeTenant = domain.TenantSummary{ID: tenant.ID, Name: tenant.Name}
	session.ActiveTenant = session.HomeTenant
	return s.consoleSession(ctx, session, sessionToken, now)
}

// GetSession 校验会话并在 token 快过期时刷新。用户所属租户或当前租户停用后，会话失效。
func (s *FeishuAuthService) GetSession(ctx context.Context, sessionToken string) (domain.ConsoleSession, error) {
	session, err := s.repository.FindSession(ctx, sessionTokenHash(sessionToken))
	if err != nil {
		return domain.ConsoleSession{}, err
	}

	now := time.Now()
	tenantsActive := session.HomeTenantStatus == domain.TenantStatusActive && session.ActiveTenantStatus == domain.TenantStatusActive
	expired := !tenantsActive || !now.Before(session.ProviderAccessTokenExpiresAt)
	if session.ProviderRefreshToken != "" && tenantsActive {
		expired = refreshTokenExpired(session, now)
	}
	if expired {
		_ = s.repository.DeleteSession(ctx, session.TokenHash)
		return domain.ConsoleSession{}, domain.ErrSessionExpired
	}

	if !session.ProviderAccessTokenExpiresAt.After(now.Add(feishuSessionRefreshWindow)) {
		refreshed, err := s.refreshSession(ctx, session, now)
		if err != nil {
			if !now.Before(session.ProviderAccessTokenExpiresAt) {
				return domain.ConsoleSession{}, err
			}
		} else {
			session = refreshed
		}
	}
	return s.consoleSession(ctx, session, sessionToken, now)
}

// SwitchTenant 把会话切换到另一个租户，只有 SuperAdmin 可以切换到所属租户以外的租户。
func (s *FeishuAuthService) SwitchTenant(ctx context.Context, sessionToken string, tenantID string) (domain.ConsoleSession, error) {
	current, err := s.GetSession(ctx, sessionToken)
	if err != nil {
		return domain.ConsoleSession{}, err
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID != current.HomeTenant.ID && !slices.Contains(current.Roles, domain.RoleSuperAdmin) {
		return domain.ConsoleSession{}, domain.ErrTenantSwitchForbidden
	}
	if err := s.repository.SetSessionActiveTenant(ctx, sessionTokenHash(sessionToken), tenantID); err != nil {
		return domain.ConsoleSession{}, err
	}
	return s.GetSession(ctx, sessionToken)
}

func (s *FeishuAuthService) Logout(ctx context.Context, sessionToken string) error {
	if sessionToken == "" {
		return nil
	}
	return s.repository.DeleteSession(ctx, sessionTokenHash(sessionToken))
}

// consoleSession 补上角色和能力。普通用户的会话始终在所属租户；不是 SuperAdmin 却位于其他租户的会话
// （例如 SuperAdmin 角色被收回）切回所属租户。
func (s *FeishuAuthService) consoleSession(ctx context.Context, session domain.AuthSession, sessionToken string, now time.Time) (domain.ConsoleSession, error) {
	roles, err := s.repository.ListUserRoles(ctx, session.UserID)
	if err != nil {
		return domain.ConsoleSession{}, err
	}
	if session.ActiveTenant.ID != session.HomeTenant.ID && !slices.Contains(roles, domain.RoleSuperAdmin) {
		if err := s.repository.SetSessionActiveTenant(ctx, session.TokenHash, session.HomeTenant.ID); err != nil {
			return domain.ConsoleSession{}, err
		}
		session.ActiveTenant = session.HomeTenant
	}

	expiresAt := session.ProviderAccessTokenExpiresAt
	if session.ProviderRefreshToken != "" && !session.ProviderRefreshTokenExpiresAt.IsZero() {
		expiresAt = session.ProviderRefreshTokenExpiresAt
	}
	return domain.ConsoleSession{
		SessionToken:        sessionToken,
		ExpiresAt:           expiresAt,
		User:                session.User,
		Identity:            session.Identity,
		HomeTenant:          session.HomeTenant,
		ActiveTenant:        session.ActiveTenant,
		Roles:               roles,
		Capabilities:        domain.TenantCapabilitiesForRoles(roles),
		ProviderAccessToken: session.ProviderAccessToken,
		ProviderToken: domain.FeishuToken{
			ExpiresIn:             secondsUntil(session.ProviderAccessTokenExpiresAt, now),
			RefreshToken:          session.ProviderRefreshToken,
			RefreshTokenExpiresIn: secondsUntil(session.ProviderRefreshTokenExpiresAt, now),
			TokenType:             session.ProviderTokenType,
			Scope:                 session.ProviderScope,
		},
	}, nil
}

// refreshSession 用用户所属租户的飞书应用刷新 token。
func (s *FeishuAuthService) refreshSession(ctx context.Context, session domain.AuthSession, now time.Time) (domain.AuthSession, error) {
	if session.ProviderRefreshToken == "" || refreshTokenExpired(session, now) {
		_ = s.repository.DeleteSession(ctx, session.TokenHash)
		return domain.AuthSession{}, domain.ErrSessionExpired
	}
	_, app, err := s.tenants.ResolveApp(ctx, session.HomeTenant.ID)
	if err != nil {
		return domain.AuthSession{}, err
	}
	token, err := s.client.RefreshAccessToken(ctx, app, session.ProviderRefreshToken)
	if err != nil {
		return domain.AuthSession{}, err
	}
	refreshToken, refreshExpiresAt := session.ProviderRefreshToken, session.ProviderRefreshTokenExpiresAt
	applyProviderToken(&session, token, now)
	if token.RefreshToken == "" {
		session.ProviderRefreshToken = refreshToken
	}
	if token.RefreshTokenExpiresIn <= 0 {
		session.ProviderRefreshTokenExpiresAt = refreshExpiresAt
	}
	if err := s.repository.UpdateSessionToken(ctx, session); err != nil {
		return domain.AuthSession{}, err
	}
	return session, nil
}

func applyProviderToken(session *domain.AuthSession, token domain.FeishuToken, now time.Time) {
	session.ProviderAccessToken = token.AccessToken
	session.ProviderAccessTokenExpiresAt = tokenExpiresAt(token.ExpiresIn, now)
	session.ProviderRefreshToken = token.RefreshToken
	session.ProviderRefreshTokenExpiresAt = time.Time{}
	if token.RefreshTokenExpiresIn > 0 {
		session.ProviderRefreshTokenExpiresAt = tokenExpiresAt(token.RefreshTokenExpiresIn, now)
	}
	session.ProviderTokenType = token.TokenType
	session.ProviderScope = token.Scope
}

func refreshTokenExpired(session domain.AuthSession, now time.Time) bool {
	return session.ProviderRefreshTokenExpiresAt.IsZero() || !now.Before(session.ProviderRefreshTokenExpiresAt)
}

func tokenExpiresAt(expiresIn int, now time.Time) time.Time {
	if expiresIn <= 0 {
		return now
	}
	return now.Add(time.Duration(expiresIn) * time.Second)
}

func secondsUntil(value time.Time, now time.Time) int {
	if value.IsZero() {
		return 0
	}
	seconds := int(value.Sub(now).Seconds())
	if seconds < 0 {
		return 0
	}
	return seconds
}

func randomSessionToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

func sessionTokenHash(sessionToken string) string {
	sum := sha256.Sum256([]byte(sessionToken))
	return hex.EncodeToString(sum[:])
}
