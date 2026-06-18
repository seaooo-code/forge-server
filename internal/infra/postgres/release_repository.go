package postgres

import (
	"context"
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

	routeType := domain.ReleaseRouteTypeDefault
	if input.ReleaseType == domain.ReleaseTypeTargeted {
		routeType = domain.ReleaseRouteTypeIdentifier
	}

	existingReleaseID, existingRouteID, found, err := findActiveReleaseIDsForBuild(
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
		if err := tx.Commit(ctx); err != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("commit existing release lookup: %w", err)
		}

		record, err := r.GetRelease(ctx, existingReleaseID)
		if err != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
		}
		route, err := r.GetReleaseRoute(ctx, existingRouteID)
		if err != nil {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
		}
		return record, route, nil
	}

	releaseID := newReleaseID()
	if _, err := tx.Exec(ctx, `
		insert into release_records (
			id,
			module_id,
			environment_code,
			build_record_id,
			release_type,
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
			'active',
			$6,
			$7,
			$8,
			$9
		)
	`, releaseID, moduleID, environment, input.BuildRecordID, input.ReleaseType, artifactKey, artifactURL, input.Description, input.CreatedByUserID); err != nil {
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

func findActiveReleaseIDsForBuild(ctx context.Context, tx pgx.Tx, buildRecordID string, moduleID string, environment string, routeType domain.ReleaseRouteType, identifier string) (string, string, bool, error) {
	var releaseID string
	var routeID string
	err := tx.QueryRow(ctx, `
		select rr.id, route.id
		from release_routes route
		join release_records rr on rr.id = route.release_id
		where route.module_id = $1
			and route.environment_code = $2
			and route.route_type = $3
			and route.identifier = $4
			and route.status = 'active'
			and rr.status = 'active'
			and rr.build_record_id = $5
		limit 1
	`, moduleID, environment, routeType, identifier, buildRecordID).Scan(&releaseID, &routeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("query existing active release route: %w", err)
	}
	return releaseID, routeID, true, nil
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

func (r *ReleaseRepository) ResolveReleaseRoute(ctx context.Context, input domain.ReleaseResolveInput) (domain.ReleaseRoute, error) {
	route, err := scanReleaseRoute(r.pool.QueryRow(ctx, releaseRouteSelectSQL()+`
		where route.id = (
			select id
			from release_routes
			where module_id = $1
				and environment_code = $2
				and status = 'active'
				and (
					($3 <> '' and route_type = 'identifier' and identifier = $3)
					or (route_type = 'default' and identifier = '')
				)
			order by
				case when $3 <> '' and route_type = 'identifier' then 0 else 1 end,
				created_at desc
			limit 1
		)
	`, input.ModuleID, input.Environment, input.Identifier))
	if err != nil {
		if errors.Is(err, domain.ErrReleaseRouteNotFound) {
			return domain.ReleaseRoute{}, r.resolveReleaseRouteMissingDependency(ctx, input)
		}
		return domain.ReleaseRoute{}, err
	}
	return route, nil
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
