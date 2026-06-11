package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

func TestFeishuAuthorizeRedirectsWithStateCookie(t *testing.T) {
	service := &fakeFeishuAuthService{}
	e := New(config.Config{}, Dependencies{
		FeishuAuthService: service,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/feishu/authorize", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusFound, rec.Code, rec.Body.String())
	}
	if service.authorizeState == "" {
		t.Fatal("expected generated state")
	}
	if location := rec.Header().Get("Location"); !strings.Contains(location, service.authorizeState) {
		t.Fatalf("expected redirect location to include state, got %q", location)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != feishuOAuthStateCookieName || cookies[0].Value != service.authorizeState {
		t.Fatalf("expected state cookie, got %#v", cookies)
	}
}

func TestFeishuCallbackCompletesAuthorization(t *testing.T) {
	service := &fakeFeishuAuthService{
		result: domain.FeishuAuthResult{
			SessionExpiresAt: time.Now().Add(time.Hour),
			SessionToken:     "session-token",
			Token: domain.FeishuToken{
				AccessToken:  "secret-token",
				RefreshToken: "secret-refresh-token",
				ExpiresIn:    7200,
				TokenType:    "Bearer",
				Scope:        "offline_access",
			},
			User: domain.FeishuUser{
				UnionID:   "on_union",
				OpenID:    "ou_open",
				UserID:    "user_id",
				Name:      "张三",
				TenantKey: "tenant",
			},
		},
	}
	e := New(config.Config{}, Dependencies{
		FeishuAuthService: service,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/feishu/callback?code=auth-code&state=state-value", nil)
	req.AddCookie(&http.Cookie{Name: feishuOAuthStateCookieName, Value: "state-value"})
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if service.callbackCode != "auth-code" {
		t.Fatalf("expected callback code, got %q", service.callbackCode)
	}

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	var response feishuAuthResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.User.UnionID != "on_union" {
		t.Fatalf("expected union id, got %q", response.User.UnionID)
	}
	if !response.Token.HasRefreshToken {
		t.Fatal("expected refresh token flag")
	}
	if strings.Contains(string(body), "secret-token") {
		t.Fatal("response must not expose access token")
	}

	var hasSessionCookie bool
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == feishuSessionCookieName && cookie.Value == "session-token" {
			hasSessionCookie = true
		}
	}
	if !hasSessionCookie {
		t.Fatalf("expected session cookie, got %#v", rec.Result().Cookies())
	}
}

func TestFeishuSessionReturnsCurrentSession(t *testing.T) {
	service := &fakeFeishuAuthService{
		result: domain.FeishuAuthResult{
			SessionExpiresAt: time.Now().Add(time.Hour),
			SessionToken:     "session-token",
			Token: domain.FeishuToken{
				RefreshToken: "secret-refresh-token",
				ExpiresIn:    3600,
				TokenType:    "Bearer",
			},
			User: domain.FeishuUser{
				UnionID: "on_union",
				Name:    "张三",
			},
		},
	}
	e := New(config.Config{}, Dependencies{
		FeishuAuthService: service,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: feishuSessionCookieName, Value: "session-token"})
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if service.sessionToken != "session-token" {
		t.Fatalf("expected session token, got %q", service.sessionToken)
	}
}

func TestFeishuCallbackRejectsInvalidState(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		FeishuAuthService: &fakeFeishuAuthService{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/feishu/callback?code=auth-code&state=bad", nil)
	req.AddCookie(&http.Cookie{Name: feishuOAuthStateCookieName, Value: "good"})
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

type fakeFeishuAuthService struct {
	authorizeState string
	callbackCode   string
	sessionErr     error
	sessionToken   string
	result         domain.FeishuAuthResult
}

func (s *fakeFeishuAuthService) AuthorizationURL(state string) (string, error) {
	s.authorizeState = state
	return "https://accounts.feishu.cn/open-apis/authen/v1/authorize?state=" + state, nil
}

func (s *fakeFeishuAuthService) CompleteAuthorization(_ context.Context, code string) (domain.FeishuAuthResult, error) {
	s.callbackCode = code
	return s.result, nil
}

func (s *fakeFeishuAuthService) GetSession(_ context.Context, sessionToken string) (domain.FeishuAuthResult, error) {
	s.sessionToken = sessionToken
	if s.sessionErr != nil {
		return domain.FeishuAuthResult{}, s.sessionErr
	}
	return s.result, nil
}

func (s *fakeFeishuAuthService) Logout(_ context.Context, sessionToken string) error {
	s.sessionToken = sessionToken
	return nil
}
