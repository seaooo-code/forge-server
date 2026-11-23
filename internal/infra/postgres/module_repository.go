package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ModuleRepository struct {
	pool *pgxpool.Pool
}

type moduleCommonQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func NewModuleRepository(pool *pgxpool.Pool) *ModuleRepository {
	return &ModuleRepository{pool: pool}
}

func (r *ModuleRepository) ListModules(ctx context.Context, filters domain.ModuleListFilters) ([]domain.Module, domain.ModuleSummary, error) {
	summary, err := r.moduleSummary(ctx, filters)
	if err != nil {
		return nil, domain.ModuleSummary{}, err
	}

	rows, err := r.pool.Query(ctx, moduleSelectSQL()+`
		where
			($1 = '' or m.kind = $1)
			and ($2 = '' or m.owner_user_id = $2)
			and ($3 = '' or m.application_id = $3)
			and (
				$4 = ''
				or m.id ilike '%' || $4 || '%'
				or m.name ilike '%' || $4 || '%'
				or m.repository_full_name ilike '%' || $4 || '%'
				or a.name ilike '%' || $4 || '%'
				or cm.name ilike '%' || $4 || '%'
			)
			order by m.sort_order asc, m.created_at asc
			limit nullif($5, 0) offset $6
		`, string(filters.Kind), filters.Owner, filters.ApplicationID, filters.Keyword, filters.Pagination.Limit(), filters.Pagination.Offset())
	if err != nil {
		return nil, domain.ModuleSummary{}, fmt.Errorf("query modules: %w", err)
	}
	defer rows.Close()

	modules := make([]domain.Module, 0)
	for rows.Next() {
		module, err := scanModule(rows)
		if err != nil {
			return nil, domain.ModuleSummary{}, err
		}
		modules = append(modules, module)
	}
	if err := rows.Err(); err != nil {
		return nil, domain.ModuleSummary{}, fmt.Errorf("iterate modules: %w", err)
	}
	return modules, summary, nil
}

func (r *ModuleRepository) moduleSummary(ctx context.Context, filters domain.ModuleListFilters) (domain.ModuleSummary, error) {
	var summary domain.ModuleSummary
	if err := r.pool.QueryRow(ctx, `
		select
			count(*)::integer,
			count(*) filter (where m.kind = 'Common')::integer,
			count(*) filter (where m.kind = 'Feature')::integer
		from modules m
		join applications a on a.id = m.application_id
		left join modules cm on cm.id = m.common_module_id
		where
			($1 = '' or m.kind = $1)
			and ($2 = '' or m.owner_user_id = $2)
			and ($3 = '' or m.application_id = $3)
			and (
				$4 = ''
				or m.id ilike '%' || $4 || '%'
				or m.name ilike '%' || $4 || '%'
				or m.repository_full_name ilike '%' || $4 || '%'
				or a.name ilike '%' || $4 || '%'
				or cm.name ilike '%' || $4 || '%'
			)
	`, string(filters.Kind), filters.Owner, filters.ApplicationID, filters.Keyword).Scan(
		&summary.Total,
		&summary.Common,
		&summary.Feature,
	); err != nil {
		return domain.ModuleSummary{}, fmt.Errorf("summarize modules: %w", err)
	}
	return summary, nil
}

func (r *ModuleRepository) GetModule(ctx context.Context, id string) (domain.Module, error) {
	return scanModule(r.pool.QueryRow(ctx, moduleSelectSQL()+` where m.id = $1`, id))
}

func (r *ModuleRepository) CreateModule(ctx context.Context, input domain.ModuleInput) (domain.Module, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Module{}, fmt.Errorf("begin create module: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	if err := upsertUser(ctx, tx, moduleOwnerUser(input)); err != nil {
		return domain.Module{}, err
	}

	if err := validateModuleCommonBinding(ctx, tx, input.ID, input); err != nil {
		return domain.Module{}, err
	}

	if _, err := tx.Exec(ctx, `
		insert into modules (
			id,
			name,
			kind,
			common_module_id,
			application_id,
			owner_user_id,
			code_source_connection_id,
			external_repository_id,
			repository_full_name,
			repository_web_url,
			repository_clone_url,
			default_branch,
			dockerfile_path,
			build_context,
			auto_build_branches,
			auto_build_environment_code,
			build_mode,
			build_adapter_id,
			build_adapter_params,
			ci_job,
			ci_artifact_path,
			sort_order
		) values (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19::jsonb, $20, $21,
			coalesce((select max(sort_order) + 10 from modules), 10)
		)
	`, moduleWriteArgs(input.ID, input)...); err != nil {
		if isUniqueViolation(err) {
			return domain.Module{}, moduleUniqueViolationError(err)
		}
		if isForeignKeyViolation(err) {
			return domain.Module{}, domain.ErrInvalidModuleInput
		}
		if isCheckViolation(err) {
			return domain.Module{}, domain.ErrInvalidModuleInput
		}
		return domain.Module{}, fmt.Errorf("insert module: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		update applications
		set module_count = module_count + 1, updated_at = now()
		where id = $1
	`, input.ApplicationID); err != nil {
		return domain.Module{}, fmt.Errorf("increment application module count: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Module{}, fmt.Errorf("commit create module: %w", err)
	}

	return r.GetModule(ctx, input.ID)
}

func (r *ModuleRepository) UpdateModule(ctx context.Context, id string, input domain.ModuleInput) (domain.Module, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Module{}, fmt.Errorf("begin update module: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	var previousApplicationID string
	if err := tx.QueryRow(ctx, `
		select application_id
		from modules
		where id = $1
		for update
	`, id).Scan(&previousApplicationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Module{}, domain.ErrModuleNotFound
		}
		return domain.Module{}, fmt.Errorf("query module for update: %w", err)
	}

	if err := upsertUser(ctx, tx, moduleOwnerUser(input)); err != nil {
		return domain.Module{}, err
	}

	if err := validateModuleCommonBinding(ctx, tx, id, input); err != nil {
		return domain.Module{}, err
	}
	if err := validateModuleCommonDependents(ctx, tx, id, input); err != nil {
		return domain.Module{}, err
	}

	if _, err := tx.Exec(ctx, `
		update modules set
			name = $2,
			kind = $3,
			common_module_id = $4,
			application_id = $5,
			owner_user_id = $6,
			code_source_connection_id = $7,
			external_repository_id = $8,
			repository_full_name = $9,
			repository_web_url = $10,
			repository_clone_url = $11,
			default_branch = $12,
			dockerfile_path = $13,
			build_context = $14,
			auto_build_branches = $15,
			auto_build_environment_code = $16,
			build_mode = $17,
			build_adapter_id = $18,
			build_adapter_params = $19::jsonb,
			ci_job = $20,
			ci_artifact_path = $21,
			updated_at = now()
		where id = $1
	`, moduleWriteArgs(id, input)...); err != nil {
		if isUniqueViolation(err) {
			return domain.Module{}, moduleUniqueViolationError(err)
		}
		if isForeignKeyViolation(err) {
			return domain.Module{}, domain.ErrInvalidModuleInput
		}
		if isCheckViolation(err) {
			return domain.Module{}, domain.ErrInvalidModuleInput
		}
		return domain.Module{}, fmt.Errorf("update module: %w", err)
	}

	if previousApplicationID != input.ApplicationID {
		if _, err := tx.Exec(ctx, `
			update applications
			set module_count = greatest(module_count - 1, 0), updated_at = now()
			where id = $1
		`, previousApplicationID); err != nil {
			return domain.Module{}, fmt.Errorf("decrement previous application module count: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			update applications
			set module_count = module_count + 1, updated_at = now()
			where id = $1
		`, input.ApplicationID); err != nil {
			return domain.Module{}, fmt.Errorf("increment next application module count: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Module{}, fmt.Errorf("commit update module: %w", err)
	}

	return r.GetModule(ctx, id)
}

func (r *ModuleRepository) DeleteModule(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete module: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	var applicationID string
	if err := tx.QueryRow(ctx, `
		delete from modules
		where id = $1
		returning application_id
	`, id).Scan(&applicationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrModuleNotFound
		}
		if isForeignKeyViolation(err) {
			return domain.ErrModuleInUse
		}
		return fmt.Errorf("delete module: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		update applications
		set module_count = greatest(module_count - 1, 0), updated_at = now()
		where id = $1
	`, applicationID); err != nil {
		return fmt.Errorf("decrement application module count: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete module: %w", err)
	}
	return nil
}

type moduleRow interface {
	Scan(dest ...any) error
}

func moduleSelectSQL() string {
	return `
		select
			m.id,
			m.name,
			m.kind,
			m.common_module_id,
			cm.name as common_module_name,
			m.application_id,
			a.name as application_name,
			a.slug as application_slug,
			m.owner_user_id,
			u.name as owner,
			u.email as owner_email,
			u.avatar_url as owner_avatar_url,
			u.department_ids as owner_department_ids,
			m.code_source_connection_id,
			csc.provider as code_source_provider,
			csc.name as code_source_connection_name,
			m.external_repository_id,
			m.repository_full_name,
			m.repository_web_url,
			m.repository_clone_url,
			m.default_branch,
			m.dockerfile_path,
			m.build_context,
			m.auto_build_branches,
			coalesce(m.auto_build_environment_code, '') as auto_build_environment_code,
			m.build_mode,
			coalesce(m.build_adapter_id, ''),
			coalesce(ba.name, ''),
			m.build_adapter_params,
			m.ci_job,
			m.ci_artifact_path,
			m.created_at,
			m.updated_at
		from modules m
		join applications a on a.id = m.application_id
		join users u on u.id = m.owner_user_id
		join code_source_connections csc on csc.id = m.code_source_connection_id
		left join modules cm on cm.id = m.common_module_id
		left join build_adapters ba on ba.id = m.build_adapter_id`
}

func scanModule(row moduleRow) (domain.Module, error) {
	var module domain.Module
	var commonModuleID *string
	var commonModuleName *string
	var buildAdapterParams []byte
	if err := row.Scan(
		&module.ID,
		&module.Name,
		&module.Kind,
		&commonModuleID,
		&commonModuleName,
		&module.ApplicationID,
		&module.ApplicationName,
		&module.ApplicationSlug,
		&module.OwnerUserID,
		&module.Owner,
		&module.OwnerEmail,
		&module.OwnerAvatarURL,
		&module.OwnerDepartmentIDs,
		&module.Repository.ConnectionID,
		&module.CodeSourceProvider,
		&module.ConnectionName,
		&module.Repository.ExternalRepositoryID,
		&module.Repository.FullName,
		&module.Repository.WebURL,
		&module.Repository.CloneURL,
		&module.Repository.DefaultBranch,
		&module.BuildConfig.DockerfilePath,
		&module.BuildConfig.BuildContext,
		&module.BuildConfig.AutoBuildBranches,
		&module.BuildConfig.AutoBuildEnvironment,
		&module.BuildConfig.BuildMode,
		&module.BuildConfig.BuildAdapterID,
		&module.BuildConfig.BuildAdapterName,
		&buildAdapterParams,
		&module.BuildConfig.CIJob,
		&module.BuildConfig.CIArtifactPath,
		&module.CreatedAt,
		&module.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Module{}, domain.ErrModuleNotFound
		}
		return domain.Module{}, fmt.Errorf("scan module: %w", err)
	}
	module.BuildConfig.BuildAdapterParams = map[string]string{}
	if err := json.Unmarshal(buildAdapterParams, &module.BuildConfig.BuildAdapterParams); err != nil {
		return domain.Module{}, fmt.Errorf("decode module build adapter params: %w", err)
	}
	if commonModuleID != nil {
		module.CommonModuleID = *commonModuleID
	}
	if commonModuleName != nil {
		module.CommonModuleName = *commonModuleName
	}
	return module, nil
}

func moduleOwnerUser(input domain.ModuleInput) domain.UserInput {
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

// moduleWriteArgs 按 insert / update 共用的占位符顺序（$1-$23）组织参数。
func moduleWriteArgs(id string, input domain.ModuleInput) []any {
	buildMode := input.BuildConfig.BuildMode
	if buildMode == "" {
		buildMode = domain.BuildModeDockerfile
	}
	var buildAdapterID *string
	if input.BuildConfig.BuildAdapterID != "" {
		buildAdapterID = &input.BuildConfig.BuildAdapterID
	}
	buildAdapterParams := input.BuildConfig.BuildAdapterParams
	if buildAdapterParams == nil {
		buildAdapterParams = map[string]string{}
	}
	// map[string]string 编码不会失败。
	buildAdapterParamsJSON, _ := json.Marshal(buildAdapterParams)
	var autoBuildEnvironment *string
	if input.BuildConfig.AutoBuildEnvironment != "" {
		autoBuildEnvironment = &input.BuildConfig.AutoBuildEnvironment
	}
	autoBuildBranches := input.BuildConfig.AutoBuildBranches
	if autoBuildBranches == nil {
		autoBuildBranches = []string{}
	}
	return []any{
		id,
		input.Name,
		input.Kind,
		moduleCommonModuleID(input),
		input.ApplicationID,
		input.OwnerUserID,
		input.Repository.ConnectionID,
		input.Repository.ExternalRepositoryID,
		input.Repository.FullName,
		input.Repository.WebURL,
		input.Repository.CloneURL,
		input.Repository.DefaultBranch,
		input.BuildConfig.DockerfilePath,
		input.BuildConfig.BuildContext,
		autoBuildBranches,
		autoBuildEnvironment,
		string(buildMode),
		buildAdapterID,
		string(buildAdapterParamsJSON),
		input.BuildConfig.CIJob,
		input.BuildConfig.CIArtifactPath,
	}
}

func moduleCommonModuleID(input domain.ModuleInput) *string {
	if input.CommonModuleID == "" {
		return nil
	}
	return &input.CommonModuleID
}

func validateModuleCommonBinding(
	ctx context.Context,
	querier moduleCommonQuerier,
	moduleID string,
	input domain.ModuleInput,
) error {
	if input.Kind == domain.ModuleKindCommon {
		return nil
	}
	if input.CommonModuleID == moduleID && moduleID != "" {
		return domain.ErrInvalidModuleInput
	}

	var applicationID string
	var kind domain.ModuleKind
	if err := querier.QueryRow(ctx, `
		select application_id, kind
		from modules
		where id = $1
	`, input.CommonModuleID).Scan(&applicationID, &kind); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrModuleCommonMissing
		}
		return fmt.Errorf("query common module binding: %w", err)
	}
	if applicationID != input.ApplicationID || kind != domain.ModuleKindCommon {
		return domain.ErrModuleCommonApplication
	}
	return nil
}

func validateModuleCommonDependents(
	ctx context.Context,
	querier moduleCommonQuerier,
	moduleID string,
	input domain.ModuleInput,
) error {
	if moduleID == "" {
		return nil
	}

	var dependentCount int
	if err := querier.QueryRow(ctx, `
		select count(*)
		from modules
		where common_module_id = $1
		  and ($2 <> 'Common' or application_id <> $3)
	`, moduleID, input.Kind, input.ApplicationID).Scan(&dependentCount); err != nil {
		return fmt.Errorf("query common module dependents: %w", err)
	}
	if dependentCount > 0 {
		if input.Kind != domain.ModuleKindCommon {
			return domain.ErrModuleInUse
		}
		return domain.ErrModuleCommonApplication
	}
	return nil
}

func moduleUniqueViolationError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return domain.ErrModuleIDConflict
	}

	switch pgErr.ConstraintName {
	case "modules_pkey":
		return domain.ErrModuleIDConflict
	case "idx_modules_common_application_unique":
		return domain.ErrModuleCommonConflict
	case "modules_repository_dockerfile_unique", "modules_repository_gitlab_ci_unique":
		return domain.ErrModuleRepositoryConflict
	default:
		return domain.ErrModuleIDConflict
	}
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}
