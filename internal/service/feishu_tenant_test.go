package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"forge-server/internal/domain"
)

func TestFeishuTenantServiceCreateEncryptsSecretAndHidesIt(t *testing.T) {
	store := newFakeFeishuTenantStore()
	service := NewFeishuTenantService(store, fakeSecretBox{})

	created, err := service.CreateFeishuTenant(context.Background(), domain.FeishuTenantInput{
		ID: "xiaomi", Name: " 小米 ", AppID: "cli_aaaaaaaaaaaa", AppSecret: "plain-secret",
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if created.AppSecret != "" || created.Status != domain.FeishuTenantStatusActive || created.Name != "小米" {
		t.Fatalf("unexpected created tenant: %+v", created)
	}
	if stored := store.tenants["xiaomi"].AppSecret; stored != "enc:plain-secret" {
		t.Fatalf("expected encrypted secret in store, got %q", stored)
	}
}

func TestFeishuTenantServiceRejectsInvalidInput(t *testing.T) {
	service := NewFeishuTenantService(newFakeFeishuTenantStore(), fakeSecretBox{})
	valid := domain.FeishuTenantInput{ID: "xiaomi", Name: "小米", AppID: "cli_aaaaaaaaaaaa", AppSecret: "s"}
	for name, mutate := range map[string]func(*domain.FeishuTenantInput){
		"bad id":         func(in *domain.FeishuTenantInput) { in.ID = "Xiaomi" },
		"bad app id":     func(in *domain.FeishuTenantInput) { in.AppID = "app_123" },
		"missing secret": func(in *domain.FeishuTenantInput) { in.AppSecret = "" },
		"empty name":     func(in *domain.FeishuTenantInput) { in.Name = " " },
		"long name":      func(in *domain.FeishuTenantInput) { in.Name = strings.Repeat("名", 65) },
		"bad status":     func(in *domain.FeishuTenantInput) { in.Status = "paused" },
	} {
		input := valid
		mutate(&input)
		if _, err := service.CreateFeishuTenant(context.Background(), input); !errors.Is(err, domain.ErrInvalidFeishuTenantInput) {
			t.Fatalf("%s: expected invalid input, got %v", name, err)
		}
	}
}

func TestFeishuTenantServiceUpdateKeepsAppIDAndSecret(t *testing.T) {
	store := newFakeFeishuTenantStore()
	store.tenants["xiaomi"] = domain.FeishuTenant{ID: "xiaomi", Name: "小米", AppID: "cli_aaaaaaaaaaaa", AppSecret: "enc:old", Status: domain.FeishuTenantStatusActive}
	service := NewFeishuTenantService(store, fakeSecretBox{})

	if _, err := service.UpdateFeishuTenant(context.Background(), "xiaomi", domain.FeishuTenantInput{
		Name: "小米集团", AppID: "cli_bbbbbbbbbbbb", Status: domain.FeishuTenantStatusActive,
	}); !errors.Is(err, domain.ErrInvalidFeishuTenantInput) {
		t.Fatalf("expected app id change to be rejected, got %v", err)
	}

	updated, err := service.UpdateFeishuTenant(context.Background(), "xiaomi", domain.FeishuTenantInput{
		Name: "小米集团", Status: domain.FeishuTenantStatusActive,
	})
	if err != nil {
		t.Fatalf("update tenant: %v", err)
	}
	if updated.Name != "小米集团" || store.lastUpdate.AppSecret != "" {
		t.Fatalf("expected name updated and secret untouched, got %+v / %+v", updated, store.lastUpdate)
	}
}

func TestFeishuTenantServiceResolveLoginTenant(t *testing.T) {
	store := newFakeFeishuTenantStore()
	store.tenants["xiaomi"] = domain.FeishuTenant{ID: "xiaomi", Name: "小米", AppID: "cli_aaaaaaaaaaaa", AppSecret: "enc:s1", Status: domain.FeishuTenantStatusActive}
	store.tenants["test"] = domain.FeishuTenant{ID: "test", Name: "测试", AppID: "cli_bbbbbbbbbbbb", AppSecret: "enc:s2", Status: domain.FeishuTenantStatusDisabled}
	service := NewFeishuTenantService(store, fakeSecretBox{})

	tenant, app, err := service.ResolveLoginTenant(context.Background(), "")
	if err != nil || tenant.ID != "xiaomi" || app.AppSecret != "s1" {
		t.Fatalf("expected the single active tenant, got %+v %+v %v", tenant, app, err)
	}
	if _, _, err := service.ResolveLoginTenant(context.Background(), "test"); !errors.Is(err, domain.ErrFeishuTenantNotFound) {
		t.Fatalf("expected disabled tenant rejected, got %v", err)
	}

	store.tenants["test"] = domain.FeishuTenant{ID: "test", Name: "测试", AppID: "cli_bbbbbbbbbbbb", AppSecret: "enc:s2", Status: domain.FeishuTenantStatusActive}
	if _, _, err := service.ResolveLoginTenant(context.Background(), ""); !errors.Is(err, domain.ErrFeishuTenantRequired) {
		t.Fatalf("expected tenant required with two active tenants, got %v", err)
	}
}

func TestFeishuTenantServiceSeedFromConfig(t *testing.T) {
	store := newFakeFeishuTenantStore()
	service := NewFeishuTenantService(store, fakeSecretBox{})

	if seeded, err := service.SeedFromConfig(context.Background(), "", ""); err != nil || seeded {
		t.Fatalf("expected no seed without config, got %v %v", seeded, err)
	}
	if seeded, err := service.SeedFromConfig(context.Background(), "cli_aaaaaaaaaaaa", "plain"); err != nil || !seeded {
		t.Fatalf("expected seed, got %v %v", seeded, err)
	}
	if tenant := store.tenants[domain.DefaultFeishuTenantID]; tenant.AppSecret != "enc:plain" || tenant.Status != domain.FeishuTenantStatusActive {
		t.Fatalf("unexpected seeded tenant: %+v", tenant)
	}
	if seeded, _ := service.SeedFromConfig(context.Background(), "cli_bbbbbbbbbbbb", "other"); seeded {
		t.Fatal("expected no seed once tenants exist")
	}
}

type fakeFeishuTenantStore struct {
	tenants    map[string]domain.FeishuTenant
	lastUpdate domain.FeishuTenant
}

func newFakeFeishuTenantStore() *fakeFeishuTenantStore {
	return &fakeFeishuTenantStore{tenants: map[string]domain.FeishuTenant{}}
}

func (s *fakeFeishuTenantStore) CreateFeishuTenant(_ context.Context, tenant domain.FeishuTenant) (domain.FeishuTenant, error) {
	if _, ok := s.tenants[tenant.ID]; ok {
		return domain.FeishuTenant{}, domain.ErrFeishuTenantIDConflict
	}
	s.tenants[tenant.ID] = tenant
	return tenant, nil
}

func (s *fakeFeishuTenantStore) GetFeishuTenant(_ context.Context, id string) (domain.FeishuTenant, error) {
	tenant, ok := s.tenants[id]
	if !ok {
		return domain.FeishuTenant{}, domain.ErrFeishuTenantNotFound
	}
	return tenant, nil
}

func (s *fakeFeishuTenantStore) ListFeishuTenants(context.Context) ([]domain.FeishuTenant, error) {
	tenants := []domain.FeishuTenant{}
	for _, id := range []string{"default", "test", "xiaomi"} {
		if tenant, ok := s.tenants[id]; ok {
			tenants = append(tenants, tenant)
		}
	}
	return tenants, nil
}

func (s *fakeFeishuTenantStore) SeedFeishuTenant(_ context.Context, tenant domain.FeishuTenant) (bool, error) {
	if len(s.tenants) > 0 {
		return false, nil
	}
	s.tenants[tenant.ID] = tenant
	return true, nil
}

func (s *fakeFeishuTenantStore) UpdateFeishuTenant(_ context.Context, tenant domain.FeishuTenant) (domain.FeishuTenant, error) {
	s.lastUpdate = tenant
	existing := s.tenants[tenant.ID]
	existing.Name, existing.Status, existing.SortOrder = tenant.Name, tenant.Status, tenant.SortOrder
	if tenant.AppSecret != "" {
		existing.AppSecret = tenant.AppSecret
	}
	s.tenants[tenant.ID] = existing
	return existing, nil
}
