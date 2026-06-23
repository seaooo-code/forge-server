package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

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
	var buildStatus domain.BuildStatus
	if err := tx.QueryRow(ctx, `
		select module_id, environment_code, artifact_key, artifact_url, status
		from build_records
		where id = $1
		for update
	`, input.BuildRecordID).Scan(&moduleID, &environment, &artifactKey, &artifactURL, &buildStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrBuildRecordNotFound
		}
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("query release build record: %w", err)
	}
	if buildStatus != domain.BuildStatusSucceeded || artifactKey == "" {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
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

	found, err := activeReleaseExistsForBuild(
		ctx,
		tx,
		input.BuildRecordID,
		moduleID,
		environment,
		routeType,
		input.Identifier,
	)
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
	}
	if found {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrReleaseAlreadyActive
	}
	if routeType == domain.ReleaseRouteTypePercentage {
		exists, err := activeReleaseRouteExists(ctx, tx, moduleID, environment, routeType, input.Identifier)
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
			created_by_user_id
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
			$12
		)
	`, releaseID, moduleID, environment, input.BuildRecordID, input.ReleaseType, input.GrayStrategy, input.GrayPercentage, input.ExpiresAt, artifactKey, artifactURL, input.Description, input.CreatedByUserID); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("insert release record: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		with deactivated_routes as (
			update release_routes
			set status = 'inactive', updated_at = now()
			where module_id = $1
				and environment_code = $2
				and route_type = $3
				and identifier = $4
				and status = 'active'
			returning release_id
		)
		update release_records rr
		set status = 'inactive', updated_at = now()
		from deactivated_routes
		where rr.id = deactivated_routes.release_id
	`, moduleID, environment, routeType, input.Identifier); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("deactivate previous release route: %w", err)
	}

	routeID := newReleaseRouteID()
	if _, err := tx.Exec(ctx, `
		insert into release_routes (
			id,
			module_id,
			environment_code,
			release_id,
			route_type,
			identifier,
			status,
			created_by_user_id
		) values (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			'active',
			$7
		)
	`, routeID, moduleID, environment, releaseID, routeType, input.Identifier, input.CreatedByUserID); err != nil {
		if isCheckViolation(err) || isUniqueViolation(err) || isForeignKeyViolation(err) {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
		}
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("insert release route: %w", err)
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

func activeReleaseExistsForBuild(ctx context.Context, tx pgx.Tx, buildRecordID string, moduleID string, environment string, routeType domain.ReleaseRouteType, identifier string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `
		select exists(
			select 1
			from release_routes route
			join release_records rr on rr.id = route.release_id
			where route.module_id = $1
				and route.environment_code = $2
				and route.route_type = $3
				and route.identifier = $4
				and route.status = 'active'
				and rr.status = 'active'
				and rr.build_record_id = $5
		)
	`, moduleID, environment, routeType, identifier, buildRecordID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("query existing active release route: %w", err)
	}
	return exists, nil
}

func activeReleaseRouteExists(ctx context.Context, tx pgx.Tx, moduleID string, environment string, routeType domain.ReleaseRouteType, identifier string) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `
		select exists(
			select 1
			from release_routes route
			join release_records rr on rr.id = route.release_id
			where route.module_id = $1
				and route.environment_code = $2
				and route.route_type = $3
				and route.identifier = $4
				and route.status = 'active'
				and rr.status = 'active'
		)
	`, moduleID, environment, routeType, identifier).Scan(&exists); err != nil {
		return false, fmt.Errorf("query active release route: %w", err)
	}
	return exists, nil
}

func releaseRouteTypeForInput(input domain.ReleaseCreateInput) domain.ReleaseRouteType {
	if input.ReleaseType == domain.ReleaseTypeGray {
		switch input.GrayStrategy {
		case domain.ReleaseGrayStrategyTargeted:
			return domain.ReleaseRouteTypeIdentifier
		case domain.ReleaseGrayStrategyPercentage:
			return domain.ReleaseRouteTypePercentage
		}
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

func (r *ReleaseRepository) ListReleases(ctx context.Context, moduleID string, environment string) ([]domain.ReleaseRecord, error) {
	rows, err := r.pool.Query(ctx, releaseRecordSelectSQL()+`
		where ($1 = '' or rr.module_id = $1)
			and ($2 = '' or rr.environment_code = $2)
		order by rr.created_at desc, rr.id desc
	`, moduleID, environment)
	if err != nil {
		return nil, fmt.Errorf("query release records: %w", err)
	}
	defer rows.Close()

	records := make([]domain.ReleaseRecord, 0)
	for rows.Next() {
		record, err := scanReleaseRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate release records: %w", err)
	}
	return records, nil
}

func (r *ReleaseRepository) ExpireTargetedGrayReleases(ctx context.Context) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin expire targeted gray releases: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	expired, err := expireTargetedGrayReleases(ctx, tx)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit expire targeted gray releases: %w", err)
	}
	return expired, nil
}

func expireTargetedGrayReleases(ctx context.Context, tx pgx.Tx) (int64, error) {
	tag, err := tx.Exec(ctx, `
		with expired_routes as (
			update release_routes route
			set status = 'inactive',
				updated_at = now()
			from release_records rr
			where route.release_id = rr.id
				and route.route_type = 'identifier'
				and route.status = 'active'
				and rr.status = 'active'
				and rr.release_type = 'gray'
				and rr.gray_strategy = 'targeted'
				and rr.expires_at is not null
				and rr.expires_at <= now()
			returning route.release_id
		)
		update release_records rr
		set status = 'inactive',
			updated_at = now()
		where rr.id in (select release_id from expired_routes)
	`)
	if err != nil {
		return 0, fmt.Errorf("expire targeted gray releases: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *ReleaseRepository) ResolveReleaseRoute(ctx context.Context, input domain.ReleaseResolveInput) (domain.ReleaseRoute, error) {
	route, err := scanReleaseRoute(r.pool.QueryRow(ctx, releaseRouteSelectSQL()+`
			where route.id = (
				select candidate.id
				from release_routes candidate
				join release_records rr on rr.id = candidate.release_id
				where candidate.module_id = $1
					and candidate.environment_code = $2
					and candidate.status = 'active'
					and rr.status = 'active'
					and (
						(
							$3 <> ''
							and candidate.route_type = 'identifier'
							and candidate.identifier = $3
							and rr.release_type = 'gray'
							and rr.gray_strategy = 'targeted'
							and rr.expires_at > now()
						)
						or (
							$4 <> ''
							and candidate.route_type = 'percentage'
							and rr.release_type = 'gray'
							and rr.gray_strategy = 'percentage'
							and rr.gray_percentage > 0
							and (('x' || substr(md5($4 || ':' || rr.id::text), 1, 7))::bit(28)::int % 100) < rr.gray_percentage
						)
						or (candidate.route_type = 'default' and candidate.identifier = '')
					)
				order by
					case
						when $3 <> '' and candidate.route_type = 'identifier' and candidate.identifier = $3 then 0
						when $4 <> '' and candidate.route_type = 'percentage' then 1
						else 2
					end,
					candidate.created_at desc
				limit 1
			)
		`, input.ModuleID, input.Environment, input.Identifier, input.UserID))
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
	err = tx.QueryRow(ctx, `
		select
			rr.module_id,
			rr.environment_code,
			rr.release_type,
			rr.gray_strategy,
			rr.status,
			route.id,
			route.status
		from release_records rr
		join release_routes route on route.release_id = rr.id
			and route.route_type = 'percentage'
		where rr.id = $1
		for update of rr, route
	`, input.ID).Scan(&moduleID, &environment, &releaseType, &grayStrategy, &releaseStatus, &routeID, &routeStatus)
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
		if err := r.promotePercentageReleaseToFull(ctx, tx, input.ID, routeID, moduleID, environment); err != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
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

func (r *ReleaseRepository) InactivateTargetedGrayRelease(ctx context.Context, input domain.ReleaseInactivateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("begin inactivate targeted gray release: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	var releaseType domain.ReleaseType
	var grayStrategy domain.ReleaseGrayStrategy
	var releaseStatus domain.ReleaseStatus
	var routeID string
	var routeStatus domain.ReleaseRouteStatus
	err = tx.QueryRow(ctx, `
		select
			rr.release_type,
			rr.gray_strategy,
			rr.status,
			route.id,
			route.status
		from release_records rr
		join release_routes route on route.release_id = rr.id
			and route.route_type = 'identifier'
		where rr.id = $1
		for update of rr, route
	`, input.ID).Scan(&releaseType, &grayStrategy, &releaseStatus, &routeID, &routeStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		if exists, existsErr := r.releaseExists(ctx, tx, input.ID); existsErr != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, existsErr
		} else if exists {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
		}
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrReleaseNotFound
	}
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("query targeted gray release: %w", err)
	}
	if releaseType != domain.ReleaseTypeGray ||
		grayStrategy != domain.ReleaseGrayStrategyTargeted ||
		releaseStatus != domain.ReleaseStatusActive ||
		routeStatus != domain.ReleaseRouteStatusActive {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrInvalidReleaseInput
	}

	if _, err := tx.Exec(ctx, `
		update release_routes
		set status = 'inactive',
			updated_at = now()
		where id = $1
	`, routeID); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("inactivate targeted gray route: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update release_records
		set status = 'inactive',
			updated_at = now()
		where id = $1
	`, input.ID); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("inactivate targeted gray release: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("commit inactivate targeted gray release: %w", err)
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

func (r *ReleaseRepository) promotePercentageReleaseToFull(ctx context.Context, tx pgx.Tx, releaseID string, routeID string, moduleID string, environment string) error {
	if _, err := tx.Exec(ctx, `
		with deactivated_routes as (
			update release_routes
			set status = 'inactive', updated_at = now()
			where module_id = $1
				and environment_code = $2
				and route_type = 'default'
				and status = 'active'
			returning release_id
		)
		update release_records rr
		set status = 'inactive', updated_at = now()
		from deactivated_routes
		where rr.id = deactivated_routes.release_id
			and rr.id <> $3
	`, moduleID, environment, releaseID); err != nil {
		return fmt.Errorf("deactivate previous full release route: %w", err)
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
		return fmt.Errorf("promote release record to full: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update release_routes
		set route_type = 'default',
			identifier = '',
			status = 'active',
			updated_at = now()
		where id = $1
	`, routeID); err != nil {
		if isCheckViolation(err) || isUniqueViolation(err) {
			return domain.ErrInvalidReleaseInput
		}
		return fmt.Errorf("promote release route to default: %w", err)
	}
	return nil
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
			route.identifier,
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
			route.route_type,
			route.identifier,
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
		&record.Identifier,
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
		&route.RouteType,
		&route.Identifier,
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
	return "route-" + time.Now().Format("20060102150405-000000000")
}
