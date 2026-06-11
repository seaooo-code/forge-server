package service

import (
	"context"
	"strings"

	"forge-server/internal/domain"
)

type FeishuContactClient interface {
	SearchUsers(ctx context.Context, accessToken string, query string, pageSize int, pageToken string) (domain.FeishuUserSearchPage, error)
}

type FeishuContactService struct {
	client FeishuContactClient
}

func NewFeishuContactService(client FeishuContactClient) *FeishuContactService {
	return &FeishuContactService{client: client}
}

func (s *FeishuContactService) SearchUsers(ctx context.Context, accessToken string, query string, pageSize int, pageToken string) (domain.FeishuUserSearchPage, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return domain.FeishuUserSearchPage{}, domain.ErrInvalidFeishuSearch
	}
	if strings.TrimSpace(accessToken) == "" {
		return domain.FeishuUserSearchPage{}, domain.ErrFeishuSessionExpired
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	return s.client.SearchUsers(ctx, accessToken, query, pageSize, strings.TrimSpace(pageToken))
}
