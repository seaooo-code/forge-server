package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"forge-server/internal/domain"
)

// 租户与会话：会话读取时带出租户状态；停用租户会删除它的会话；不能停用最后一个启用租户；app_id 唯一。
func TestIntegrationFeishuTenantLifecycle(t *testing.T) {
	pool := newIntegrationPool(t)
	ctx := context.Background()
	tenants := NewFeishuTenantRepository(pool)
	users := NewFeishuUserRepository(pool)

	// 测试库独占：清空租户（级联删除会话），保证"最后一个启用租户"的判断只看本测试的数据。
	if _, err := pool.Exec(ctx, `delete from feishu_tenants`); err != nil {
		t.Fatalf("reset tenants: %v", err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)

	seeded, err := tenants.SeedFeishuTenant(ctx, domain.FeishuTenant{ID: "default", Name: "默认", AppID: "cli_seed" + suffix, AppSecret: "v1:x", Status: domain.FeishuTenantStatusActive})
	if err != nil || !seeded {
		t.Fatalf("seed into empty table: seeded=%v err=%v", seeded, err)
	}
	if seeded, err := tenants.SeedFeishuTenant(ctx, domain.FeishuTenant{ID: "other", Name: "x", AppID: "cli_other" + suffix, AppSecret: "v1:x", Status: domain.FeishuTenantStatusActive}); err != nil || seeded {
		t.Fatalf("seed must skip non-empty table: seeded=%v err=%v", seeded, err)
	}

	test, err := tenants.CreateFeishuTenant(ctx, domain.FeishuTenant{ID: "test", Name: "测试企业", AppID: "cli_test" + suffix, AppSecret: "v1:secret", Status: domain.FeishuTenantStatusActive, SortOrder: 2})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if _, err := tenants.CreateFeishuTenant(ctx, domain.FeishuTenant{ID: "test2", Name: "重复", AppID: test.AppID, AppSecret: "v1:x", Status: domain.FeishuTenantStatusActive}); !errors.Is(err, domain.ErrFeishuTenantAppIDConflict) {
		t.Fatalf("expected app id conflict, got %v", err)
	}
	if _, err := tenants.CreateFeishuTenant(ctx, domain.FeishuTenant{ID: "test", Name: "重复", AppID: "cli_dup" + suffix, AppSecret: "v1:x", Status: domain.FeishuTenantStatusActive}); !errors.Is(err, domain.ErrFeishuTenantIDConflict) {
		t.Fatalf("expected id conflict, got %v", err)
	}

	// 空密钥更新沿用原值。
	renamed, err := tenants.UpdateFeishuTenant(ctx, domain.FeishuTenant{ID: "test", Name: "测试企业 2", Status: domain.FeishuTenantStatusActive, SortOrder: 2})
	if err != nil || renamed.Name != "测试企业 2" || renamed.AppSecret != "v1:secret" {
		t.Fatalf("rename tenant: %+v err=%v", renamed, err)
	}

	union := "on_tenant_" + suffix
	if _, err := users.UpsertFeishuUser(ctx, domain.FeishuUser{UnionID: union, OpenID: "ou_" + suffix, Name: "张三"}); err != nil {
		t.Fatalf("upsert feishu user: %v", err)
	}
	session := domain.FeishuAuthSession{SessionTokenHash: "hash-" + suffix, UnionID: union, TenantID: "test", AccessToken: "a", AccessTokenExpiresAt: time.Now().Add(time.Hour)}
	if err := users.CreateFeishuAuthSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	found, err := users.FindFeishuAuthSession(ctx, session.SessionTokenHash)
	if err != nil || found.TenantID != "test" || found.TenantName != "测试企业 2" || found.TenantStatus != domain.FeishuTenantStatusActive {
		t.Fatalf("find session with tenant: %+v err=%v", found, err)
	}

	if _, err := tenants.UpdateFeishuTenant(ctx, domain.FeishuTenant{ID: "test", Name: "测试企业 2", Status: domain.FeishuTenantStatusDisabled}); err != nil {
		t.Fatalf("disable tenant: %v", err)
	}
	if _, err := users.FindFeishuAuthSession(ctx, session.SessionTokenHash); !errors.Is(err, domain.ErrFeishuSessionNotFound) {
		t.Fatalf("expected sessions of disabled tenant deleted, got %v", err)
	}

	if _, err := tenants.UpdateFeishuTenant(ctx, domain.FeishuTenant{ID: "default", Name: "默认", Status: domain.FeishuTenantStatusDisabled}); !errors.Is(err, domain.ErrFeishuTenantLastActive) {
		t.Fatalf("expected last active tenant protected, got %v", err)
	}
	if _, err := tenants.UpdateFeishuTenant(ctx, domain.FeishuTenant{ID: "missing", Name: "x", Status: domain.FeishuTenantStatusActive}); !errors.Is(err, domain.ErrFeishuTenantNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	list, err := tenants.ListFeishuTenants(ctx)
	if err != nil || len(list) != 2 || list[0].ID != "default" || list[1].ID != "test" {
		t.Fatalf("list tenants by sort order: %+v err=%v", list, err)
	}
}
