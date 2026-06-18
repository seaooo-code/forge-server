package postgres

import (
	"context"
	"errors"
	"fmt"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5"
)

type ArtifactRepository struct {
	pool artifactQuerier
}

type artifactQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func NewArtifactRepository(pool artifactQuerier) *ArtifactRepository {
	return &ArtifactRepository{pool: pool}
}

func (r *ArtifactRepository) GetArtifactBuildRecord(ctx context.Context, input domain.ArtifactDownloadInput) (domain.BuildRecord, error) {
	record, err := scanBuildRecord(r.pool.QueryRow(ctx, buildRecordSelectSQL("build_records", `
		join modules m on m.id = br.module_id
		join applications a on a.id = m.application_id
		join environments e on e.code = br.environment_code
		join storage_configurations s on s.id = br.storage_configuration_id
		where a.slug = $1
			and br.module_id = $2
			and br.environment_code = $3
			and br.id = $4
	`), input.ApplicationSlug, input.ModuleID, input.Environment, input.BuildRecordID))
	if err != nil {
		if errors.Is(err, domain.ErrBuildRecordNotFound) {
			return domain.BuildRecord{}, domain.ErrArtifactNotFound
		}
		return domain.BuildRecord{}, err
	}
	return record, nil
}

func (r *ArtifactRepository) GetArtifactManifestSource(ctx context.Context, input domain.ArtifactManifestInput) (domain.ArtifactManifestSource, error) {
	source, err := r.getArtifactApplicationEnvironment(ctx, input)
	if err != nil {
		return domain.ArtifactManifestSource{}, err
	}

	rows, err := r.pool.Query(ctx, `
		select
			m.id,
			m.name,
			m.kind,
			coalesce(selected.release_id::text, ''),
			coalesce(selected.release_type, ''),
			coalesce(selected.identifier, ''),
			coalesce(selected.build_record_id::text, '')
		from modules m
		left join lateral (
			select
				rr.id as release_id,
				rr.release_type,
				route.identifier,
				rr.build_record_id
			from release_routes route
			join release_records rr on rr.id = route.release_id
			where route.module_id = m.id
				and route.environment_code = $2
				and route.status = 'active'
				and rr.status = 'active'
				and (
					($3 <> '' and route.route_type = 'identifier' and route.identifier = $3)
					or route.route_type = 'default'
				)
			order by
				case when $3 <> '' and route.route_type = 'identifier' and route.identifier = $3 then 0 else 1 end,
				route.created_at desc
			limit 1
		) selected on true
		where m.application_id = $1
		order by m.sort_order asc, m.created_at asc
	`, source.Application.ID, input.Environment, input.Identifier)
	if err != nil {
		return domain.ArtifactManifestSource{}, fmt.Errorf("query artifact manifest modules: %w", err)
	}
	defer rows.Close()

	source.Modules = []domain.ArtifactModuleReleaseSource{}
	for rows.Next() {
		var module domain.ArtifactModuleReleaseSource
		var releaseType string
		if err := rows.Scan(
			&module.ID,
			&module.Name,
			&module.Kind,
			&module.ReleaseID,
			&releaseType,
			&module.Identifier,
			&module.BuildRecordID,
		); err != nil {
			return domain.ArtifactManifestSource{}, fmt.Errorf("scan artifact manifest module: %w", err)
		}
		module.ReleaseType = domain.ReleaseType(releaseType)
		source.Modules = append(source.Modules, module)
	}
	if err := rows.Err(); err != nil {
		return domain.ArtifactManifestSource{}, fmt.Errorf("iterate artifact manifest modules: %w", err)
	}
	return source, nil
}

func (r *ArtifactRepository) getArtifactApplicationEnvironment(ctx context.Context, input domain.ArtifactManifestInput) (domain.ArtifactManifestSource, error) {
	var source domain.ArtifactManifestSource
	if err := r.pool.QueryRow(ctx, `
		select
			a.id,
			a.slug,
			a.name,
			e.code,
			e.name
		from applications a
		join application_environment_bindings b on b.application_id = a.id
		join environments e on e.code = b.environment_code
		where a.slug = $1
			and e.code = $2
	`, input.ApplicationSlug, input.Environment).Scan(
		&source.Application.ID,
		&source.Application.Slug,
		&source.Application.Name,
		&source.Environment.ID,
		&source.Environment.Name,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ArtifactManifestSource{}, domain.ErrArtifactNotFound
		}
		return domain.ArtifactManifestSource{}, fmt.Errorf("query artifact application environment: %w", err)
	}
	return source, nil
}
