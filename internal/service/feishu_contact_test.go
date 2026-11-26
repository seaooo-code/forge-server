package service

import (
	"context"
	"errors"
	"testing"

	"forge-server/internal/domain"
)

func homeTenantSession() domain.ConsoleSession {
	return domain.ConsoleSession{
		User:                domain.User{ID: "user-1", TenantID: "xiaomi"},
		Identity:            domain.UserIdentity{Provider: domain.IdentityProviderFeishu, Issuer: "cli_xiaomi0001"},
		HomeTenant:          domain.TenantSummary{ID: "xiaomi"},
		ActiveTenant:        domain.TenantSummary{ID: "xiaomi"},
		ProviderAccessToken: "user-access-token",
	}
}

func TestFeishuContactServiceRejectsEmptyQuery(t *testing.T) {
	service := NewFeishuContactService(&fakeFeishuContactClient{}, &fakeAuthUserRepository{})

	_, err := service.SearchUsers(context.Background(), homeTenantSession(), "  ", 10, "")
	if !errors.Is(err, domain.ErrInvalidFeishuSearch) {
		t.Fatalf("expected invalid feishu search request, got %v", err)
	}
}

func TestFeishuContactServiceRejectsEmptyAccessToken(t *testing.T) {
	service := NewFeishuContactService(&fakeFeishuContactClient{}, &fakeAuthUserRepository{})
	session := homeTenantSession()
	session.ProviderAccessToken = ""

	_, err := service.SearchUsers(context.Background(), session, "li", 10, "")
	if !errors.Is(err, domain.ErrSessionExpired) {
		t.Fatalf("expected expired session, got %v", err)
	}
}

func TestFeishuContactServiceOnlyInHomeTenant(t *testing.T) {
	service := NewFeishuContactService(&fakeFeishuContactClient{}, &fakeAuthUserRepository{})
	session := homeTenantSession()
	session.ActiveTenant = domain.TenantSummary{ID: "other"}

	if _, err := service.SearchUsers(context.Background(), session, "li", 10, ""); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected search forbidden outside home tenant, got %v", err)
	}
	if _, err := service.ResolveUser(context.Background(), session, domain.FeishuUserSearchResult{OpenID: "ou_1", Name: "李四"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected resolve forbidden outside home tenant, got %v", err)
	}
}

func TestFeishuContactServiceCapsPageSize(t *testing.T) {
	client := &fakeFeishuContactClient{}
	service := NewFeishuContactService(client, &fakeAuthUserRepository{})

	_, err := service.SearchUsers(context.Background(), homeTenantSession(), "li", 500, "  cursor  ")
	if err != nil {
		t.Fatalf("search users: %v", err)
	}
	if client.pageSize != 200 || client.accessToken != "user-access-token" || client.pageToken != "cursor" {
		t.Fatalf("unexpected search call: %+v", client)
	}
}

func TestFeishuContactServiceResolveUserUsesLoginApp(t *testing.T) {
	users := &fakeAuthUserRepository{}
	service := NewFeishuContactService(&fakeFeishuContactClient{}, users)

	user, err := service.ResolveUser(context.Background(), homeTenantSession(), domain.FeishuUserSearchResult{OpenID: " ou_1 ", Name: "李四", UnionID: "on_1"})
	if err != nil {
		t.Fatalf("resolve user: %v", err)
	}
	if user.ID != "user-1" || users.resolvedTenantID != "xiaomi" || users.resolvedLogin {
		t.Fatalf("expected user resolved in home tenant without login, got %+v tenant=%q", user, users.resolvedTenantID)
	}
	if users.resolvedExternal.Issuer != "cli_xiaomi0001" || users.resolvedExternal.Subject != "ou_1" || users.resolvedExternal.Profile["unionId"] != "on_1" {
		t.Fatalf("expected identity of login app, got %+v", users.resolvedExternal)
	}
	if _, err := service.ResolveUser(context.Background(), homeTenantSession(), domain.FeishuUserSearchResult{Name: "无 open_id"}); !errors.Is(err, domain.ErrInvalidFeishuSearch) {
		t.Fatalf("expected missing open_id rejected, got %v", err)
	}
}

type fakeFeishuContactClient struct {
	accessToken string
	pageSize    int
	pageToken   string
}

func (c *fakeFeishuContactClient) SearchUsers(_ context.Context, accessToken string, _ string, pageSize int, pageToken string) (domain.FeishuUserSearchPage, error) {
	c.accessToken = accessToken
	c.pageSize = pageSize
	c.pageToken = pageToken
	return domain.FeishuUserSearchPage{}, nil
}
