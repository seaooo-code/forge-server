package service

import (
	"context"
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
			Name:      "张三",
			TenantKey: "tenant",
		},
	}
	repository := &fakeFeishuUserRepository{}
	service := NewFeishuAuthService(client, repository)

	result, err := service.CompleteAuthorization(context.Background(), "auth-code")
	if err != nil {
		t.Fatalf("complete authorization: %v", err)
	}

	if result.User.UnionID != "on_union" {
		t.Fatalf("expected saved user, got %q", result.User.UnionID)
	}
	if repository.savedUser.UnionID != "on_union" {
		t.Fatalf("expected repository to save user, got %q", repository.savedUser.UnionID)
	}
	if repository.savedConsoleUser.ID != "ou_open" || repository.savedConsoleUser.FeishuUnionID != "on_union" {
		t.Fatalf("expected repository to sync console user, got %+v", repository.savedConsoleUser)
	}
	if repository.savedConsoleUser.DepartmentIDs == nil || len(repository.savedConsoleUser.DepartmentIDs) != 0 {
		t.Fatalf("expected empty department ids, got %#v", repository.savedConsoleUser.DepartmentIDs)
	}
	if result.SessionToken == "" {
		t.Fatal("expected session token")
	}
	if repository.createdSession.SessionTokenHash == "" {
		t.Fatal("expected repository to save auth session")
	}
}

func TestFeishuAuthServiceGetSessionRefreshesExpiringAccessToken(t *testing.T) {
	now := time.Now()
	client := fakeFeishuClient{
		refreshToken: domain.FeishuToken{
			AccessToken:           "new-access-token",
			ExpiresIn:             7200,
			RefreshToken:          "new-refresh-token",
			RefreshTokenExpiresIn: 30 * 24 * 3600,
			TokenType:             "Bearer",
			Scope:                 "offline_access",
		},
	}
	repository := &fakeFeishuUserRepository{
		session: domain.FeishuAuthSession{
			SessionTokenHash:      "hash",
			UnionID:               "on_union",
			AccessToken:           "old-access-token",
			AccessTokenExpiresAt:  now.Add(time.Minute),
			RefreshToken:          "old-refresh-token",
			RefreshTokenExpiresAt: now.Add(24 * time.Hour),
			TokenType:             "Bearer",
			Scope:                 "offline_access",
			User: domain.FeishuUser{
				UnionID: "on_union",
				OpenID:  "ou_open",
				Name:    "张三",
			},
		},
	}
	service := NewFeishuAuthService(client, repository)

	result, err := service.GetSession(context.Background(), "session-token")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}

	if result.User.UnionID != "on_union" {
		t.Fatalf("expected session user, got %q", result.User.UnionID)
	}
	if repository.updatedSession.AccessToken != "new-access-token" {
		t.Fatalf("expected refreshed access token, got %q", repository.updatedSession.AccessToken)
	}
	if result.Token.AccessToken != "new-access-token" {
		t.Fatalf("expected result access token, got %q", result.Token.AccessToken)
	}
	if repository.updatedSession.RefreshToken != "new-refresh-token" {
		t.Fatalf("expected refreshed refresh token, got %q", repository.updatedSession.RefreshToken)
	}
	if repository.savedConsoleUser.ID != "ou_open" {
		t.Fatalf("expected synced console user id, got %+v", repository.savedConsoleUser)
	}
}

type fakeFeishuClient struct {
	token        domain.FeishuToken
	refreshToken domain.FeishuToken
	user         domain.FeishuUser
}

func (c fakeFeishuClient) AuthorizationURL(state string) (string, error) {
	return "https://accounts.feishu.cn/oauth?state=" + state, nil
}

func (c fakeFeishuClient) ExchangeCode(context.Context, string) (domain.FeishuToken, error) {
	return c.token, nil
}

func (c fakeFeishuClient) GetUserInfo(context.Context, string) (domain.FeishuUser, error) {
	return c.user, nil
}

func (c fakeFeishuClient) RefreshAccessToken(context.Context, string) (domain.FeishuToken, error) {
	return c.refreshToken, nil
}

type fakeFeishuUserRepository struct {
	createdSession   domain.FeishuAuthSession
	savedConsoleUser domain.UserInput
	savedUser        domain.FeishuUser
	session          domain.FeishuAuthSession
	updatedSession   domain.FeishuAuthSession
}

func (r *fakeFeishuUserRepository) UpsertFeishuUser(_ context.Context, user domain.FeishuUser) (domain.FeishuUser, error) {
	r.savedUser = user
	return user, nil
}

func (r *fakeFeishuUserRepository) UpsertUser(_ context.Context, user domain.UserInput) error {
	r.savedConsoleUser = user
	return nil
}

func (r *fakeFeishuUserRepository) CreateFeishuAuthSession(_ context.Context, session domain.FeishuAuthSession) error {
	r.createdSession = session
	return nil
}

func (r *fakeFeishuUserRepository) FindFeishuAuthSession(context.Context, string) (domain.FeishuAuthSession, error) {
	return r.session, nil
}

func (r *fakeFeishuUserRepository) UpdateFeishuAuthSessionToken(_ context.Context, session domain.FeishuAuthSession) error {
	r.updatedSession = session
	return nil
}

func (r *fakeFeishuUserRepository) DeleteFeishuAuthSession(context.Context, string) error {
	return nil
}
