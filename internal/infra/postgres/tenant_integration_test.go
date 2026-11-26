package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 租户、用户身份和会话：租户带飞书应用；同一个人按身份只会建一个用户；停用租户删除它的会话；
// 不能停用最后一个启用租户；飞书 App ID 唯一。
func TestIntegrationTenantLifecycle(t *testing.T) {
	pool := newIntegrationPool(t)
	ctx := context.Background()
	tenants := NewTenantRepository(pool)
	users := NewUserRepository(pool)

	// 测试库独占：清空租户及其下属数据，保证"最后一个启用租户"的判断只看本测试的数据。
	if _, err := pool.Exec(ctx, `truncate tenants cascade`); err != nil {
		t.Fatalf("reset tenants: %v", err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)

	seeded, err := tenants.CreateFirstTenant(ctx, domain.Tenant{ID: "default", Name: "默认", FeishuAppID: "cli_seed" + suffix, FeishuAppSecret: "v1:x", Status: domain.TenantStatusActive})
	if err != nil || !seeded {
		t.Fatalf("seed into empty table: seeded=%v err=%v", seeded, err)
	}
	if seeded, err := tenants.CreateFirstTenant(ctx, domain.Tenant{ID: "other", Name: "x", FeishuAppID: "cli_other" + suffix, FeishuAppSecret: "v1:x", Status: domain.TenantStatusActive}); err != nil || seeded {
		t.Fatalf("seed must skip non-empty table: seeded=%v err=%v", seeded, err)
	}

	test, err := tenants.CreateTenant(ctx, domain.Tenant{ID: "test", Name: "测试企业", FeishuAppID: "cli_test" + suffix, FeishuAppSecret: "v1:secret", Status: domain.TenantStatusActive, SortOrder: 2})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if test.FeishuAppID != "cli_test"+suffix || test.FeishuAppSecret != "v1:secret" {
		t.Fatalf("expected feishu app of tenant, got %+v", test)
	}
	if _, err := tenants.CreateTenant(ctx, domain.Tenant{ID: "test2", Name: "重复", FeishuAppID: test.FeishuAppID, FeishuAppSecret: "v1:x", Status: domain.TenantStatusActive}); !errors.Is(err, domain.ErrFeishuAppIDConflict) {
		t.Fatalf("expected app id conflict, got %v", err)
	}
	if _, err := tenants.CreateTenant(ctx, domain.Tenant{ID: "test", Name: "重复", FeishuAppID: "cli_dup" + suffix, FeishuAppSecret: "v1:x", Status: domain.TenantStatusActive}); !errors.Is(err, domain.ErrTenantIDConflict) {
		t.Fatalf("expected id conflict, got %v", err)
	}

	// 空密钥更新沿用原值。
	renamed, err := tenants.UpdateTenant(ctx, domain.Tenant{ID: "test", Name: "测试企业 2", Status: domain.TenantStatusActive, SortOrder: 2})
	if err != nil || renamed.Name != "测试企业 2" || renamed.FeishuAppSecret != "v1:secret" {
		t.Fatalf("rename tenant: %+v err=%v", renamed, err)
	}

	// 同一个飞书身份重复解析只建一个用户，登录时记录登录时间。
	external := domain.ExternalUser{
		Provider: domain.IdentityProviderFeishu,
		Issuer:   test.FeishuAppID,
		Subject:  "ou_" + suffix,
		Name:     "张三",
		Email:    "zhangsan@example.com",
		Profile:  map[string]string{"unionId": "on_" + suffix},
	}
	user, identity, err := users.ResolveExternalUser(ctx, "test", external, true)
	if err != nil {
		t.Fatalf("resolve user: %v", err)
	}
	if user.ID[:5] != "user-" || user.TenantID != "test" || identity.Issuer != test.FeishuAppID {
		t.Fatalf("expected generated user in tenant, got %+v %+v", user, identity)
	}
	again, againIdentity, err := users.ResolveExternalUser(ctx, "test", external, false)
	if err != nil || again.ID != user.ID || againIdentity.ID != identity.ID {
		t.Fatalf("expected same user for same identity, got %+v %+v err=%v", again, againIdentity, err)
	}
	if _, _, err := users.ResolveExternalUser(ctx, "default", external, false); !errors.Is(err, domain.ErrUserTenantMismatch) {
		t.Fatalf("expected identity of another tenant rejected, got %v", err)
	}

	// 首个登录用户补 SuperAdmin 和本租户 TenantAdmin，重复调用不重复授予。
	if err := users.BootstrapUserRoles(ctx, user); err != nil {
		t.Fatalf("bootstrap roles: %v", err)
	}
	if err := users.BootstrapUserRoles(ctx, user); err != nil {
		t.Fatalf("bootstrap roles twice: %v", err)
	}
	roles, err := users.ListUserRoles(ctx, user.ID)
	if err != nil || len(roles) != 2 {
		t.Fatalf("expected super admin and tenant admin, got %v err=%v", roles, err)
	}

	now := time.Now()
	session := domain.AuthSession{
		TokenHash:                    "hash-" + suffix,
		UserID:                       user.ID,
		IdentityID:                   identity.ID,
		ActiveTenantID:               "test",
		ProviderAccessToken:          "a",
		ProviderAccessTokenExpiresAt: now.Add(time.Hour),
	}
	if err := users.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	found, err := users.FindSession(ctx, session.TokenHash)
	if err != nil || found.ActiveTenant.ID != "test" || found.ActiveTenant.Name != "测试企业 2" ||
		found.HomeTenant.ID != "test" || found.ActiveTenantStatus != domain.TenantStatusActive || found.User.ID != user.ID {
		t.Fatalf("find session with tenant: %+v err=%v", found, err)
	}

	// 切换租户只能切到启用中的租户。
	if err := users.SetSessionActiveTenant(ctx, session.TokenHash, "default"); err != nil {
		t.Fatalf("switch session tenant: %v", err)
	}
	if err := users.SetSessionActiveTenant(ctx, session.TokenHash, "missing"); !errors.Is(err, domain.ErrTenantNotFound) {
		t.Fatalf("expected missing tenant rejected, got %v", err)
	}

	if _, err := tenants.UpdateTenant(ctx, domain.Tenant{ID: "test", Name: "测试企业 2", Status: domain.TenantStatusDisabled}); err != nil {
		t.Fatalf("disable tenant: %v", err)
	}
	if _, err := users.FindSession(ctx, session.TokenHash); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("expected sessions of disabled tenant deleted, got %v", err)
	}

	if _, err := tenants.UpdateTenant(ctx, domain.Tenant{ID: "default", Name: "默认", Status: domain.TenantStatusDisabled}); !errors.Is(err, domain.ErrTenantLastActive) {
		t.Fatalf("expected last active tenant protected, got %v", err)
	}
	if _, err := tenants.UpdateTenant(ctx, domain.Tenant{ID: "missing", Name: "x", Status: domain.TenantStatusActive}); !errors.Is(err, domain.ErrTenantNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	list, err := tenants.ListTenants(ctx)
	if err != nil || len(list) != 2 || list[0].ID != "default" || list[1].ID != "test" {
		t.Fatalf("list tenants by sort order: %+v err=%v", list, err)
	}
}

// 租户范围隔离：环境编码在租户内重复，按编码查询只命中当前租户；系统范围不过滤。
func TestIntegrationTenantScopedQueries(t *testing.T) {
	pool := newIntegrationPool(t)
	ctx := context.Background()
	environments := NewEnvironmentRepository(pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	for _, tenantID := range []string{"tnt-a-" + suffix, "tnt-b-" + suffix} {
		if _, err := pool.Exec(ctx, `insert into tenants (id, name, status, sort_order) values ($1, 't', 'active', 1)`, tenantID); err != nil {
			t.Fatalf("seed tenant: %v", err)
		}
		userID := "user-" + tenantID
		if _, err := pool.Exec(ctx, `insert into users (id, tenant_id, name) values ($1, $2, 'owner')`, userID, tenantID); err != nil {
			t.Fatalf("seed user: %v", err)
		}
		storageID := "storage-" + tenantID
		if _, err := pool.Exec(ctx, `
			insert into storage_configurations (id, tenant_id, name, adapter, bucket, endpoint, region, owner_user_id, access_key_id, secret_access_key, status, sort_order)
			values ($1, $2, 's', 'S3', 'b', 'http://s3.example.com', 'r', $3, 'k', 's', 'healthy', 1)
		`, storageID, tenantID, userID); err != nil {
			t.Fatalf("seed storage: %v", err)
		}
		if _, err := environments.CreateEnvironment(domain.WithTenant(ctx, tenantID), domain.EnvironmentInput{
			Code: "prod", Name: "生产", Status: domain.EnvironmentStatusHealthy, Tone: "blue", StorageConfigurationID: storageID,
		}); err != nil {
			t.Fatalf("create environment: %v", err)
		}
	}

	listA, err := environments.ListEnvironments(domain.WithTenant(ctx, "tnt-a-"+suffix))
	if err != nil || len(listA) != 1 || listA[0].Code != "prod" {
		t.Fatalf("expected only tenant A environment, got %+v err=%v", listA, err)
	}
	if _, err := environments.GetEnvironmentDetail(domain.WithTenant(ctx, "tnt-a-"+suffix), "prod"); err != nil {
		t.Fatalf("get tenant A environment: %v", err)
	}
	// 系统范围不过滤，两个租户的同名环境都看得到。
	all, err := environments.ListEnvironments(domain.WithSystemScope(ctx))
	if err != nil || len(all) != 2 {
		t.Fatalf("expected both environments in system scope, got %d err=%v", len(all), err)
	}
	// 没有范围时拒绝查询，不静默返回全部数据。
	if _, err := environments.ListEnvironments(ctx); !errors.Is(err, domain.ErrTenantScopeMissing) {
		t.Fatalf("expected missing scope rejected, got %v", err)
	}
	// 跨租户引用存储配置被组合外键拒绝。
	if _, err := environments.CreateEnvironment(domain.WithTenant(ctx, "tnt-a-"+suffix), domain.EnvironmentInput{
		Code: "dev", Name: "开发", Status: domain.EnvironmentStatusHealthy, Tone: "blue", StorageConfigurationID: "storage-tnt-b-" + suffix,
	}); !errors.Is(err, domain.ErrStorageConfigurationNotFound) {
		t.Fatalf("expected cross tenant storage rejected, got %v", err)
	}
}

// integrationCtx 让仓储测试以系统范围执行：种子数据覆盖所有租户，和后台任务、worker 的调用方式一致。
func integrationCtx() context.Context {
	return domain.WithSystemScope(context.Background())
}

// moduleTenantCtx 返回模块所属租户的上下文。新增数据的方法要求具体租户，不能只给系统范围。
func moduleTenantCtx(t *testing.T, pool *pgxpool.Pool, moduleID string) context.Context {
	t.Helper()
	var tenantID string
	if err := pool.QueryRow(integrationCtx(), `select tenant_id from modules where id = $1`, moduleID).Scan(&tenantID); err != nil {
		t.Fatalf("query module tenant: %v", err)
	}
	return domain.WithTenant(context.Background(), tenantID)
}

// newIntegrationTenant 建一个测试租户和一个属于它的用户，返回租户 ID 和用户 ID。
func newIntegrationTenant(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantID := "tnt-" + suffix
	userID := "user-" + suffix
	if _, err := pool.Exec(integrationCtx(), `insert into tenants (id, name, status, sort_order) values ($1, 't', 'active', 1)`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := pool.Exec(integrationCtx(), `insert into users (id, tenant_id, name) values ($1, $2, 'tester')`, userID, tenantID); err != nil {
		t.Fatalf("seed tenant user: %v", err)
	}
	return tenantID, userID
}
