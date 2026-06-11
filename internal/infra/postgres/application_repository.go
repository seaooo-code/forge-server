package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

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

func (r *ApplicationRepository) ListApplications(ctx context.Context) ([]domain.Application, error) {
	rows, err := r.pool.Query(ctx, `
		select
			a.id,
			a.name,
			a.slug,
			a.platform,
			a.owner,
			a.owner_email,
			a.owner_avatar_url,
			a.owner_department_ids,
			a.module_count,
			count(b.environment_code)::integer as environment_count,
			a.created_at,
			a.status
		from applications a
		left join application_environment_bindings b on b.application_id = a.id
		group by
			a.id,
			a.name,
			a.slug,
			a.platform,
			a.owner,
			a.owner_email,
			a.owner_avatar_url,
			a.owner_department_ids,
			a.module_count,
			a.created_at,
			a.status,
			a.sort_order
		order by a.sort_order asc, a.created_at asc
	`)
	if err != nil {
		return nil, fmt.Errorf("query applications: %w", err)
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
			&application.Owner,
			&application.OwnerEmail,
			&application.OwnerAvatarURL,
			&application.OwnerDepartmentIDs,
			&application.ModuleCount,
			&application.EnvironmentCount,
			&application.CreatedAt,
			&application.Status,
		); err != nil {
			return nil, fmt.Errorf("scan application: %w", err)
		}
		applications = append(applications, application)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applications: %w", err)
	}

	return applications, nil
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

	applicationID := "app-" + input.Slug
	if _, err := tx.Exec(ctx, `
		insert into applications (
			id,
			slug,
			name,
			status,
			platform,
			owner,
			owner_email,
			owner_avatar_url,
			owner_department_ids,
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
			$8,
			$9,
			$10,
			0,
			coalesce((select max(sort_order) + 10 from applications), 10)
		)
	`, applicationID,
		input.Slug,
		input.Name,
		input.Status,
		input.Platform,
		input.Owner,
		input.OwnerEmail,
		input.OwnerAvatarURL,
		input.OwnerDepartmentIDs,
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

	var applicationID string
	if err := tx.QueryRow(ctx, `
		update applications set
			name = $2,
			status = $3,
			platform = $4,
			owner = $5,
			owner_email = $6,
			owner_avatar_url = $7,
			owner_department_ids = $8,
			description = $9,
			updated_at = now()
		where slug = $1
		returning id
	`,
		slug,
		input.Name,
		input.Status,
		input.Platform,
		input.Owner,
		input.OwnerEmail,
		input.OwnerAvatarURL,
		input.OwnerDepartmentIDs,
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

func (r *ApplicationRepository) DeleteApplication(ctx context.Context, slug string) (domain.ApplicationDetail, error) {
	commandTag, err := r.pool.Exec(ctx, `
		update applications
		set status = $2, updated_at = now()
		where slug = $1
	`, slug, domain.ApplicationStatusOffline)
	if err != nil {
		return domain.ApplicationDetail{}, fmt.Errorf("soft delete application: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return domain.ApplicationDetail{}, domain.ErrApplicationNotFound
	}

	return r.GetApplicationDetail(ctx, slug)
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
			a.owner,
			a.owner_email,
			a.owner_avatar_url,
			a.owner_department_ids,
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
		where a.slug = $1
	`, slug).Scan(
		&detail.Application.ID,
		&detail.Application.Name,
		&detail.Application.Slug,
		&detail.Application.Platform,
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

	detail.ID = detail.Application.ID
	detail.AppKey = "rn." + detail.Application.Slug
	detail.Slug = detail.Application.Slug
	detail.Name = detail.Application.Name
	detail.OwnerDepartmentIDs = detail.Application.OwnerDepartmentIDs
	detail.Modules = []domain.ApplicationModule{}
	detail.Pipelines = []domain.ApplicationPipeline{}

	return detail, nil
}

func (r *ApplicationRepository) ListApplicationOwners(ctx context.Context) ([]domain.ApplicationOptionItem, error) {
	rows, err := r.pool.Query(ctx, `
		select owner
		from applications
		where owner <> ''
		group by owner
		order by min(sort_order) asc, owner asc
	`)
	if err != nil {
		return nil, fmt.Errorf("query application owners: %w", err)
	}
	defer rows.Close()

	owners := make([]domain.ApplicationOptionItem, 0)
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return nil, fmt.Errorf("scan application owner: %w", err)
		}
		owners = append(owners, domain.ApplicationOptionItem{
			Label: owner,
			Value: owner,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application owners: %w", err)
	}

	return owners, nil
}

func (r *ApplicationRepository) listApplicationEnvironments(ctx context.Context, querier applicationQuerier, applicationID string) ([]domain.ApplicationEnvironmentBinding, error) {
	rows, err := querier.Query(ctx, `
		select
			e.code,
			e.current_combination,
			e.bundle_count,
			e.traffic_label,
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
			&environment.Combination,
			&environment.BundleCount,
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
		select member_key, name, role, team
		from application_members
		where application_id = $1
		order by sort_order asc
	`, applicationID)
	if err != nil {
		return nil, fmt.Errorf("query application members: %w", err)
	}
	defer rows.Close()

	members := make([]domain.ApplicationMember, 0)
	for rows.Next() {
		var member domain.ApplicationMember
		if err := rows.Scan(&member.ID, &member.Name, &member.Role, &member.Team); err != nil {
			return nil, fmt.Errorf("scan application member: %w", err)
		}
		members = append(members, member)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application members: %w", err)
	}

	return members, nil
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
			return fmt.Errorf("insert application environment: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `delete from application_members where application_id = $1`, applicationID); err != nil {
		return fmt.Errorf("delete application members: %w", err)
	}
	for index, member := range input.Members {
		if _, err := tx.Exec(ctx, `
			insert into application_members (
				application_id,
				member_key,
				name,
				role,
				team,
				sort_order
			) values ($1, $2, $3, $4, $5, $6)
		`,
			applicationID,
			memberKey(member, index),
			member.Name,
			member.Role,
			member.Team,
			(index+1)*10,
		); err != nil {
			return fmt.Errorf("insert application member: %w", err)
		}
	}

	return nil
}

func memberKey(member domain.ApplicationMemberInput, index int) string {
	value := strings.TrimSpace(strings.ToLower(member.Name))
	value = strings.ReplaceAll(value, ".", "-")
	value = strings.ReplaceAll(value, "_", "-")
	if value == "" {
		return "member-" + strconv.Itoa(index+1)
	}
	return value
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
