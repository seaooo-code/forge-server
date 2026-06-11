package service

import (
	"context"
	"errors"
	"testing"

	"forge-server/internal/domain"
)

func TestFeishuContactServiceRejectsEmptyQuery(t *testing.T) {
	service := NewFeishuContactService(&fakeFeishuContactClient{})

	_, err := service.SearchUsers(context.Background(), "user-access-token", "  ", 10, "")
	if !errors.Is(err, domain.ErrInvalidFeishuSearch) {
		t.Fatalf("expected invalid feishu search request, got %v", err)
	}
}

func TestFeishuContactServiceRejectsEmptyAccessToken(t *testing.T) {
	service := NewFeishuContactService(&fakeFeishuContactClient{})

	_, err := service.SearchUsers(context.Background(), "", "li", 10, "")
	if !errors.Is(err, domain.ErrFeishuSessionExpired) {
		t.Fatalf("expected expired feishu session, got %v", err)
	}
}

func TestFeishuContactServiceCapsPageSize(t *testing.T) {
	client := &fakeFeishuContactClient{}
	service := NewFeishuContactService(client)

	_, err := service.SearchUsers(context.Background(), "user-access-token", "li", 500, "  cursor  ")
	if err != nil {
		t.Fatalf("search users: %v", err)
	}

	if client.pageSize != 200 {
		t.Fatalf("expected page size 200, got %d", client.pageSize)
	}
	if client.accessToken != "user-access-token" {
		t.Fatalf("expected user access token, got %q", client.accessToken)
	}
	if client.pageToken != "cursor" {
		t.Fatalf("expected trimmed page token, got %q", client.pageToken)
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
