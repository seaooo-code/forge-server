package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forge-server/internal/domain"
)

func TestTenantListNeverReturnsSecret(t *testing.T) {
	service := &fakeTenantService{all: []domain.Tenant{
		{ID: "xiaomi", Name: "小米", FeishuAppID: "cli_aaaaaaaaaaaa", FeishuAppSecret: "should-not-leak", Status: domain.TenantStatusActive},
	}}
	e := newAuthorizedTestServer(Dependencies{TenantService: service})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "should-not-leak") || strings.Contains(rec.Body.String(), "feishuAppSecret") {
		t.Fatalf("response must not contain app secret: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"feishuAppId":"cli_aaaaaaaaaaaa"`) {
		t.Fatalf("expected app id in response: %s", rec.Body.String())
	}
}

func TestTenantRoutesRequireManagePermission(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		AuthorizationService: fakeAuthorizationService{err: domain.ErrForbidden},
		TenantService:        &fakeTenantService{},
	})

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/tenants", ""},
		{http.MethodPost, "/api/v1/tenants", `{"id":"xiaomi"}`},
		{http.MethodPatch, "/api/v1/tenants/xiaomi", `{"name":"小米"}`},
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

func TestTenantUpdateMapsLastActiveToConflict(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{TenantService: &fakeTenantService{err: domain.ErrTenantLastActive}})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tenants/xiaomi", strings.NewReader(`{"name":"小米","status":"disabled"}`))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

type fakeTenantService struct {
	active        []domain.Tenant
	all           []domain.Tenant
	err           error
	setupRequired bool
	setupToken    string
}

func (s *fakeTenantService) SetupRequired(context.Context) (bool, error) {
	return s.setupRequired, nil
}

func (s *fakeTenantService) SetupFirstTenant(_ context.Context, token string, input domain.TenantInput) (domain.Tenant, error) {
	if !s.setupRequired {
		return domain.Tenant{}, domain.ErrPlatformSetupCompleted
	}
	if token != s.setupToken {
		return domain.Tenant{}, domain.ErrInvalidSetupToken
	}
	s.setupRequired = false
	return domain.Tenant{ID: input.ID, Name: input.Name, FeishuAppID: input.FeishuAppID, Status: domain.TenantStatusActive}, nil
}

func (s *fakeTenantService) CreateTenant(_ context.Context, input domain.TenantInput) (domain.Tenant, error) {
	return domain.Tenant{ID: input.ID, Name: input.Name, FeishuAppID: input.FeishuAppID, Status: input.Status}, s.err
}

func (s *fakeTenantService) ListActiveTenants(context.Context) ([]domain.Tenant, error) {
	return s.active, s.err
}

func (s *fakeTenantService) ListTenants(context.Context) ([]domain.Tenant, error) {
	return s.all, s.err
}

func (s *fakeTenantService) UpdateTenant(_ context.Context, id string, input domain.TenantInput) (domain.Tenant, error) {
	if s.err != nil {
		return domain.Tenant{}, s.err
	}
	return domain.Tenant{ID: id, Name: input.Name, Status: input.Status}, nil
}
