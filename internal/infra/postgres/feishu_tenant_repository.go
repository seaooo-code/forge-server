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

// FeishuTenantRepository 读写飞书登录租户。app_secret 列存 secretbox 密文，加解密由 service 负责。
type FeishuTenantRepository struct {
	pool *pgxpool.Pool
}

func NewFeishuTenantRepository(pool *pgxpool.Pool) *FeishuTenantRepository {
	return &FeishuTenantRepository{pool: pool}
}

const feishuTenantColumns = `id, name, app_id, app_secret, status, sort_order, created_at, updated_at`

func (r *FeishuTenantRepository) ListFeishuTenants(ctx context.Context) ([]domain.FeishuTenant, error) {
	rows, err := r.pool.Query(ctx, `select `+feishuTenantColumns+` from feishu_tenants order by sort_order asc, id asc`)
	if err != nil {
		return nil, fmt.Errorf("query feishu tenants: %w", err)
	}
	defer rows.Close()

	tenants := []domain.FeishuTenant{}
	for rows.Next() {
		tenant, err := scanFeishuTenant(rows)
		if err != nil {
			return nil, err
		}
		tenants = append(tenants, tenant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feishu tenants: %w", err)
	}
	return tenants, nil
}

func (r *FeishuTenantRepository) GetFeishuTenant(ctx context.Context, id string) (domain.FeishuTenant, error) {
	return scanFeishuTenant(r.pool.QueryRow(ctx, `select `+feishuTenantColumns+` from feishu_tenants where id = $1`, id))
}

func (r *FeishuTenantRepository) CreateFeishuTenant(ctx context.Context, tenant domain.FeishuTenant) (domain.FeishuTenant, error) {
	created, err := scanFeishuTenant(r.pool.QueryRow(ctx, `
		insert into feishu_tenants (id, name, app_id, app_secret, status, sort_order)
		values ($1, $2, $3, $4, $5, $6)
		returning `+feishuTenantColumns,
		tenant.ID, tenant.Name, tenant.AppID, tenant.AppSecret, tenant.Status, tenant.SortOrder))
	if err != nil {
		return domain.FeishuTenant{}, feishuTenantConflict(err)
	}
	return created, nil
}

// CreateFirstFeishuTenant 只在表为空时写入租户，返回是否写入。用于平台首次初始化，并发请求只有一个能成功。
func (r *FeishuTenantRepository) CreateFirstFeishuTenant(ctx context.Context, tenant domain.FeishuTenant) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		insert into feishu_tenants (id, name, app_id, app_secret, status, sort_order)
		select $1, $2, $3, $4, $5, $6
		where not exists (select 1 from feishu_tenants)
		on conflict do nothing
	`, tenant.ID, tenant.Name, tenant.AppID, tenant.AppSecret, tenant.Status, tenant.SortOrder)
	if err != nil {
		return false, fmt.Errorf("create first feishu tenant: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// UpdateFeishuTenant 更新名称、密钥、状态和排序，app_id 不可修改。tenant.AppSecret 为空时沿用原值。
// 停用租户时同一事务内删除该租户的全部会话；停用最后一个启用中的租户返回 ErrFeishuTenantLastActive。
func (r *FeishuTenantRepository) UpdateFeishuTenant(ctx context.Context, tenant domain.FeishuTenant) (domain.FeishuTenant, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.FeishuTenant{}, fmt.Errorf("begin update feishu tenant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 锁住全部租户行，保证并发停用时"至少保留一个启用租户"的检查成立。
	var activeOthers int
	if err := tx.QueryRow(ctx, `
		select count(*) filter (where status = 'active' and id <> $1)
		from (select id, status from feishu_tenants for update) t
	`, tenant.ID).Scan(&activeOthers); err != nil {
		return domain.FeishuTenant{}, fmt.Errorf("lock feishu tenants: %w", err)
	}
	if tenant.Status == domain.FeishuTenantStatusDisabled && activeOthers == 0 {
		return domain.FeishuTenant{}, domain.ErrFeishuTenantLastActive
	}

	updated, err := scanFeishuTenant(tx.QueryRow(ctx, `
		update feishu_tenants
		set
			name = $2,
			app_secret = coalesce(nullif($3, ''), app_secret),
			status = $4,
			sort_order = $5,
			updated_at = now()
		where id = $1
		returning `+feishuTenantColumns,
		tenant.ID, tenant.Name, tenant.AppSecret, tenant.Status, tenant.SortOrder))
	if err != nil {
		return domain.FeishuTenant{}, err
	}
	if updated.Status == domain.FeishuTenantStatusDisabled {
		if _, err := tx.Exec(ctx, `delete from feishu_auth_sessions where feishu_tenant_id = $1`, tenant.ID); err != nil {
			return domain.FeishuTenant{}, fmt.Errorf("delete feishu tenant sessions: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FeishuTenant{}, fmt.Errorf("commit update feishu tenant: %w", err)
	}
	return updated, nil
}

func scanFeishuTenant(row pgx.Row) (domain.FeishuTenant, error) {
	var tenant domain.FeishuTenant
	if err := row.Scan(&tenant.ID, &tenant.Name, &tenant.AppID, &tenant.AppSecret, &tenant.Status, &tenant.SortOrder, &tenant.CreatedAt, &tenant.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.FeishuTenant{}, domain.ErrFeishuTenantNotFound
		}
		return domain.FeishuTenant{}, fmt.Errorf("scan feishu tenant: %w", err)
	}
	return tenant, nil
}

func feishuTenantConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if pgErr.ConstraintName == "feishu_tenants_app_id_key" {
			return domain.ErrFeishuTenantAppIDConflict
		}
		return domain.ErrFeishuTenantIDConflict
	}
	return err
}
