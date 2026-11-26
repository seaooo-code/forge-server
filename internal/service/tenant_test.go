package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"forge-server/internal/domain"
)

func TestTenantServiceCreateEncryptsSecretAndHidesIt(t *testing.T) {
	store := newFakeFeishuTenantStore()
	service := NewTenantService(store, fakeSecretBox{})

	created, err := service.CreateTenant(context.Background(), domain.TenantInput{
		ID: "xiaomi", Name: " 小米 ", FeishuAppID: "cli_aaaaaaaaaaaa", FeishuAppSecret: "plain-secret",
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if created.FeishuAppSecret != "" || created.Status != domain.TenantStatusActive || created.Name != "小米" {
		t.Fatalf("unexpected created tenant: %+v", created)
	}
	if stored := store.tenants["xiaomi"].FeishuAppSecret; stored != "enc:plain-secret" {
		t.Fatalf("expected encrypted secret in store, got %q", stored)
	}
}

func TestTenantServiceRejectsInvalidInput(t *testing.T) {
	service := NewTenantService(newFakeFeishuTenantStore(), fakeSecretBox{})
	valid := domain.TenantInput{ID: "xiaomi", Name: "小米", FeishuAppID: "cli_aaaaaaaaaaaa", FeishuAppSecret: "s"}
	for name, mutate := range map[string]func(*domain.TenantInput){
		"bad id":         func(in *domain.TenantInput) { in.ID = "Xiaomi" },
		"bad app id":     func(in *domain.TenantInput) { in.FeishuAppID = "app_123" },
		"missing secret": func(in *domain.TenantInput) { in.FeishuAppSecret = "" },
		"empty name":     func(in *domain.TenantInput) { in.Name = " " },
		"long name":      func(in *domain.TenantInput) { in.Name = strings.Repeat("名", 65) },
		"bad status":     func(in *domain.TenantInput) { in.Status = "paused" },
	} {
		input := valid
		mutate(&input)
		if _, err := service.CreateTenant(context.Background(), input); !errors.Is(err, domain.ErrInvalidTenantInput) {
			t.Fatalf("%s: expected invalid input, got %v", name, err)
		}
	}
}

func TestTenantServiceUpdateKeepsAppIDAndSecret(t *testing.T) {
	store := newFakeFeishuTenantStore()
	store.tenants["xiaomi"] = domain.Tenant{ID: "xiaomi", Name: "小米", FeishuAppID: "cli_aaaaaaaaaaaa", FeishuAppSecret: "enc:old", Status: domain.TenantStatusActive}
	service := NewTenantService(store, fakeSecretBox{})

	if _, err := service.UpdateTenant(context.Background(), "xiaomi", domain.TenantInput{
		Name: "小米集团", FeishuAppID: "cli_bbbbbbbbbbbb", Status: domain.TenantStatusActive,
	}); !errors.Is(err, domain.ErrInvalidTenantInput) {
		t.Fatalf("expected app id change to be rejected, got %v", err)
	}

	updated, err := service.UpdateTenant(context.Background(), "xiaomi", domain.TenantInput{
		Name: "小米集团", Status: domain.TenantStatusActive,
	})
	if err != nil {
		t.Fatalf("update tenant: %v", err)
	}
	if updated.Name != "小米集团" || store.lastUpdate.FeishuAppSecret != "" {
		t.Fatalf("expected name updated and secret untouched, got %+v / %+v", updated, store.lastUpdate)
	}
}

func TestTenantServiceResolveLoginTenant(t *testing.T) {
	store := newFakeFeishuTenantStore()
	store.tenants["xiaomi"] = domain.Tenant{ID: "xiaomi", Name: "小米", FeishuAppID: "cli_aaaaaaaaaaaa", FeishuAppSecret: "enc:s1", Status: domain.TenantStatusActive}
	store.tenants["test"] = domain.Tenant{ID: "test", Name: "测试", FeishuAppID: "cli_bbbbbbbbbbbb", FeishuAppSecret: "enc:s2", Status: domain.TenantStatusDisabled}
	service := NewTenantService(store, fakeSecretBox{})

	tenant, app, err := service.ResolveLoginTenant(context.Background(), "")
	if err != nil || tenant.ID != "xiaomi" || app.AppSecret != "s1" {
		t.Fatalf("expected the single active tenant, got %+v %+v %v", tenant, app, err)
	}
	if _, _, err := service.ResolveLoginTenant(context.Background(), "test"); !errors.Is(err, domain.ErrTenantNotFound) {
		t.Fatalf("expected disabled tenant rejected, got %v", err)
	}

	store.tenants["test"] = domain.Tenant{ID: "test", Name: "测试", FeishuAppID: "cli_bbbbbbbbbbbb", FeishuAppSecret: "enc:s2", Status: domain.TenantStatusActive}
	if _, _, err := service.ResolveLoginTenant(context.Background(), ""); !errors.Is(err, domain.ErrTenantRequired) {
		t.Fatalf("expected tenant required with two active tenants, got %v", err)
	}
}

func TestTenantServiceSetupFirstTenant(t *testing.T) {
	store := newFakeFeishuTenantStore()
	service := NewTenantService(store, fakeSecretBox{})
	input := domain.TenantInput{ID: "xiaomi", Name: "小米", FeishuAppID: "cli_aaaaaaaaaaaa", FeishuAppSecret: "plain"}

	if _, err := service.SetupFirstTenant(context.Background(), "", input); !errors.Is(err, domain.ErrPlatformSetupCompleted) {
		t.Fatalf("expected setup closed before PrepareSetup, got %v", err)
	}
	token, err := service.PrepareSetup(context.Background())
	if err != nil || len(token) < 32 {
		t.Fatalf("expected setup token, got %q %v", token, err)
	}
	if _, err := service.SetupFirstTenant(context.Background(), "wrong", input); !errors.Is(err, domain.ErrInvalidSetupToken) {
		t.Fatalf("expected wrong token rejected, got %v", err)
	}
	created, err := service.SetupFirstTenant(context.Background(), token, input)
	if err != nil || created.ID != "xiaomi" || created.FeishuAppSecret != "" {
		t.Fatalf("setup first tenant: %+v %v", created, err)
	}
	if stored := store.tenants["xiaomi"]; stored.FeishuAppSecret != "enc:plain" || stored.Status != domain.TenantStatusActive {
		t.Fatalf("unexpected stored tenant: %+v", stored)
	}
	if _, err := service.SetupFirstTenant(context.Background(), token, input); !errors.Is(err, domain.ErrPlatformSetupCompleted) {
		t.Fatalf("expected token single use, got %v", err)
	}
	if again, _ := service.PrepareSetup(context.Background()); again != "" {
		t.Fatal("expected no setup token once a tenant exists")
	}
}

type fakeFeishuTenantStore struct {
	tenants    map[string]domain.Tenant
	lastUpdate domain.Tenant
}

func newFakeFeishuTenantStore() *fakeFeishuTenantStore {
	return &fakeFeishuTenantStore{tenants: map[string]domain.Tenant{}}
}

func (s *fakeFeishuTenantStore) CreateTenant(_ context.Context, tenant domain.Tenant) (domain.Tenant, error) {
	if _, ok := s.tenants[tenant.ID]; ok {
		return domain.Tenant{}, domain.ErrTenantIDConflict
	}
	s.tenants[tenant.ID] = tenant
	return tenant, nil
}

func (s *fakeFeishuTenantStore) GetTenant(_ context.Context, id string) (domain.Tenant, error) {
	tenant, ok := s.tenants[id]
	if !ok {
		return domain.Tenant{}, domain.ErrTenantNotFound
	}
	return tenant, nil
}

func (s *fakeFeishuTenantStore) ListTenants(context.Context) ([]domain.Tenant, error) {
	tenants := []domain.Tenant{}
	for _, id := range []string{"default", "test", "xiaomi"} {
		if tenant, ok := s.tenants[id]; ok {
			tenants = append(tenants, tenant)
		}
	}
	return tenants, nil
}

func (s *fakeFeishuTenantStore) CreateFirstTenant(_ context.Context, tenant domain.Tenant) (bool, error) {
	if len(s.tenants) > 0 {
		return false, nil
	}
	s.tenants[tenant.ID] = tenant
	return true, nil
}

func (s *fakeFeishuTenantStore) UpdateTenant(_ context.Context, tenant domain.Tenant) (domain.Tenant, error) {
	s.lastUpdate = tenant
	existing := s.tenants[tenant.ID]
	existing.Name, existing.Status, existing.SortOrder = tenant.Name, tenant.Status, tenant.SortOrder
	if tenant.FeishuAppSecret != "" {
		existing.FeishuAppSecret = tenant.FeishuAppSecret
	}
	s.tenants[tenant.ID] = existing
	return existing, nil
}
