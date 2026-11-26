package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"forge-server/internal/domain"
)

func TestFeishuAuthServiceCompleteAuthorization(t *testing.T) {
	client := fakeFeishuClient{
		token: domain.FeishuToken{
			AccessToken:           "user-access-token",
			ExpiresIn:             7200,
			RefreshToken:          "refresh-token",
			RefreshTokenExpiresIn: 30 * 24 * 3600,
			TokenType:             "Bearer",
			Scope:                 "offline_access",
		},
		user: domain.FeishuUser{
			UnionID:   "on_union",
			OpenID:    "ou_open",
			UserID:    "u_feishu",
			Name:      "张三",
			TenantKey: "tenant",
		},
	}
	repository := &fakeAuthUserRepository{}
	service := NewFeishuAuthService(client, repository, testFeishuTenantResolver())

	if _, err := service.CompleteAuthorization(context.Background(), "xiaomi", "https://evil.example.com/steal", "auth-code"); err != domain.ErrInvalidFeishuRedirectURI {
		t.Fatalf("expected invalid redirect rejected, got %v", err)
	}
	result, err := service.CompleteAuthorization(context.Background(), "xiaomi", "https://forge.example.com/login/feishu/callback", "auth-code")
	if err != nil {
		t.Fatalf("complete authorization: %v", err)
	}

	resolved := repository.resolvedExternal
	if repository.resolvedTenantID != "xiaomi" || resolved.Provider != domain.IdentityProviderFeishu ||
		resolved.Issuer != "cli_xiaomi0001" || resolved.Subject != "ou_open" || !repository.resolvedLogin {
		t.Fatalf("expected feishu identity of tenant app, got tenant=%q %+v", repository.resolvedTenantID, resolved)
	}
	if resolved.Profile["unionId"] != "on_union" || resolved.Profile["userId"] != "u_feishu" {
		t.Fatalf("expected feishu ids in identity profile, got %+v", resolved.Profile)
	}
	if result.User.ID != "user-1" || repository.bootstrappedUser.ID != "user-1" {
		t.Fatalf("expected generated user id and role bootstrap, got result=%+v bootstrap=%+v", result.User, repository.bootstrappedUser)
	}
	if len(result.Roles) != 1 || result.Roles[0] != domain.RoleSuperAdmin || !result.Capabilities.CanManageTenants {
		t.Fatalf("expected super admin role, got %#v %#v", result.Roles, result.Capabilities)
	}
	if result.SessionToken == "" || repository.createdSession.TokenHash == "" {
		t.Fatal("expected session token and stored session")
	}
	if repository.createdSession.ActiveTenantID != "xiaomi" || result.ActiveTenant.Name != "小米" || result.HomeTenant.ID != "xiaomi" {
		t.Fatalf("expected session in home tenant, got session=%+v result=%+v/%+v", repository.createdSession, result.HomeTenant, result.ActiveTenant)
	}
	if repository.createdSession.ProviderAccessToken != "user-access-token" || result.ProviderAccessToken != "user-access-token" {
		t.Fatalf("expected provider token kept on session, got %+v", repository.createdSession)
	}
}

func TestFeishuAuthServiceGetSessionExpiresWhenTenantDisabled(t *testing.T) {
	now := time.Now()
	for name, session := range map[string]domain.AuthSession{
		"home disabled":   activeTestSession(now, func(s *domain.AuthSession) { s.HomeTenantStatus = domain.TenantStatusDisabled }),
		"active disabled": activeTestSession(now, func(s *domain.AuthSession) { s.ActiveTenantStatus = domain.TenantStatusDisabled }),
	} {
		t.Run(name, func(t *testing.T) {
			repository := &fakeAuthUserRepository{session: session}
			service := NewFeishuAuthService(fakeFeishuClient{}, repository, testFeishuTenantResolver())

			if _, err := service.GetSession(context.Background(), "session-token"); err != domain.ErrSessionExpired {
				t.Fatalf("expected session expired for disabled tenant, got %v", err)
			}
			if !repository.deletedSession {
				t.Fatal("expected disabled tenant session to be deleted")
			}
		})
	}
}

func TestFeishuAuthServiceGetSessionRefreshesWithHomeTenantApp(t *testing.T) {
	now := time.Now()
	client := fakeFeishuClient{
		refreshApp: &domain.FeishuApp{},
		refreshToken: domain.FeishuToken{
			AccessToken:           "new-access-token",
			ExpiresIn:             7200,
			RefreshToken:          "new-refresh-token",
			RefreshTokenExpiresIn: 30 * 24 * 3600,
			TokenType:             "Bearer",
		},
	}
	session := activeTestSession(now, func(s *domain.AuthSession) {
		s.ProviderAccessTokenExpiresAt = now.Add(time.Minute)
		s.ActiveTenantID = "other"
		s.ActiveTenant = domain.TenantSummary{ID: "other", Name: "其他"}
	})
	repository := &fakeAuthUserRepository{session: session, roles: []domain.Role{domain.RoleSuperAdmin}}
	service := NewFeishuAuthService(client, repository, testFeishuTenantResolver())

	result, err := service.GetSession(context.Background(), "session-token")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if repository.updatedSession.ProviderAccessToken != "new-access-token" || result.ProviderAccessToken != "new-access-token" {
		t.Fatalf("expected refreshed access token, got %+v", repository.updatedSession)
	}
	if repository.updatedSession.ProviderRefreshToken != "new-refresh-token" {
		t.Fatalf("expected refreshed refresh token, got %q", repository.updatedSession.ProviderRefreshToken)
	}
	if client.refreshApp.AppID != "cli_xiaomi0001" {
		t.Fatalf("expected refresh with home tenant app, got %+v", client.refreshApp)
	}
	if result.ActiveTenant.ID != "other" {
		t.Fatalf("expected super admin to stay in switched tenant, got %+v", result.ActiveTenant)
	}
}

func TestFeishuAuthServiceMovesNonSuperAdminBackToHomeTenant(t *testing.T) {
	session := activeTestSession(time.Now(), func(s *domain.AuthSession) {
		s.ActiveTenantID = "other"
		s.ActiveTenant = domain.TenantSummary{ID: "other", Name: "其他"}
	})
	repository := &fakeAuthUserRepository{session: session, roles: []domain.Role{domain.RoleTenantAdmin}}
	service := NewFeishuAuthService(fakeFeishuClient{}, repository, testFeishuTenantResolver())

	result, err := service.GetSession(context.Background(), "session-token")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if result.ActiveTenant.ID != "xiaomi" || repository.switchedTenantID != "xiaomi" {
		t.Fatalf("expected session moved back to home tenant, got %+v switched=%q", result.ActiveTenant, repository.switchedTenantID)
	}
}

func TestFeishuAuthServiceSwitchTenant(t *testing.T) {
	session := activeTestSession(time.Now(), nil)

	repository := &fakeAuthUserRepository{session: session, roles: []domain.Role{domain.RoleTenantAdmin}}
	service := NewFeishuAuthService(fakeFeishuClient{}, repository, testFeishuTenantResolver())
	if _, err := service.SwitchTenant(context.Background(), "session-token", "other"); !errors.Is(err, domain.ErrTenantSwitchForbidden) {
		t.Fatalf("expected tenant admin switch forbidden, got %v", err)
	}
	if _, err := service.SwitchTenant(context.Background(), "session-token", "xiaomi"); err != nil {
		t.Fatalf("expected switching to home tenant allowed, got %v", err)
	}

	repository = &fakeAuthUserRepository{session: session, roles: []domain.Role{domain.RoleSuperAdmin}}
	service = NewFeishuAuthService(fakeFeishuClient{}, repository, testFeishuTenantResolver())
	if _, err := service.SwitchTenant(context.Background(), "session-token", "other"); err != nil {
		t.Fatalf("expected super admin switch allowed, got %v", err)
	}
	if repository.switchedTenantID != "other" {
		t.Fatalf("expected session switched to other, got %q", repository.switchedTenantID)
	}
}

func activeTestSession(now time.Time, mutate func(*domain.AuthSession)) domain.AuthSession {
	session := domain.AuthSession{
		TokenHash:                     "hash",
		UserID:                        "user-1",
		IdentityID:                    "identity-1",
		ActiveTenantID:                "xiaomi",
		ProviderAccessToken:           "access-token",
		ProviderAccessTokenExpiresAt:  now.Add(time.Hour),
		ProviderRefreshToken:          "refresh-token",
		ProviderRefreshTokenExpiresAt: now.Add(24 * time.Hour),
		User:                          domain.User{ID: "user-1", TenantID: "xiaomi", Name: "张三"},
		Identity:                      domain.UserIdentity{ID: "identity-1", UserID: "user-1", Provider: domain.IdentityProviderFeishu, Issuer: "cli_xiaomi0001", Subject: "ou_open"},
		HomeTenant:                    domain.TenantSummary{ID: "xiaomi", Name: "小米"},
		HomeTenantStatus:              domain.TenantStatusActive,
		ActiveTenant:                  domain.TenantSummary{ID: "xiaomi", Name: "小米"},
		ActiveTenantStatus:            domain.TenantStatusActive,
	}
	if mutate != nil {
		mutate(&session)
	}
	return session
}

type fakeFeishuClient struct {
	token        domain.FeishuToken
	refreshToken domain.FeishuToken
	user         domain.FeishuUser
	refreshApp   *domain.FeishuApp
}

func (c fakeFeishuClient) AuthorizationURL(app domain.FeishuApp, redirectURI string, state string) (string, error) {
	return "https://accounts.feishu.cn/oauth?client_id=" + app.AppID + "&redirect_uri=" + redirectURI + "&state=" + state, nil
}

func (c fakeFeishuClient) ExchangeCode(_ context.Context, app domain.FeishuApp, _ string, _ string) (domain.FeishuToken, error) {
	if app.AppSecret == "" {
		return domain.FeishuToken{}, domain.ErrFeishuAuthNotConfigured
	}
	return c.token, nil
}

func (c fakeFeishuClient) GetUserInfo(context.Context, string) (domain.FeishuUser, error) {
	return c.user, nil
}

func (c fakeFeishuClient) RefreshAccessToken(_ context.Context, app domain.FeishuApp, _ string) (domain.FeishuToken, error) {
	if c.refreshApp != nil {
		*c.refreshApp = app
	}
	return c.refreshToken, nil
}

type fakeFeishuTenantResolver struct {
	tenants map[string]domain.Tenant
}

func testFeishuTenantResolver() fakeFeishuTenantResolver {
	return fakeFeishuTenantResolver{tenants: map[string]domain.Tenant{
		"xiaomi": {ID: "xiaomi", Name: "小米", FeishuAppID: "cli_xiaomi0001", FeishuAppSecret: "secret", Status: domain.TenantStatusActive},
	}}
}

func (r fakeFeishuTenantResolver) ResolveApp(_ context.Context, id string) (domain.Tenant, domain.FeishuApp, error) {
	tenant, ok := r.tenants[id]
	if !ok {
		return domain.Tenant{}, domain.FeishuApp{}, domain.ErrTenantNotFound
	}
	return tenant, domain.FeishuApp{AppID: tenant.FeishuAppID, AppSecret: tenant.FeishuAppSecret}, nil
}

func (r fakeFeishuTenantResolver) ResolveLoginTenant(ctx context.Context, id string) (domain.Tenant, domain.FeishuApp, error) {
	return r.ResolveApp(ctx, id)
}

type fakeAuthUserRepository struct {
	resolvedTenantID string
	resolvedExternal domain.ExternalUser
	resolvedLogin    bool
	bootstrappedUser domain.User
	roles            []domain.Role
	createdSession   domain.AuthSession
	session          domain.AuthSession
	updatedSession   domain.AuthSession
	switchedTenantID string
	deletedSession   bool
}

func (r *fakeAuthUserRepository) ResolveExternalUser(_ context.Context, tenantID string, external domain.ExternalUser, login bool) (domain.User, domain.UserIdentity, error) {
	r.resolvedTenantID, r.resolvedExternal, r.resolvedLogin = tenantID, external, login
	user := domain.User{ID: "user-1", TenantID: tenantID, Name: external.Name}
	return user, domain.UserIdentity{ID: "identity-1", UserID: user.ID, Provider: external.Provider, Issuer: external.Issuer, Subject: external.Subject}, nil
}

func (r *fakeAuthUserRepository) SearchUsers(context.Context, string, int) ([]domain.User, error) {
	return nil, nil
}

func (r *fakeAuthUserRepository) BootstrapUserRoles(_ context.Context, user domain.User) error {
	r.bootstrappedUser = user
	if len(r.roles) == 0 {
		r.roles = []domain.Role{domain.RoleSuperAdmin}
	}
	return nil
}

func (r *fakeAuthUserRepository) ListUserRoles(context.Context, string) ([]domain.Role, error) {
	return r.roles, nil
}

func (r *fakeAuthUserRepository) CreateSession(_ context.Context, session domain.AuthSession) error {
	r.createdSession = session
	return nil
}

func (r *fakeAuthUserRepository) FindSession(context.Context, string) (domain.AuthSession, error) {
	return r.session, nil
}

func (r *fakeAuthUserRepository) UpdateSessionToken(_ context.Context, session domain.AuthSession) error {
	r.updatedSession = session
	return nil
}

func (r *fakeAuthUserRepository) SetSessionActiveTenant(_ context.Context, _ string, tenantID string) error {
	r.switchedTenantID = tenantID
	r.session.ActiveTenantID = tenantID
	r.session.ActiveTenant = domain.TenantSummary{ID: tenantID, Name: tenantID}
	return nil
}

func (r *fakeAuthUserRepository) DeleteSession(context.Context, string) error {
	r.deletedSession = true
	return nil
}
