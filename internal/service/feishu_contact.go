package service

import (
	"context"
	"strings"

	"forge-server/internal/domain"
)

type FeishuContactClient interface {
	SearchUsers(ctx context.Context, accessToken string, query string, pageSize int, pageToken string) (domain.FeishuUserSearchPage, error)
}

type DirectoryUserRepository interface {
	ResolveExternalUser(ctx context.Context, tenantID string, external domain.ExternalUser, login bool) (domain.User, domain.UserIdentity, error)
	SearchUsers(ctx context.Context, query string, limit int) ([]domain.User, error)
}

// FeishuContactService 搜索飞书通讯录，并把选中的人写成当前租户的用户。
// 搜索用的是当前用户自己的飞书 token，所以只能在用户所属租户里使用。
type FeishuContactService struct {
	client FeishuContactClient
	users  DirectoryUserRepository
}

func NewFeishuContactService(client FeishuContactClient, users DirectoryUserRepository) *FeishuContactService {
	return &FeishuContactService{client: client, users: users}
}

func (s *FeishuContactService) SearchUsers(ctx context.Context, session domain.ConsoleSession, query string, pageSize int, pageToken string) (domain.FeishuUserSearchPage, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return domain.FeishuUserSearchPage{}, domain.ErrInvalidFeishuSearch
	}
	if !session.InHomeTenant() || session.Identity.Provider != domain.IdentityProviderFeishu {
		return domain.FeishuUserSearchPage{}, domain.ErrForbidden
	}
	if strings.TrimSpace(session.ProviderAccessToken) == "" {
		return domain.FeishuUserSearchPage{}, domain.ErrSessionExpired
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return s.client.SearchUsers(ctx, session.ProviderAccessToken, query, pageSize, strings.TrimSpace(pageToken))
}

// ResolveUser 把通讯录搜索结果写成当前租户的用户，返回 Forge 用户。
// open_id 按飞书应用隔离，所以身份的 issuer 取当前用户登录所用的飞书 App ID。
func (s *FeishuContactService) ResolveUser(ctx context.Context, session domain.ConsoleSession, result domain.FeishuUserSearchResult) (domain.User, error) {
	if !session.InHomeTenant() || session.Identity.Provider != domain.IdentityProviderFeishu {
		return domain.User{}, domain.ErrForbidden
	}
	result.OpenID = strings.TrimSpace(result.OpenID)
	result.Name = strings.TrimSpace(result.Name)
	if result.OpenID == "" || result.Name == "" {
		return domain.User{}, domain.ErrInvalidFeishuSearch
	}
	user, _, err := s.users.ResolveExternalUser(ctx, session.HomeTenant.ID, result.ExternalUser(session.Identity.Issuer), false)
	return user, err
}

// SearchLocalUsers 在当前租户已有的用户里搜索，SuperAdmin 进入其他租户时用它选人。
func (s *FeishuContactService) SearchLocalUsers(ctx context.Context, query string, limit int) ([]domain.User, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	return s.users.SearchUsers(ctx, strings.TrimSpace(query), limit)
}
