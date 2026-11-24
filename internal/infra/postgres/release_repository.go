package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"forge-server/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ReleaseRepository struct {
	pool *pgxpool.Pool
}

type releaseRecordRow interface {
	Scan(dest ...any) error
}

func NewReleaseRepository(pool *pgxpool.Pool) *ReleaseRepository {
	return &ReleaseRepository{pool: pool}
}

func (r *ReleaseRepository) CreateRelease(ctx context.Context, input domain.ReleaseCreateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("begin create release: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	var moduleID string
	var environment string
	var artifactKey string
	var artifactURL string
	var runtimeVersion string
	var buildStatus domain.BuildStatus
	var build releaseCapabilityBuild
	if err := tx.QueryRow(ctx, `
		select br.module_id, br.environment_code, br.artifact_key, br.artifact_url, br.runtime_version, br.status,
			m.kind, coalesce(m.common_module_id, ''), br.provided_capabilities, br.required_capabilities
		from build_records br
		join modules m on m.id = br.module_id
		where br.id = $1
		for update of br
	`, input.BuildRecordID).Scan(&moduleID, &environment, &artifactKey, &artifactURL, &runtimeVersion, &buildStatus,
		&build.moduleKind, &build.commonModuleID, &build.provides, &build.requires); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrBuildRecordNotFound
		}
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("query release build record: %w", err)
	}
	if buildStatus != domain.BuildStatusSucceeded || artifactKey == "" {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
	}
	if input.Channel != "" {
		// for share 与删除渠道时的 for update 互斥：渠道删除和发布不会并发，删除时的"没有生效发布"检查才可靠。
		var registered bool
		if err := tx.QueryRow(ctx, `
			select exists(
				select 1
				from application_channels ch
				join modules m on m.application_id = ch.application_id
				where m.id = $1 and ch.code = $2
				for share of ch
			)
		`, moduleID, input.Channel).Scan(&registered); err != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("query release channel: %w", err)
		}
		if !registered {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrReleaseChannelNotFound
		}
	}

	var createdByExists bool
	if err := tx.QueryRow(ctx, `
		select exists(select 1 from users where id = $1)
	`, input.CreatedByUserID).Scan(&createdByExists); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("query release creator: %w", err)
	}
	if !createdByExists {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
	}

	routeType := releaseRouteTypeForInput(input)
	build.id, build.moduleID, build.environment, build.runtimeVersion, build.channel = input.BuildRecordID, moduleID, environment, runtimeVersion, input.Channel
	if err := checkReleaseCapabilities(ctx, tx, build); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
	}

	found, err := activeReleaseExistsForBuild(
		ctx,
		tx,
		input.BuildRecordID,
		moduleID,
		environment,
		runtimeVersion,
		input.Channel,
		routeType,
	)
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
	}
	if found {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrReleaseAlreadyActive
	}
	if routeType == domain.ReleaseRouteTypePercentage {
		exists, err := activeReleaseRouteExists(ctx, tx, moduleID, environment, runtimeVersion, input.Channel, routeType)
		if err != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
		}
		if exists {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
		}
	}

	releaseID := newReleaseID()
	if _, err := tx.Exec(ctx, `
		insert into release_records (
			id,
			module_id,
			environment_code,
			build_record_id,
			release_type,
			gray_strategy,
			gray_percentage,
			expires_at,
			status,
			artifact_key,
			artifact_url,
			description,
			created_by_user_id,
			channel
		) values (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			'active',
			$9,
			$10,
			$11,
			$12,
			$13
		)
	`, releaseID, moduleID, environment, input.BuildRecordID, input.ReleaseType, input.GrayStrategy, input.GrayPercentage, nil, artifactKey, artifactURL, input.Description, input.CreatedByUserID, input.Channel); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("insert release record: %w", err)
	}

	// 新发布只替换同一渠道、同一 runtime_version 下的路由，其他渠道和运行时版本的客户端不受影响。
	// 比例灰度替换旧的比例灰度；全量同时结束进行中的比例灰度，否则分到灰度桶的用户会一直拿旧的灰度构建。
	replacedReleaseIDs, err := queryReleaseIDs(ctx, tx, `
		with deactivated_routes as (
			update release_routes
			set status = 'inactive', updated_at = now()
			where module_id = $1
				and environment_code = $2
				and runtime_version = $4
				and channel = $5
				and (route_type = $3 or ($3 = 'default' and route_type = 'percentage'))
				and status = 'active'
			returning release_id
		)
		update release_records rr
		set status = 'inactive', updated_at = now()
		from deactivated_routes
		where rr.id = deactivated_routes.release_id
		returning rr.id::text
	`, moduleID, environment, routeType, runtimeVersion, input.Channel)
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("deactivate previous release route: %w", err)
	}

	routeID := newReleaseRouteID()
	if _, err := tx.Exec(ctx, `
		insert into release_routes (
			id,
			module_id,
			environment_code,
			release_id,
			runtime_version,
			route_type,
			status,
			created_by_user_id,
			channel
		) values (
			$1,
			$2,
			$3,
			$4,
			$7,
			$5,
			'active',
			$6,
			$8
		)
	`, routeID, moduleID, environment, releaseID, routeType, input.CreatedByUserID, runtimeVersion, input.Channel); err != nil {
		if isCheckViolation(err) || isUniqueViolation(err) || isForeignKeyViolation(err) {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
		}
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("insert release route: %w", err)
	}

	createdDetail := map[string]any{"releaseType": input.ReleaseType, "buildRecordId": input.BuildRecordID, "runtimeVersion": runtimeVersion, "channel": input.Channel}
	if input.ReleaseType == domain.ReleaseTypeGray {
		createdDetail["grayPercentage"] = input.GrayPercentage
	}
	if input.RollbackFromReleaseID != "" {
		createdDetail["rollbackFromReleaseId"] = input.RollbackFromReleaseID
	}
	if err := insertReleaseEvent(ctx, tx, releaseID, domain.ReleaseEventCreated, createdDetail, input.CreatedByUserID); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
	}
	for _, replacedID := range replacedReleaseIDs {
		if err := insertReleaseEvent(ctx, tx, replacedID, domain.ReleaseEventReplaced, map[string]any{"byReleaseId": releaseID}, input.CreatedByUserID); err != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("commit create release: %w", err)
	}

	record, err := r.GetRelease(ctx, releaseID)
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
	}
	route, err := r.GetReleaseRoute(ctx, routeID)
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
	}
	return record, route, nil
}

func activeReleaseExistsForBuild(ctx context.Context, tx pgx.Tx, buildRecordID string, moduleID string, environment string, runtimeVersion string, channel string, routeType domain.ReleaseRouteType) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `
		select exists(
			select 1
			from release_routes route
			join release_records rr on rr.id = route.release_id
			where route.module_id = $1
				and route.environment_code = $2
				and route.runtime_version = $5
				and route.channel = $6
				and route.route_type = $3
				and route.status = 'active'
				and rr.status = 'active'
				and rr.build_record_id = $4
		)
	`, moduleID, environment, routeType, buildRecordID, runtimeVersion, channel).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("query existing active release route: %w", err)
	}
	return exists, nil
}

func activeReleaseRouteExists(ctx context.Context, tx pgx.Tx, moduleID string, environment string, runtimeVersion string, channel string, routeType domain.ReleaseRouteType) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `
		select exists(
			select 1
			from release_routes route
			join release_records rr on rr.id = route.release_id
			where route.module_id = $1
				and route.environment_code = $2
				and route.runtime_version = $4
				and route.channel = $5
				and route.route_type = $3
				and route.status = 'active'
				and rr.status = 'active'
		)
	`, moduleID, environment, routeType, runtimeVersion, channel).Scan(&exists); err != nil {
		return false, fmt.Errorf("query active release route: %w", err)
	}
	return exists, nil
}

func releaseRouteTypeForInput(input domain.ReleaseCreateInput) domain.ReleaseRouteType {
	if input.ReleaseType == domain.ReleaseTypeGray && input.GrayStrategy == domain.ReleaseGrayStrategyPercentage {
		return domain.ReleaseRouteTypePercentage
	}
	return domain.ReleaseRouteTypeDefault
}

func (r *ReleaseRepository) GetRelease(ctx context.Context, id string) (domain.ReleaseRecord, error) {
	return scanReleaseRecord(r.pool.QueryRow(ctx, releaseRecordSelectSQL()+`
		where rr.id = $1
	`, id))
}

func (r *ReleaseRepository) GetReleaseRoute(ctx context.Context, id string) (domain.ReleaseRoute, error) {
	return scanReleaseRoute(r.pool.QueryRow(ctx, releaseRouteSelectSQL()+`
		where route.id = $1
	`, id))
}

func (r *ReleaseRepository) ListReleases(ctx context.Context, filters domain.ReleaseRecordFilters) ([]domain.ReleaseRecord, domain.ReleaseRecordSummary, error) {
	summary, err := r.releaseRecordSummary(ctx, filters)
	if err != nil {
		return nil, domain.ReleaseRecordSummary{}, err
	}

	rows, err := r.pool.Query(ctx, releaseRecordSelectSQL()+`
	where ($1 = '' or rr.module_id = $1)
			and ($2 = '' or rr.environment_code = $2)
			and ($3 = '' or rr.status = $3)
			and ($4 = '' or rr.release_type = $4)
			and ($5 = '' or m.application_id = $5)
			and ($8::text is null or rr.channel = $8)
			order by rr.created_at desc, rr.id desc
			limit nullif($6, 0) offset $7
	`, filters.ModuleID, filters.Environment, string(filters.Status), string(filters.ReleaseType), filters.ApplicationID, filters.Pagination.Limit(), filters.Pagination.Offset(), filters.Channel)
	if err != nil {
		return nil, domain.ReleaseRecordSummary{}, fmt.Errorf("query release records: %w", err)
	}
	defer rows.Close()

	records := make([]domain.ReleaseRecord, 0)
	for rows.Next() {
		record, err := scanReleaseRecord(rows)
		if err != nil {
			return nil, domain.ReleaseRecordSummary{}, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, domain.ReleaseRecordSummary{}, fmt.Errorf("iterate release records: %w", err)
	}
	return records, summary, nil
}

func (r *ReleaseRepository) releaseRecordSummary(ctx context.Context, filters domain.ReleaseRecordFilters) (domain.ReleaseRecordSummary, error) {
	var summary domain.ReleaseRecordSummary
	if err := r.pool.QueryRow(ctx, `
		select
			count(*)::integer,
			count(*) filter (where rr.status = 'active')::integer,
			count(*) filter (where rr.status = 'inactive')::integer
		from release_records rr
		join modules m on m.id = rr.module_id
		where ($1 = '' or rr.module_id = $1)
			and ($2 = '' or rr.environment_code = $2)
			and ($3 = '' or rr.status = $3)
			and ($4 = '' or rr.release_type = $4)
			and ($5 = '' or m.application_id = $5)
			and ($6::text is null or rr.channel = $6)
	`, filters.ModuleID, filters.Environment, string(filters.Status), string(filters.ReleaseType), filters.ApplicationID, filters.Channel).Scan(
		&summary.Total,
		&summary.Active,
		&summary.Inactive,
	); err != nil {
		return domain.ReleaseRecordSummary{}, fmt.Errorf("summarize release records: %w", err)
	}
	return summary, nil
}

func (r *ReleaseRepository) ResolveReleaseRoute(ctx context.Context, input domain.ReleaseResolveInput) (domain.ReleaseRoute, error) {
	route, err := scanReleaseRoute(r.pool.QueryRow(ctx, releaseRouteSelectSQL()+`
			where route.id = (
				select candidate.id
				from release_routes candidate
				join release_records rr on rr.id = candidate.release_id
				where candidate.module_id = $1
					and candidate.environment_code = $2
					and ($4 = '' or candidate.runtime_version = $4)
					and (candidate.channel = $5 or candidate.channel = '')
					and candidate.status = 'active'
					and rr.status = 'active'
					and (
						(
							$3 <> ''
							and candidate.route_type = 'percentage'
							and rr.release_type = 'gray'
							and rr.gray_strategy = 'percentage'
							and rr.gray_percentage > 0
							and (('x' || substr(md5($3 || ':' || rr.id::text), 1, 7))::bit(28)::int % 100) < rr.gray_percentage
						)
						or candidate.route_type = 'default'
					)
				order by
					case when candidate.channel = $5 then 0 else 1 end,
					case when candidate.route_type = 'percentage' then 0 else 1 end,
					candidate.created_at desc
				limit 1
			)
		`, input.ModuleID, input.Environment, input.UserID, input.RuntimeVersion, input.Channel))
	if err != nil {
		if errors.Is(err, domain.ErrReleaseRouteNotFound) {
			return domain.ReleaseRoute{}, r.resolveReleaseRouteMissingDependency(ctx, input)
		}
		return domain.ReleaseRoute{}, err
	}
	return route, nil
}

func (r *ReleaseRepository) UpdateReleaseGrayPercentage(ctx context.Context, input domain.ReleaseGrayPercentageUpdateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("begin update release gray percentage: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	var moduleID string
	var environment string
	var releaseType domain.ReleaseType
	var grayStrategy domain.ReleaseGrayStrategy
	var releaseStatus domain.ReleaseStatus
	var routeID string
	var routeStatus domain.ReleaseRouteStatus
	var runtimeVersion string
	var channel string
	var previousPercentage int
	err = tx.QueryRow(ctx, `
		select
			rr.module_id,
			rr.environment_code,
			rr.release_type,
			rr.gray_strategy,
			rr.gray_percentage,
			rr.status,
			route.id,
			route.status,
			route.runtime_version,
			route.channel
		from release_records rr
		join release_routes route on route.release_id = rr.id
			and route.route_type = 'percentage'
		where rr.id = $1
		for update of rr, route
	`, input.ID).Scan(&moduleID, &environment, &releaseType, &grayStrategy, &previousPercentage, &releaseStatus, &routeID, &routeStatus, &runtimeVersion, &channel)
	if errors.Is(err, pgx.ErrNoRows) {
		if exists, existsErr := r.releaseExists(ctx, tx, input.ID); existsErr != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, existsErr
		} else if exists {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
		}
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrReleaseNotFound
	}
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("query percentage release: %w", err)
	}
	if releaseType != domain.ReleaseTypeGray ||
		grayStrategy != domain.ReleaseGrayStrategyPercentage ||
		releaseStatus != domain.ReleaseStatusActive ||
		routeStatus != domain.ReleaseRouteStatusActive {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
	}

	if input.GrayPercentage == 100 {
		replacedReleaseIDs, err := r.promotePercentageReleaseToFull(ctx, tx, input.ID, routeID, moduleID, environment, runtimeVersion, channel)
		if err != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
		}
		if err := insertReleaseEvent(ctx, tx, input.ID, domain.ReleaseEventPromotedToFull, map[string]any{"fromPercentage": previousPercentage}, input.UpdatedByUserID); err != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
		}
		for _, replacedID := range replacedReleaseIDs {
			if err := insertReleaseEvent(ctx, tx, replacedID, domain.ReleaseEventReplaced, map[string]any{"byReleaseId": input.ID}, input.UpdatedByUserID); err != nil {
				return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
			}
		}
	} else {
		if _, err := tx.Exec(ctx, `
			update release_records
			set gray_percentage = $2,
				updated_at = now()
			where id = $1
		`, input.ID, input.GrayPercentage); err != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("update release gray percentage: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			update release_routes
			set updated_at = now()
			where id = $1
		`, routeID); err != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("touch release percentage route: %w", err)
		}
		if err := insertReleaseEvent(ctx, tx, input.ID, domain.ReleaseEventGrayPercentageUpdated,
			map[string]any{"from": previousPercentage, "to": input.GrayPercentage}, input.UpdatedByUserID); err != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("commit update release gray percentage: %w", err)
	}

	record, err := r.GetRelease(ctx, input.ID)
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
	}
	route, err := r.GetReleaseRoute(ctx, routeID)
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
	}
	return record, route, nil
}

func (r *ReleaseRepository) releaseExists(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `
		select exists(select 1 from release_records where id = $1)
	`, id).Scan(&exists); err != nil {
		return false, fmt.Errorf("query release existence: %w", err)
	}
	return exists, nil
}

// promotePercentageReleaseToFull 把百分比灰度转为全量，只顶掉同一渠道、同一 runtime_version 下的默认路由。
func (r *ReleaseRepository) promotePercentageReleaseToFull(ctx context.Context, tx pgx.Tx, releaseID string, routeID string, moduleID string, environment string, runtimeVersion string, channel string) ([]string, error) {
	replacedReleaseIDs, err := queryReleaseIDs(ctx, tx, `
		with deactivated_routes as (
			update release_routes
			set status = 'inactive', updated_at = now()
			where module_id = $1
				and environment_code = $2
				and runtime_version = $4
				and channel = $5
				and route_type = 'default'
				and status = 'active'
			returning release_id
		)
		update release_records rr
		set status = 'inactive', updated_at = now()
		from deactivated_routes
		where rr.id = deactivated_routes.release_id
			and rr.id <> $3
		returning rr.id::text
	`, moduleID, environment, releaseID, runtimeVersion, channel)
	if err != nil {
		return nil, fmt.Errorf("deactivate previous full release route: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update release_records
		set release_type = 'full',
			gray_strategy = '',
			gray_percentage = 0,
			expires_at = null,
			updated_at = now()
		where id = $1
	`, releaseID); err != nil {
		return nil, fmt.Errorf("promote release record to full: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update release_routes
		set route_type = 'default',
			status = 'active',
			updated_at = now()
		where id = $1
	`, routeID); err != nil {
		if isCheckViolation(err) || isUniqueViolation(err) {
			return nil, domain.ErrInvalidReleaseInput
		}
		return nil, fmt.Errorf("promote release route to default: %w", err)
	}
	return replacedReleaseIDs, nil
}

func (r *ReleaseRepository) resolveReleaseRouteMissingDependency(ctx context.Context, input domain.ReleaseResolveInput) error {
	var moduleExists bool
	if err := r.pool.QueryRow(ctx, `
		select exists(select 1 from modules where id = $1)
	`, input.ModuleID).Scan(&moduleExists); err != nil {
		return fmt.Errorf("query release route module dependency: %w", err)
	}
	if !moduleExists {
		return domain.ErrModuleNotFound
	}

	var environmentExists bool
	if err := r.pool.QueryRow(ctx, `
		select exists(select 1 from environments where code = $1)
	`, input.Environment).Scan(&environmentExists); err != nil {
		return fmt.Errorf("query release route environment dependency: %w", err)
	}
	if !environmentExists {
		return domain.ErrEnvironmentNotFound
	}
	return domain.ErrReleaseRouteNotFound
}

func releaseRecordSelectSQL() string {
	return `
		select
			rr.id,
			a.name as application_name,
			rr.module_id,
			m.name as module_name,
			rr.environment_code,
			e.name as environment_name,
			rr.build_record_id,
			route.runtime_version,
			rr.channel,
			rr.release_type,
			rr.gray_strategy,
			rr.gray_percentage,
			rr.expires_at,
			rr.status,
			rr.artifact_key,
			rr.artifact_url,
			rr.description,
			rr.created_by_user_id,
			u.name as created_by,
			rr.created_at,
			rr.updated_at
		from release_records rr
		join modules m on m.id = rr.module_id
		join applications a on a.id = m.application_id
		join environments e on e.code = rr.environment_code
		join users u on u.id = rr.created_by_user_id
		join release_routes route on route.release_id = rr.id
	`
}

func releaseRouteSelectSQL() string {
	return `
		select
			route.id,
			a.name as application_name,
			route.module_id,
			m.name as module_name,
			route.environment_code,
			e.name as environment_name,
			route.release_id,
			rr.build_record_id,
			route.runtime_version,
			route.channel,
			route.route_type,
			rr.gray_strategy,
			rr.gray_percentage,
			route.status,
			rr.artifact_key,
			rr.artifact_url,
			route.created_by_user_id,
			u.name as created_by,
			route.created_at,
			route.updated_at
		from release_routes route
		join release_records rr on rr.id = route.release_id
		join modules m on m.id = route.module_id
		join applications a on a.id = m.application_id
		join environments e on e.code = route.environment_code
		join users u on u.id = route.created_by_user_id
	`
}

func scanReleaseRecord(row releaseRecordRow) (domain.ReleaseRecord, error) {
	var record domain.ReleaseRecord
	var expiresAt sql.NullTime
	if err := row.Scan(
		&record.ID,
		&record.ApplicationName,
		&record.ModuleID,
		&record.ModuleName,
		&record.Environment,
		&record.EnvironmentName,
		&record.BuildRecordID,
		&record.RuntimeVersion,
		&record.Channel,
		&record.ReleaseType,
		&record.GrayStrategy,
		&record.GrayPercentage,
		&expiresAt,
		&record.Status,
		&record.ArtifactKey,
		&record.ArtifactURL,
		&record.Description,
		&record.CreatedByUserID,
		&record.CreatedBy,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ReleaseRecord{}, domain.ErrReleaseNotFound
		}
		return domain.ReleaseRecord{}, fmt.Errorf("scan release record: %w", err)
	}
	if expiresAt.Valid {
		record.ExpiresAt = &expiresAt.Time
	}
	return record, nil
}

func scanReleaseRoute(row releaseRecordRow) (domain.ReleaseRoute, error) {
	var route domain.ReleaseRoute
	if err := row.Scan(
		&route.ID,
		&route.ApplicationName,
		&route.ModuleID,
		&route.ModuleName,
		&route.Environment,
		&route.EnvironmentName,
		&route.ReleaseID,
		&route.BuildRecordID,
		&route.RuntimeVersion,
		&route.Channel,
		&route.RouteType,
		&route.GrayStrategy,
		&route.GrayPercentage,
		&route.Status,
		&route.ArtifactKey,
		&route.ArtifactURL,
		&route.CreatedByUserID,
		&route.CreatedBy,
		&route.CreatedAt,
		&route.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ReleaseRoute{}, domain.ErrReleaseRouteNotFound
		}
		return domain.ReleaseRoute{}, fmt.Errorf("scan release route: %w", err)
	}
	return route, nil
}

func newReleaseID() string {
	return uuid.NewString()
}

func newReleaseRouteID() string {
	return uuid.NewString()
}
