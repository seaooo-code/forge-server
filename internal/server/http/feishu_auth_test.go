package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/feishu/authorize?tenant=xiaomi&redirect="+url.QueryEscape("https://forge.example.com/login/feishu/callback"), nil)
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

	if service.authorizeTenant != "xiaomi" {
		t.Fatalf("expected tenant from query, got %q", service.authorizeTenant)
	}
	cookies := map[string]string{}
	for _, cookie := range rec.Result().Cookies() {
		cookies[cookie.Name] = cookie.Value
	}
	if cookies[feishuOAuthStateCookieName] != service.authorizeState || cookies[feishuOAuthTenantCookieName] != "xiaomi" ||
		cookies[feishuOAuthRedirectCookieName] != "https://forge.example.com/login/feishu/callback" {
		t.Fatalf("expected state and tenant cookies, got %#v", cookies)
	}
}

func TestFeishuAuthorizeRejectsInvalidRedirect(t *testing.T) {
	e := New(config.Config{}, Dependencies{FeishuAuthService: &fakeFeishuAuthService{}})

	for _, redirect := range []string{"", "https://forge.example.com/other", "javascript:alert(1)", "https://forge.example.com/login/feishu/callback?x=1"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/feishu/authorize?redirect="+url.QueryEscape(redirect), nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("redirect %q: expected 400, got %d", redirect, rec.Code)
		}
	}
}

func TestFeishuAuthorizeRequiresTenantWhenSeveralActive(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		FeishuAuthService: &fakeFeishuAuthService{authorizeErr: domain.ErrFeishuTenantRequired},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/feishu/authorize", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "feishu tenant required") {
		t.Fatalf("expected tenant required, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFeishuLoginTenantsListsActiveTenantsWithoutCredentials(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		FeishuTenantService: &fakeFeishuTenantService{active: []domain.FeishuTenant{
			{ID: "xiaomi", Name: "小米", AppID: "cli_aaaaaaaaaaaa", AppSecret: "secret", Status: domain.FeishuTenantStatusActive},
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/feishu/tenants", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "{\"tenants\":[{\"id\":\"xiaomi\",\"name\":\"小米\"}]}\n" {
		t.Fatalf("expected id and name only, got %s", body)
	}
}

func TestFeishuCallbackCompletesAuthorization(t *testing.T) {
	service := &fakeFeishuAuthService{
		result: domain.FeishuAuthResult{
			SessionExpiresAt: time.Now().Add(time.Hour),
			SessionToken:     "session-token",
			Tenant:           domain.FeishuTenantSummary{ID: "xiaomi", Name: "小米"},
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
	req.AddCookie(&http.Cookie{Name: feishuOAuthTenantCookieName, Value: "xiaomi"})
	req.AddCookie(&http.Cookie{Name: feishuOAuthRedirectCookieName, Value: "https://forge.example.com/login/feishu/callback"})
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if service.callbackCode != "auth-code" || service.callbackTenant != "xiaomi" || service.callbackRedirect != "https://forge.example.com/login/feishu/callback" {
		t.Fatalf("expected callback code and tenant, got %q %q", service.callbackCode, service.callbackTenant)
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
	if response.Tenant.ID != "xiaomi" || response.Tenant.Name != "小米" {
		t.Fatalf("expected tenant in response, got %+v", response.Tenant)
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

func TestFeishuSessionTreatsAuthorizationFailureAsUnauthorized(t *testing.T) {
	service := &fakeFeishuAuthService{sessionErr: domain.ErrFeishuAuthorization}
	e := New(config.Config{}, Dependencies{
		FeishuAuthService: service,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: feishuSessionCookieName, Value: "session-token"})
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
	if service.sessionToken != "session-token" {
		t.Fatalf("expected session token, got %q", service.sessionToken)
	}
	assertClearedSessionCookie(t, rec)
}

func TestFeishuCallbackRejectsMissingTenantCookie(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		FeishuAuthService: &fakeFeishuAuthService{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/feishu/callback?code=auth-code&state=good", nil)
	req.AddCookie(&http.Cookie{Name: feishuOAuthStateCookieName, Value: "good"})
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
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

func assertClearedSessionCookie(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == feishuSessionCookieName {
			if cookie.MaxAge < 0 {
				return
			}
			t.Fatalf("expected cleared session cookie, got %#v", cookie)
		}
	}
	t.Fatalf("expected cleared session cookie, got %#v", rec.Result().Cookies())
}

type fakeFeishuAuthService struct {
	authorizeErr     error
	authorizeState   string
	authorizeTenant  string
	callbackCode     string
	callbackTenant   string
	callbackRedirect string
	sessionErr       error
	sessionToken     string
	result           domain.FeishuAuthResult
}

func (s *fakeFeishuAuthService) AuthorizationURL(_ context.Context, tenantID string, redirectURI string, state string) (string, string, error) {
	if s.authorizeErr != nil {
		return "", "", s.authorizeErr
	}
	if !domain.IsValidFeishuRedirectURI(redirectURI) {
		return "", "", domain.ErrInvalidFeishuRedirectURI
	}
	s.authorizeState = state
	s.authorizeTenant = tenantID
	return "https://accounts.feishu.cn/open-apis/authen/v1/authorize?state=" + state, tenantID, nil
}

func (s *fakeFeishuAuthService) CompleteAuthorization(_ context.Context, tenantID string, redirectURI string, code string) (domain.FeishuAuthResult, error) {
	s.callbackCode = code
	s.callbackTenant = tenantID
	s.callbackRedirect = redirectURI
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
