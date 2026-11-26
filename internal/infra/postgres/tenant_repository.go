package postgres

import (
	"context"
	"errors"
	"fmt"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TenantRepository 读写租户和租户的飞书登录配置。app_secret 列存 secretbox 密文，加解密由 service 负责。
type TenantRepository struct {
	pool *pgxpool.Pool
}

func NewTenantRepository(pool *pgxpool.Pool) *TenantRepository {
	return &TenantRepository{pool: pool}
}

const tenantSelect = `
	select t.id, t.name, t.status, t.sort_order, coalesce(f.app_id, ''), coalesce(f.app_secret, ''), t.created_at, t.updated_at
	from tenants t
	left join tenant_feishu_apps f on f.tenant_id = t.id`

func (r *TenantRepository) ListTenants(ctx context.Context) ([]domain.Tenant, error) {
	rows, err := r.pool.Query(ctx, tenantSelect+` order by t.sort_order asc, t.id asc`)
	if err != nil {
		return nil, fmt.Errorf("query tenants: %w", err)
	}
	defer rows.Close()

	tenants := []domain.Tenant{}
	for rows.Next() {
		tenant, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		tenants = append(tenants, tenant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenants: %w", err)
	}
	return tenants, nil
}

func (r *TenantRepository) GetTenant(ctx context.Context, id string) (domain.Tenant, error) {
	return scanTenant(r.pool.QueryRow(ctx, tenantSelect+` where t.id = $1`, id))
}

func (r *TenantRepository) CreateTenant(ctx context.Context, tenant domain.Tenant) (domain.Tenant, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("begin create tenant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := insertTenant(ctx, tx, tenant); err != nil {
		return domain.Tenant{}, err
	}
	created, err := scanTenant(tx.QueryRow(ctx, tenantSelect+` where t.id = $1`, tenant.ID))
	if err != nil {
		return domain.Tenant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Tenant{}, fmt.Errorf("commit create tenant: %w", err)
	}
	return created, nil
}

// CreateFirstTenant 只在没有任何租户时写入，返回是否写入。用于平台首次初始化，并发请求只有一个能成功。
func (r *TenantRepository) CreateFirstTenant(ctx context.Context, tenant domain.Tenant) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin create first tenant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 表锁让并发的首次初始化串行执行，第二个请求会看到第一个写入的租户。
	if _, err := tx.Exec(ctx, `lock table tenants in exclusive mode`); err != nil {
		return false, fmt.Errorf("lock tenants: %w", err)
	}
	var exists bool
	if err := tx.QueryRow(ctx, `select exists (select 1 from tenants)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("check tenants: %w", err)
	}
	if exists {
		return false, nil
	}
	if err := insertTenant(ctx, tx, tenant); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit create first tenant: %w", err)
	}
	return true, nil
}

// UpdateTenant 更新名称、飞书密钥、状态和排序，飞书 App ID 不可修改。FeishuAppSecret 为空时沿用原值。
// 停用租户时同一事务内删除当前位于该租户的全部会话；停用最后一个启用中的租户返回 ErrTenantLastActive。
func (r *TenantRepository) UpdateTenant(ctx context.Context, tenant domain.Tenant) (domain.Tenant, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("begin update tenant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 锁住全部租户行，保证并发停用时"至少保留一个启用租户"的检查成立。
	var activeOthers int
	if err := tx.QueryRow(ctx, `
		select count(*) filter (where status = 'active' and id <> $1)
		from (select id, status from tenants for update) t
	`, tenant.ID).Scan(&activeOthers); err != nil {
		return domain.Tenant{}, fmt.Errorf("lock tenants: %w", err)
	}
	if tenant.Status == domain.TenantStatusDisabled && activeOthers == 0 {
		return domain.Tenant{}, domain.ErrTenantLastActive
	}

	tag, err := tx.Exec(ctx, `
		update tenants
		set name = $2, status = $3, sort_order = $4, updated_at = now()
		where id = $1
	`, tenant.ID, tenant.Name, tenant.Status, tenant.SortOrder)
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("update tenant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.Tenant{}, domain.ErrTenantNotFound
	}
	if tenant.FeishuAppSecret != "" {
		if _, err := tx.Exec(ctx, `
			update tenant_feishu_apps set app_secret = $2, updated_at = now() where tenant_id = $1
		`, tenant.ID, tenant.FeishuAppSecret); err != nil {
			return domain.Tenant{}, fmt.Errorf("update tenant feishu app: %w", err)
		}
	}
	if tenant.Status == domain.TenantStatusDisabled {
		if _, err := tx.Exec(ctx, `
			delete from auth_sessions s
			using users u
			where u.id = s.user_id and (s.active_tenant_id = $1 or u.tenant_id = $1)
		`, tenant.ID); err != nil {
			return domain.Tenant{}, fmt.Errorf("delete tenant sessions: %w", err)
		}
	}
	updated, err := scanTenant(tx.QueryRow(ctx, tenantSelect+` where t.id = $1`, tenant.ID))
	if err != nil {
		return domain.Tenant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Tenant{}, fmt.Errorf("commit update tenant: %w", err)
	}
	return updated, nil
}

func insertTenant(ctx context.Context, tx pgx.Tx, tenant domain.Tenant) error {
	if _, err := tx.Exec(ctx, `
		insert into tenants (id, name, status, sort_order) values ($1, $2, $3, $4)
	`, tenant.ID, tenant.Name, tenant.Status, tenant.SortOrder); err != nil {
		return tenantConflict(err)
	}
	if _, err := tx.Exec(ctx, `
		insert into tenant_feishu_apps (tenant_id, app_id, app_secret) values ($1, $2, $3)
	`, tenant.ID, tenant.FeishuAppID, tenant.FeishuAppSecret); err != nil {
		return tenantConflict(err)
	}
	return nil
}

func scanTenant(row pgx.Row) (domain.Tenant, error) {
	var tenant domain.Tenant
	if err := row.Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.Status,
		&tenant.SortOrder,
		&tenant.FeishuAppID,
		&tenant.FeishuAppSecret,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Tenant{}, domain.ErrTenantNotFound
		}
		return domain.Tenant{}, fmt.Errorf("scan tenant: %w", err)
	}
	return tenant, nil
}

func tenantConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if pgErr.ConstraintName == "tenant_feishu_apps_app_id_key" {
			return domain.ErrFeishuAppIDConflict
		}
		return domain.ErrTenantIDConflict
	}
	return fmt.Errorf("insert tenant: %w", err)
}

// tenantFilter 返回查询用的租户参数：租户范围返回租户 ID，系统范围返回空字符串。
// SQL 里统一写成 ($n::text = ” or x.tenant_id = $n)。
func tenantFilter(ctx context.Context) (string, error) {
	scope, err := domain.TenantScopeFromContext(ctx)
	if err != nil {
		return "", err
	}
	if scope.System {
		return "", nil
	}
	return scope.TenantID, nil
}
