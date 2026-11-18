package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forge-server/internal/domain"
)

func TestFeishuTenantListNeverReturnsSecret(t *testing.T) {
	service := &fakeFeishuTenantService{all: []domain.FeishuTenant{
		{ID: "xiaomi", Name: "小米", AppID: "cli_aaaaaaaaaaaa", AppSecret: "should-not-leak", Status: domain.FeishuTenantStatusActive},
	}}
	e := newAuthorizedTestServer(Dependencies{FeishuTenantService: service})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/feishu-tenants", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "should-not-leak") || strings.Contains(rec.Body.String(), "appSecret") {
		t.Fatalf("response must not contain app secret: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"appId":"cli_aaaaaaaaaaaa"`) {
		t.Fatalf("expected app id in response: %s", rec.Body.String())
	}
}

func TestFeishuTenantRoutesRequireManagePermission(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		AuthorizationService: fakeAuthorizationService{err: domain.ErrForbidden},
		FeishuTenantService:  &fakeFeishuTenantService{},
	})

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/feishu-tenants", ""},
		{http.MethodPost, "/api/v1/feishu-tenants", `{"id":"xiaomi"}`},
		{http.MethodPatch, "/api/v1/feishu-tenants/xiaomi", `{"name":"小米"}`},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		addTestConsoleSession(req)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s: expected 403, got %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestFeishuTenantUpdateMapsLastActiveToConflict(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{FeishuTenantService: &fakeFeishuTenantService{err: domain.ErrFeishuTenantLastActive}})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/feishu-tenants/xiaomi", strings.NewReader(`{"name":"小米","status":"disabled"}`))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

type fakeFeishuTenantService struct {
	active []domain.FeishuTenant
	all    []domain.FeishuTenant
	err    error
}

func (s *fakeFeishuTenantService) CreateFeishuTenant(_ context.Context, input domain.FeishuTenantInput) (domain.FeishuTenant, error) {
	return domain.FeishuTenant{ID: input.ID, Name: input.Name, AppID: input.AppID, Status: input.Status}, s.err
}

func (s *fakeFeishuTenantService) ListActiveFeishuTenants(context.Context) ([]domain.FeishuTenant, error) {
	return s.active, s.err
}

func (s *fakeFeishuTenantService) ListFeishuTenants(context.Context) ([]domain.FeishuTenant, error) {
	return s.all, s.err
}

func (s *fakeFeishuTenantService) UpdateFeishuTenant(_ context.Context, id string, input domain.FeishuTenantInput) (domain.FeishuTenant, error) {
	if s.err != nil {
		return domain.FeishuTenant{}, s.err
	}
	return domain.FeishuTenant{ID: id, Name: input.Name, Status: input.Status}, nil
}
