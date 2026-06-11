package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

func TestSearchFeishuUsers(t *testing.T) {
	authService := &fakeFeishuAuthService{
		result: testFeishuContactAuthResult(),
	}
	contactService := fakeFeishuContactService{
		page: domain.FeishuUserSearchPage{
			Users: []domain.FeishuUserSearchResult{
				{
					UnionID:       "ou-test",
					OpenID:        "open-test",
					UserID:        "user-test",
					Name:          "李明",
					Email:         "liming@example.com",
					DepartmentIDs: []string{"od-rn-platform"},
				},
			},
			HasMore:   true,
			PageToken: "next-page",
		},
	}
	e := New(config.Config{}, Dependencies{
		FeishuAuthService:    authService,
		FeishuContactService: &contactService,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/feishu/users/search?query=li&page_size=5&page_token=cursor", nil)
	req.AddCookie(&http.Cookie{Name: feishuSessionCookieName, Value: "session-token"})
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response feishuUserSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(response.Users))
	}
	if response.Users[0].ID != "user-test" {
		t.Fatalf("expected user id, got %q", response.Users[0].ID)
	}
	if response.Users[0].DepartmentIDs[0] != "od-rn-platform" {
		t.Fatalf("expected department id, got %#v", response.Users[0].DepartmentIDs)
	}
	if !response.HasMore {
		t.Fatal("expected has more")
	}
	if response.PageToken != "next-page" {
		t.Fatalf("expected page token, got %q", response.PageToken)
	}
	if contactService.accessToken != "user-access-token" {
		t.Fatalf("expected user access token, got %q", contactService.accessToken)
	}
	if contactService.pageSize != 5 {
		t.Fatalf("expected page size 5, got %d", contactService.pageSize)
	}
	if contactService.pageToken != "cursor" {
		t.Fatalf("expected page token cursor, got %q", contactService.pageToken)
	}
	if authService.sessionToken != "session-token" {
		t.Fatalf("expected session token, got %q", authService.sessionToken)
	}
}

func TestSearchFeishuUsersRejectsInvalidQuery(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		FeishuAuthService:    &fakeFeishuAuthService{result: testFeishuContactAuthResult()},
		FeishuContactService: &fakeFeishuContactService{err: domain.ErrInvalidFeishuSearch},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/feishu/users/search", nil)
	req.AddCookie(&http.Cookie{Name: feishuSessionCookieName, Value: "session-token"})
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestSearchFeishuUsersRejectsMissingSession(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		FeishuAuthService:    &fakeFeishuAuthService{result: testFeishuContactAuthResult()},
		FeishuContactService: &fakeFeishuContactService{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/feishu/users/search?query=li", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

func TestSearchFeishuUsersRejectsEmptyAccessToken(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		FeishuAuthService: &fakeFeishuAuthService{
			result: domain.FeishuAuthResult{
				SessionExpiresAt: time.Now().Add(time.Hour),
				SessionToken:     "session-token",
			},
		},
		FeishuContactService: &fakeFeishuContactService{err: domain.ErrFeishuSessionExpired},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/feishu/users/search?query=li", nil)
	req.AddCookie(&http.Cookie{Name: feishuSessionCookieName, Value: "session-token"})
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

func TestSearchFeishuUsersReturnsBadGatewayForFeishuError(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		FeishuAuthService: &fakeFeishuAuthService{result: testFeishuContactAuthResult()},
		FeishuContactService: &fakeFeishuContactService{err: &domain.FeishuAPIError{
			Code:     20027,
			Endpoint: "search.v1.user",
			Msg:      "current app permission is insufficient",
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/feishu/users/search?query=li", nil)
	req.AddCookie(&http.Cookie{Name: feishuSessionCookieName, Value: "session-token"})
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadGateway, rec.Code, rec.Body.String())
	}

	var response feishuErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.FeishuCode != 20027 {
		t.Fatalf("expected feishu code, got %d", response.FeishuCode)
	}
	if response.FeishuMsg != "current app permission is insufficient" {
		t.Fatalf("expected feishu msg, got %q", response.FeishuMsg)
	}
}

type fakeFeishuContactService struct {
	accessToken string
	pageSize    int
	pageToken   string
	page        domain.FeishuUserSearchPage
	err         error
}

func (s *fakeFeishuContactService) SearchUsers(_ context.Context, accessToken string, _ string, pageSize int, pageToken string) (domain.FeishuUserSearchPage, error) {
	s.accessToken = accessToken
	s.pageSize = pageSize
	s.pageToken = pageToken
	if s.err != nil {
		return domain.FeishuUserSearchPage{}, s.err
	}
	return s.page, nil
}

func testFeishuContactAuthResult() domain.FeishuAuthResult {
	return domain.FeishuAuthResult{
		SessionExpiresAt: time.Now().Add(time.Hour),
		SessionToken:     "session-token",
		Token: domain.FeishuToken{
			AccessToken: "user-access-token",
			ExpiresIn:   7200,
			TokenType:   "Bearer",
		},
	}
}
