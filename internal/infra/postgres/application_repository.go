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

type ApplicationRepository struct {
	pool *pgxpool.Pool
}

func NewApplicationRepository(pool *pgxpool.Pool) *ApplicationRepository {
	return &ApplicationRepository{pool: pool}
}

func (r *ApplicationRepository) ListApplications(ctx context.Context, filters domain.ApplicationListFilters) ([]domain.Application, domain.ApplicationSummary, error) {
	summary, err := r.applicationSummary(ctx, filters)
	if err != nil {
		return nil, domain.ApplicationSummary{}, err
	}

	rows, err := r.pool.Query(ctx, `
		select
			a.id,
			a.name,
			a.slug,
			a.platform,
			a.owner_user_id,
			u.name as owner,
			u.email as owner_email,
			u.avatar_url as owner_avatar_url,
			u.department_ids as owner_department_ids,
			a.module_count,
			count(b.environment_code)::integer as environment_count,
			a.created_at,
			a.status
		from applications a
		join users u on u.id = a.owner_user_id
		left join application_environment_bindings b on b.application_id = a.id
		where
			($1 = '' or exists (
				select 1
				from unnest(string_to_array(a.platform, '/')) as platform_part(value)
				where btrim(platform_part.value) = $1
			))
			and ($2 = '' or a.status = $2)
				and ($3 = '' or a.owner_user_id = $3)
			and ($4 = '' or a.name ilike '%' || $4 || '%')
		group by
			a.id,
			a.name,
			a.slug,
			a.platform,
			a.owner_user_id,
			u.name,
			u.email,
			u.avatar_url,
			u.department_ids,
			a.module_count,
			a.created_at,
			a.status,
			a.sort_order
			order by a.sort_order asc, a.created_at asc
			limit nullif($5, 0) offset $6
		`, filters.Platform, string(filters.Status), filters.Owner, filters.Keyword, filters.Pagination.Limit(), filters.Pagination.Offset())
	if err != nil {
		return nil, domain.ApplicationSummary{}, fmt.Errorf("query applications: %w", err)
	}
	defer rows.Close()

	applications := make([]domain.Application, 0)
	for rows.Next() {
		var application domain.Application
		if err := rows.Scan(
			&application.ID,
			&application.Name,
			&application.Slug,
			&application.Platform,
			&application.OwnerUserID,
			&application.Owner,
			&application.OwnerEmail,
			&application.OwnerAvatarURL,
			&application.OwnerDepartmentIDs,
			&application.ModuleCount,
			&application.EnvironmentCount,
			&application.CreatedAt,
			&application.Status,
		); err != nil {
			return nil, domain.ApplicationSummary{}, fmt.Errorf("scan application: %w", err)
		}
		applications = append(applications, application)
	}

	if err := rows.Err(); err != nil {
		return nil, domain.ApplicationSummary{}, fmt.Errorf("iterate applications: %w", err)
	}

	return applications, summary, nil
}

func (r *ApplicationRepository) applicationSummary(ctx context.Context, filters domain.ApplicationListFilters) (domain.ApplicationSummary, error) {
	var summary domain.ApplicationSummary
	if err := r.pool.QueryRow(ctx, `
		select
			count(*)::integer,
			count(*) filter (where a.status = 'running')::integer,
			count(*) filter (where a.status = 'maintenance')::integer,
			count(*) filter (where a.status = 'offline')::integer
		from applications a
		where
			($1 = '' or exists (
				select 1
				from unnest(string_to_array(a.platform, '/')) as platform_part(value)
				where btrim(platform_part.value) = $1
			))
			and ($2 = '' or a.status = $2)
			and ($3 = '' or a.owner_user_id = $3)
			and ($4 = '' or a.name ilike '%' || $4 || '%')
	`, filters.Platform, string(filters.Status), filters.Owner, filters.Keyword).Scan(
		&summary.Total,
		&summary.Running,
		&summary.Maintenance,
		&summary.Offline,
	); err != nil {
		return domain.ApplicationSummary{}, fmt.Errorf("summarize applications: %w", err)
	}
	return summary, nil
}

func (r *ApplicationRepository) GetApplicationDetail(ctx context.Context, slug string) (domain.ApplicationDetail, error) {
	return r.getApplicationDetail(ctx, r.pool, slug)
}

func (r *ApplicationRepository) CreateApplication(ctx context.Context, input domain.ApplicationInput) (domain.ApplicationDetail, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ApplicationDetail{}, fmt.Errorf("begin create application: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	if err := upsertUser(ctx, tx, applicationOwnerUser(input)); err != nil {
		return domain.ApplicationDetail{}, err
	}

	applicationID := "app-" + input.Slug
	if _, err := tx.Exec(ctx, `
		insert into applications (
			id,
			slug,
			name,
			status,
			platform,
			owner_user_id,
			description,
			module_count,
			sort_order
		) values (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			0,
			coalesce((select max(sort_order) + 10 from applications), 10)
		)
	`, applicationID,
		input.Slug,
		input.Name,
		input.Status,
		input.Platform,
		input.OwnerUserID,
		input.Description,
	); err != nil {
		if isUniqueViolation(err) {
			return domain.ApplicationDetail{}, domain.ErrApplicationSlugConflict
		}
		return domain.ApplicationDetail{}, fmt.Errorf("insert application: %w", err)
	}

	if err := r.replaceApplicationRelations(ctx, tx, applicationID, input); err != nil {
		return domain.ApplicationDetail{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ApplicationDetail{}, fmt.Errorf("commit create application: %w", err)
	}

	return r.GetApplicationDetail(ctx, input.Slug)
}

func (r *ApplicationRepository) UpdateApplication(ctx context.Context, slug string, input domain.ApplicationInput) (domain.ApplicationDetail, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ApplicationDetail{}, fmt.Errorf("begin update application: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	if err := upsertUser(ctx, tx, applicationOwnerUser(input)); err != nil {
		return domain.ApplicationDetail{}, err
	}

	var applicationID string
	if err := tx.QueryRow(ctx, `
		update applications set
			name = $2,
			status = $3,
			platform = $4,
			owner_user_id = $5,
			description = $6,
			updated_at = now()
		where slug = $1
		returning id
	`,
		slug,
		input.Name,
		input.Status,
		input.Platform,
		input.OwnerUserID,
		input.Description,
	).Scan(&applicationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ApplicationDetail{}, domain.ErrApplicationNotFound
		}
		return domain.ApplicationDetail{}, fmt.Errorf("update application: %w", err)
	}

	if err := r.replaceApplicationRelations(ctx, tx, applicationID, input); err != nil {
		return domain.ApplicationDetail{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ApplicationDetail{}, fmt.Errorf("commit update application: %w", err)
	}

	return r.GetApplicationDetail(ctx, slug)
}

func (r *ApplicationRepository) UpdateApplicationStatus(ctx context.Context, slug string, status domain.ApplicationStatus) (domain.ApplicationDetail, error) {
	commandTag, err := r.pool.Exec(ctx, `
		update applications
		set status = $2, updated_at = now()
		where slug = $1
	`, slug, status)
	if err != nil {
		return domain.ApplicationDetail{}, fmt.Errorf("update application status: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return domain.ApplicationDetail{}, domain.ErrApplicationNotFound
	}

	return r.GetApplicationDetail(ctx, slug)
}

func (r *ApplicationRepository) CheckApplicationDelete(ctx context.Context, slug string) (domain.ApplicationDeleteCheck, error) {
	var applicationID string
	var moduleCount int
	if err := r.pool.QueryRow(ctx, `
		select id, module_count
		from applications
		where slug = $1
	`, slug).Scan(&applicationID, &moduleCount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ApplicationDeleteCheck{}, domain.ErrApplicationNotFound
		}
		return domain.ApplicationDeleteCheck{}, fmt.Errorf("query application delete dependencies: %w", err)
	}

	check := domain.ApplicationDeleteCheck{
		Deletable:    true,
		Dependencies: []domain.ApplicationDeleteDependency{},
		Message:      "应用没有模块依赖，可以删除",
	}
	if moduleCount > 0 {
		check.Deletable = false
		check.Message = "应用仍有关联模块，不能删除"
		check.Dependencies = append(check.Dependencies, domain.ApplicationDeleteDependency{
			ID:          applicationID + ":modules",
			Name:        "接入模块",
			Type:        "module",
			Description: fmt.Sprintf("当前应用仍有 %d 个接入模块", moduleCount),
		})
	}
	return check, nil
}

func (r *ApplicationRepository) DeleteApplication(ctx context.Context, slug string) error {
	check, err := r.CheckApplicationDelete(ctx, slug)
	if err != nil {
		return err
	}
	if !check.Deletable {
		return domain.ErrApplicationInUse
	}

	commandTag, err := r.pool.Exec(ctx, `
		delete from applications
		where slug = $1
	`, slug)
	if err != nil {
		if isForeignKeyViolation(err) {
			return domain.ErrApplicationInUse
		}
		return fmt.Errorf("delete application: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return domain.ErrApplicationNotFound
	}
	return nil
}

type applicationQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (r *ApplicationRepository) getApplicationDetail(ctx context.Context, querier applicationQuerier, slug string) (domain.ApplicationDetail, error) {
	var detail domain.ApplicationDetail
	if err := querier.QueryRow(ctx, `
		select
			a.id,
			a.name,
			a.slug,
			a.platform,
			a.owner_user_id,
			u.name as owner,
			u.email as owner_email,
			u.avatar_url as owner_avatar_url,
			u.department_ids as owner_department_ids,
			a.module_count,
			(
				select count(*)::integer
				from application_environment_bindings b
				where b.application_id = a.id
			) as environment_count,
			a.created_at,
			a.status,
			a.description
		from applications a
		join users u on u.id = a.owner_user_id
		where a.slug = $1
	`, slug).Scan(
		&detail.Application.ID,
		&detail.Application.Name,
		&detail.Application.Slug,
		&detail.Application.Platform,
		&detail.Application.OwnerUserID,
		&detail.Application.Owner,
		&detail.Application.OwnerEmail,
		&detail.Application.OwnerAvatarURL,
		&detail.Application.OwnerDepartmentIDs,
		&detail.Application.ModuleCount,
		&detail.Application.EnvironmentCount,
		&detail.Application.CreatedAt,
		&detail.Application.Status,
		&detail.Description,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ApplicationDetail{}, domain.ErrApplicationNotFound
		}
		return domain.ApplicationDetail{}, fmt.Errorf("query application detail: %w", err)
	}

	environments, err := r.listApplicationEnvironments(ctx, querier, detail.Application.ID)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}
	detail.Environments = environments

	members, err := r.listApplicationMembers(ctx, querier, detail.Application.ID)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}
	detail.Members = members

	modules, err := r.listApplicationModules(ctx, querier, detail.Application.ID)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}
	detail.Modules = modules

	detail.ID = detail.Application.ID
	detail.AppKey = "rn." + detail.Application.Slug
	detail.Slug = detail.Application.Slug
	detail.Name = detail.Application.Name
	detail.OwnerDepartmentIDs = detail.Application.OwnerDepartmentIDs

	return detail, nil
}

func (r *ApplicationRepository) ListApplicationOwners(ctx context.Context) ([]domain.ApplicationOptionItem, error) {
	rows, err := r.pool.Query(ctx, `
		select u.id, u.name
		from applications a
		join users u on u.id = a.owner_user_id
		where u.name <> ''
		group by u.id, u.name
		order by min(a.sort_order) asc, u.name asc
	`)
	if err != nil {
		return nil, fmt.Errorf("query application owners: %w", err)
	}
	defer rows.Close()

	owners := make([]domain.ApplicationOptionItem, 0)
	for rows.Next() {
		var ownerID string
		var ownerName string
		if err := rows.Scan(&ownerID, &ownerName); err != nil {
			return nil, fmt.Errorf("scan application owner: %w", err)
		}
		owners = append(owners, domain.ApplicationOptionItem{
			Label: ownerName,
			Value: ownerID,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application owners: %w", err)
	}

	return owners, nil
}

func (r *ApplicationRepository) ListApplicationEnvironments(ctx context.Context) ([]domain.ApplicationOptionItem, error) {
	rows, err := r.pool.Query(ctx, `
		select code, name
		from environments
		order by sort_order asc, code asc
	`)
	if err != nil {
		return nil, fmt.Errorf("query application environments: %w", err)
	}
	defer rows.Close()

	environments := make([]domain.ApplicationOptionItem, 0)
	for rows.Next() {
		var code string
		var name string
		if err := rows.Scan(&code, &name); err != nil {
			return nil, fmt.Errorf("scan application environment option: %w", err)
		}
		environments = append(environments, domain.ApplicationOptionItem{
			Label: code,
			Value: code,
		})
		if name != "" && name != code {
			environments[len(environments)-1].Label = name
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application environment options: %w", err)
	}

	return environments, nil
}

func (r *ApplicationRepository) ListApplicationMembers(ctx context.Context, applicationID string) ([]domain.ApplicationMember, error) {
	return r.listApplicationMembers(ctx, r.pool, applicationID)
}

func (r *ApplicationRepository) UpsertApplicationMember(ctx context.Context, applicationID string, input domain.ApplicationMemberInput) (domain.ApplicationMember, error) {
	if _, err := r.pool.Exec(ctx, `
		insert into application_members (
			application_id,
			user_id,
			role,
			sort_order
		) values (
			$1,
			$2,
			$3,
			coalesce((select max(sort_order) + 10 from application_members where application_id = $1), 10)
		)
		on conflict (application_id, user_id) do update set
			role = excluded.role,
			updated_at = now()
	`, applicationID, input.UserID, input.Role); err != nil {
		if isForeignKeyViolation(err) || isCheckViolation(err) {
			return domain.ApplicationMember{}, domain.ErrInvalidApplicationInput
		}
		return domain.ApplicationMember{}, fmt.Errorf("upsert application member: %w", err)
	}
	return r.getApplicationMember(ctx, r.pool, applicationID, input.UserID)
}

func (r *ApplicationRepository) DeleteApplicationMember(ctx context.Context, applicationID string, userID string) error {
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
			return domain.ErrApplicationLastOwner
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

func (r *ApplicationRepository) listApplicationEnvironments(ctx context.Context, querier applicationQuerier, applicationID string) ([]domain.ApplicationEnvironmentBinding, error) {
	rows, err := querier.Query(ctx, `
		select
			e.code,
			'-' as traffic,
			e.updated_at,
			e.status
		from application_environment_bindings b
		join environments e on e.code = b.environment_code
		where b.application_id = $1
		order by b.sort_order asc, e.sort_order asc
	`, applicationID)
	if err != nil {
		return nil, fmt.Errorf("query application environments: %w", err)
	}
	defer rows.Close()

	environments := make([]domain.ApplicationEnvironmentBinding, 0)
	for rows.Next() {
		var environment domain.ApplicationEnvironmentBinding
		if err := rows.Scan(
			&environment.Environment,
			&environment.Traffic,
			&environment.UpdatedAt,
			&environment.Status,
		); err != nil {
			return nil, fmt.Errorf("scan application environment: %w", err)
		}
		environment.ID = environment.Environment
		environments = append(environments, environment)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application environments: %w", err)
	}

	return environments, nil
}

func (r *ApplicationRepository) listApplicationMembers(ctx context.Context, querier applicationQuerier, applicationID string) ([]domain.ApplicationMember, error) {
	rows, err := querier.Query(ctx, `
		select
			am.id::text,
			am.user_id,
			u.name,
			u.email,
			u.avatar_url,
			coalesce(array_to_string(u.department_ids, ' / '), ''),
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
			&member.Team,
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

func (r *ApplicationRepository) getApplicationMember(ctx context.Context, querier applicationQuerier, applicationID string, userID string) (domain.ApplicationMember, error) {
	var member domain.ApplicationMember
	if err := querier.QueryRow(ctx, `
		select
			am.id::text,
			am.user_id,
			u.name,
			u.email,
			u.avatar_url,
			coalesce(array_to_string(u.department_ids, ' / '), ''),
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
		&member.Team,
		&member.Role,
	); err != nil {
		if err == pgx.ErrNoRows {
			return domain.ApplicationMember{}, domain.ErrApplicationNotFound
		}
		return domain.ApplicationMember{}, fmt.Errorf("query application member: %w", err)
	}
	return member, nil
}

func (r *ApplicationRepository) listApplicationModules(ctx context.Context, querier applicationQuerier, applicationID string) ([]domain.ApplicationModule, error) {
	rows, err := querier.Query(ctx, `
		select
			m.id,
			m.name,
			m.kind,
			m.common_module_id,
			cm.name as common_module_name,
			m.repository_full_name,
			u.name as owner
		from modules m
		join users u on u.id = m.owner_user_id
		left join modules cm on cm.id = m.common_module_id
		where m.application_id = $1
		order by m.sort_order asc, m.created_at asc
	`, applicationID)
	if err != nil {
		return nil, fmt.Errorf("query application modules: %w", err)
	}
	defer rows.Close()

	modules := make([]domain.ApplicationModule, 0)
	for rows.Next() {
		var module domain.ApplicationModule
		var commonModuleID *string
		var commonModuleName *string
		if err := rows.Scan(
			&module.ID,
			&module.Name,
			&module.Kind,
			&commonModuleID,
			&commonModuleName,
			&module.Repository,
			&module.Owner,
		); err != nil {
			return nil, fmt.Errorf("scan application module: %w", err)
		}
		if commonModuleID != nil {
			module.CommonModuleID = *commonModuleID
		}
		if commonModuleName != nil {
			module.CommonModuleName = *commonModuleName
		}
		modules = append(modules, module)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application modules: %w", err)
	}

	return modules, nil
}

func (r *ApplicationRepository) replaceApplicationRelations(ctx context.Context, tx pgx.Tx, applicationID string, input domain.ApplicationInput) error {
	if _, err := tx.Exec(ctx, `delete from application_environment_bindings where application_id = $1`, applicationID); err != nil {
		return fmt.Errorf("delete application environments: %w", err)
	}
	for index, environment := range input.Environments {
		if _, err := tx.Exec(ctx, `
			insert into application_environment_bindings (
				application_id,
				environment_code,
				sort_order
			) values ($1, $2, $3)
		`,
			applicationID,
			environment.Environment,
			(index+1)*10,
		); err != nil {
			if isForeignKeyViolation(err) {
				return domain.ErrInvalidApplicationInput
			}
			return fmt.Errorf("insert application environment: %w", err)
		}
	}

	return nil
}

func applicationOwnerUser(input domain.ApplicationInput) domain.UserInput {
	return domain.UserInput{
		ID:            input.OwnerUserID,
		Name:          input.Owner,
		Email:         input.OwnerEmail,
		AvatarURL:     input.OwnerAvatarURL,
		DepartmentIDs: input.OwnerDepartmentIDs,
		FeishuUnionID: input.OwnerFeishuUnionID,
		FeishuOpenID:  input.OwnerFeishuOpenID,
		FeishuUserID:  input.OwnerFeishuUserID,
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
