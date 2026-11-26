package postgres

import (
	"context"
	"fmt"

	"forge-server/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuthorizationRepository struct {
	pool *pgxpool.Pool
}

func NewAuthorizationRepository(pool *pgxpool.Pool) *AuthorizationRepository {
	return &AuthorizationRepository{pool: pool}
}

func (r *AuthorizationRepository) ListUserRoles(ctx context.Context, userID string) ([]domain.Role, error) {
	return listUserRoles(ctx, r.pool, userID)
}

func (r *AuthorizationRepository) GetApplicationRole(ctx context.Context, userID string, applicationID string) (domain.ApplicationMemberRole, error) {
	var role domain.ApplicationMemberRole
	err := r.pool.QueryRow(ctx, `
		select role
		from application_members
		where user_id = $1 and application_id = $2
	`, userID, applicationID).Scan(&role)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("query application role: %w", err)
	}
	if !domain.IsValidApplicationMemberRole(role) {
		return "", nil
	}
	return role, nil
}

func (r *AuthorizationRepository) ListApplicationRoles(ctx context.Context, userID string) ([]domain.ApplicationMemberRole, error) {
	rows, err := r.pool.Query(ctx, `
		select distinct role
		from application_members
		where user_id = $1
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("query application roles: %w", err)
	}
	defer rows.Close()

	roles := make([]domain.ApplicationMemberRole, 0)
	for rows.Next() {
		var role domain.ApplicationMemberRole
		if err := rows.Scan(&role); err != nil {
			return nil, fmt.Errorf("scan application role: %w", err)
		}
		if domain.IsValidApplicationMemberRole(role) {
			roles = append(roles, role)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application roles: %w", err)
	}
	return roles, nil
}

func (r *AuthorizationRepository) ApplicationIDBySlug(ctx context.Context, slug string) (string, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return "", err
	}
	var id string
	if err := r.pool.QueryRow(ctx, `
		select id from applications where slug = $1 and ($2::text = '' or tenant_id = $2)
	`, slug, tenantID).Scan(&id); err != nil {
		if err == pgx.ErrNoRows {
			return "", domain.ErrApplicationNotFound
		}
		return "", fmt.Errorf("query application by slug: %w", err)
	}
	return id, nil
}

func (r *AuthorizationRepository) ApplicationIDByModuleID(ctx context.Context, moduleID string) (string, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return "", err
	}
	var id string
	if err := r.pool.QueryRow(ctx, `
		select application_id from modules where id = $1 and ($2::text = '' or tenant_id = $2)
	`, moduleID, tenantID).Scan(&id); err != nil {
		if err == pgx.ErrNoRows {
			return "", domain.ErrModuleNotFound
		}
		return "", fmt.Errorf("query module application: %w", err)
	}
	return id, nil
}

func (r *AuthorizationRepository) ApplicationIDByBuildID(ctx context.Context, buildID string) (string, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return "", err
	}
	var id string
	if err := r.pool.QueryRow(ctx, `
		select m.application_id
		from build_records b
		join modules m on m.id = b.module_id
		where b.id = $1 and ($2::text = '' or m.tenant_id = $2)
	`, buildID, tenantID).Scan(&id); err != nil {
		if err == pgx.ErrNoRows {
			return "", domain.ErrBuildRecordNotFound
		}
		return "", fmt.Errorf("query build application: %w", err)
	}
	return id, nil
}

func (r *AuthorizationRepository) ApplicationIDByReleaseID(ctx context.Context, releaseID string) (string, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return "", err
	}
	var id string
	if err := r.pool.QueryRow(ctx, `
		select m.application_id
		from release_records r
		join modules m on m.id = r.module_id
		where r.id = $1 and ($2::text = '' or m.tenant_id = $2)
	`, releaseID, tenantID).Scan(&id); err != nil {
		if err == pgx.ErrNoRows {
			return "", domain.ErrReleaseNotFound
		}
		return "", fmt.Errorf("query release application: %w", err)
	}
	return id, nil
}

func (r *AuthorizationRepository) ApplicationIDByTestPlanID(ctx context.Context, testPlanID string) (string, error) {
	if _, err := uuid.Parse(testPlanID); err != nil {
		return "", domain.ErrTestPlanNotFound
	}
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return "", err
	}
	var id string
	if err := r.pool.QueryRow(ctx, `
		select p.application_id
		from test_plans p
		join applications a on a.id = p.application_id
		where p.id = $1 and ($2::text = '' or a.tenant_id = $2)
	`, testPlanID, tenantID).Scan(&id); err != nil {
		if err == pgx.ErrNoRows {
			return "", domain.ErrTestPlanNotFound
		}
		return "", fmt.Errorf("query test plan application: %w", err)
	}
	return id, nil
}

func (r *AuthorizationRepository) ListApplicationMembers(ctx context.Context, applicationID string) ([]domain.ApplicationMember, error) {
	return r.listApplicationMembers(ctx, r.pool, applicationID)
}

func (r *AuthorizationRepository) UpsertApplicationMember(ctx context.Context, applicationID string, input domain.ApplicationMemberInput) (domain.ApplicationMember, error) {
	// 成员只能是应用所属租户的用户。
	tag, err := r.pool.Exec(ctx, `
		insert into application_members (
			application_id,
			user_id,
			role,
			sort_order
		)
		select
			a.id,
			u.id,
			$3,
			coalesce((select max(sort_order) + 10 from application_members where application_id = a.id), 10)
		from applications a
		join users u on u.id = $2 and u.tenant_id = a.tenant_id
		where a.id = $1
		on conflict (application_id, user_id) do update set
			role = excluded.role,
			updated_at = now()
	`, applicationID, input.UserID, input.Role)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.ApplicationMember{}, domain.ErrUserTenantMismatch
	}
	if err != nil {
		if isForeignKeyViolation(err) {
			return domain.ApplicationMember{}, domain.ErrInvalidApplicationInput
		}
		if isCheckViolation(err) {
			return domain.ApplicationMember{}, domain.ErrInvalidApplicationInput
		}
		return domain.ApplicationMember{}, fmt.Errorf("upsert application member: %w", err)
	}
	return r.getApplicationMember(ctx, r.pool, applicationID, input.UserID)
}

func (r *AuthorizationRepository) DeleteApplicationMember(ctx context.Context, applicationID string, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete application member: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	var role domain.ApplicationMemberRole
	if err := tx.QueryRow(ctx, `
		select role
		from application_members
		where application_id = $1 and user_id = $2
	`, applicationID, userID).Scan(&role); err != nil {
		if err == pgx.ErrNoRows {
			return domain.ErrApplicationNotFound
		}
		return fmt.Errorf("query application member before delete: %w", err)
	}

	if role == domain.ApplicationMemberRoleOwner {
		var ownerCount int
		if err := tx.QueryRow(ctx, `
			select count(*)
			from application_members
			where application_id = $1 and role = $2
		`, applicationID, domain.ApplicationMemberRoleOwner).Scan(&ownerCount); err != nil {
			return fmt.Errorf("count application owners: %w", err)
		}
		if ownerCount <= 1 {
			return domain.ErrInvalidApplicationInput
		}
	}

	tag, err := tx.Exec(ctx, `
		delete from application_members
		where application_id = $1 and user_id = $2
	`, applicationID, userID)
	if err != nil {
		return fmt.Errorf("delete application member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrApplicationNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete application member: %w", err)
	}
	return nil
}

func (r *AuthorizationRepository) getApplicationMember(ctx context.Context, querier applicationQuerier, applicationID string, userID string) (domain.ApplicationMember, error) {
	var member domain.ApplicationMember
	if err := querier.QueryRow(ctx, `
		select
			am.id::text,
			am.user_id,
			u.name,
			u.email,
			u.avatar_url,
			am.role
		from application_members am
		join users u on u.id = am.user_id
		where am.application_id = $1 and am.user_id = $2
	`, applicationID, userID).Scan(
		&member.ID,
		&member.UserID,
		&member.Name,
		&member.Email,
		&member.AvatarURL,
		&member.Role,
	); err != nil {
		if err == pgx.ErrNoRows {
			return domain.ApplicationMember{}, domain.ErrApplicationNotFound
		}
		return domain.ApplicationMember{}, fmt.Errorf("query application member: %w", err)
	}
	return member, nil
}

func (r *AuthorizationRepository) listApplicationMembers(ctx context.Context, querier applicationQuerier, applicationID string) ([]domain.ApplicationMember, error) {
	rows, err := querier.Query(ctx, `
		select
			am.id::text,
			am.user_id,
			u.name,
			u.email,
			u.avatar_url,
			am.role
		from application_members am
		join users u on u.id = am.user_id
		where am.application_id = $1
		order by am.sort_order asc, am.created_at asc
	`, applicationID)
	if err != nil {
		return nil, fmt.Errorf("query application members: %w", err)
	}
	defer rows.Close()

	members := make([]domain.ApplicationMember, 0)
	for rows.Next() {
		var member domain.ApplicationMember
		if err := rows.Scan(
			&member.ID,
			&member.UserID,
			&member.Name,
			&member.Email,
			&member.AvatarURL,
			&member.Role,
		); err != nil {
			return nil, fmt.Errorf("scan application member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application members: %w", err)
	}
	return members, nil
}
