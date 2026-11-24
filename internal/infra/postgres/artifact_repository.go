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
		join code_source_connections csc on csc.id = m.code_source_connection_id
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

	// 每个模块按「提测单 > 渠道比例灰度 > 渠道全量 > 默认渠道比例灰度 > 默认渠道全量」取第一个命中的构建，
	// 规则见 docs/test-plans.md 第 4 节和 docs/channels.md 第 3 节。请求的渠道未注册时 source.Channel 为空，只看默认渠道。
	// 提测单：userId 是成员、提测单进行中且所在环境开启了提测单，条目构建的运行时版本与请求一致。
	rows, err := r.pool.Query(ctx, `
		select
			m.id,
			m.name,
			m.kind,
			coalesce(m.common_module_id, ''),
			coalesce(plan.test_plan_id, ''),
			coalesce(plan.test_plan_name, ''),
			coalesce(plan.matches, 0),
			coalesce(plan.build_record_id, ''),
			coalesce(selected.release_id::text, ''),
			coalesce(selected.release_type, ''),
			coalesce(selected.gray_strategy, ''),
			coalesce(selected.gray_percentage, 0),
			coalesce(selected.channel, ''),
			coalesce(selected.build_record_id::text, ''),
			exists(
				select 1
				from release_routes runtime_route
				join release_records runtime_rr on runtime_rr.id = runtime_route.release_id
				where runtime_route.module_id = m.id
					and runtime_route.environment_code = $2
					and runtime_route.runtime_version = $4
					and runtime_route.channel in ($5, '')
					and runtime_route.route_type in ('default', 'percentage')
					and runtime_route.status = 'active'
					and runtime_rr.status = 'active'
			) as has_runtime_route
		from modules m
		left join lateral (
			select
				tp.id::text as test_plan_id,
				tp.name as test_plan_name,
				item.build_record_id::text as build_record_id,
				count(*) over () as matches
			from test_plan_items item
			join test_plans tp on tp.id = item.test_plan_id
			join test_plan_members member on member.test_plan_id = tp.id and member.user_id = $3
			join environments env on env.code = tp.environment_code
			join build_records plan_build on plan_build.id = item.build_record_id
			where $3 <> ''
				and item.module_id = m.id
				and tp.environment_code = $2
				and tp.status = 'open'
				and tp.expires_at > now()
				and env.allow_test_plans
				and plan_build.runtime_version = $4
			order by item.updated_at desc
			limit 1
		) plan on true
		left join lateral (
			select
				rr.id as release_id,
				rr.release_type,
				rr.gray_strategy,
				rr.gray_percentage,
				route.channel,
				rr.build_record_id
			from release_routes route
			join release_records rr on rr.id = route.release_id
			where route.module_id = m.id
				and route.environment_code = $2
				and route.runtime_version = $4
				and route.channel in ($5, '')
				and route.status = 'active'
				and rr.status = 'active'
				and (
					(
						$3 <> ''
						and route.route_type = 'percentage'
						and rr.release_type = 'gray'
						and rr.gray_strategy = 'percentage'
						and rr.gray_percentage > 0
						and (('x' || substr(md5($3 || ':' || rr.id::text), 1, 7))::bit(28)::int % 100) < rr.gray_percentage
					)
					or route.route_type = 'default'
				)
			order by
				case when route.channel = $5 then 0 else 1 end,
				case when route.route_type = 'percentage' then 0 else 1 end,
				route.created_at desc
			limit 1
		) selected on true
		where m.application_id = $1
		order by m.sort_order asc, m.created_at asc
	`, source.Application.ID, input.Environment, input.UserID, input.RuntimeVersion, source.Channel)
	if err != nil {
		return domain.ArtifactManifestSource{}, fmt.Errorf("query artifact manifest modules: %w", err)
	}
	defer rows.Close()

	source.Modules = []domain.ArtifactModuleReleaseSource{}
	for rows.Next() {
		var module domain.ArtifactModuleReleaseSource
		var releaseType string
		var grayStrategy string
		var planBuildRecordID string
		if err := rows.Scan(
			&module.ID,
			&module.Name,
			&module.Kind,
			&module.CommonModuleID,
			&module.TestPlanID,
			&module.TestPlanName,
			&module.TestPlanMatches,
			&planBuildRecordID,
			&module.ReleaseID,
			&releaseType,
			&grayStrategy,
			&module.GrayPercentage,
			&module.Channel,
			&module.BuildRecordID,
			&module.HasRuntimeRoute,
		); err != nil {
			return domain.ArtifactManifestSource{}, fmt.Errorf("scan artifact manifest module: %w", err)
		}
		if module.TestPlanID != "" {
			// 命中提测单时不返回发布信息，避免客户端误以为这是一次发布。
			module.BuildRecordID = planBuildRecordID
			module.ReleaseID = ""
			releaseType = ""
			grayStrategy = ""
			module.GrayPercentage = 0
			module.Channel = ""
		}
		module.ReleaseType = domain.ReleaseType(releaseType)
		module.GrayStrategy = domain.ReleaseGrayStrategy(grayStrategy)
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
			e.name,
			coalesce((
				select ch.code
				from application_channels ch
				where ch.application_id = a.id and ch.code = $3
			), '')
		from applications a
		join application_environment_bindings b on b.application_id = a.id
		join environments e on e.code = b.environment_code
		where a.slug = $1
			and e.code = $2
	`, input.ApplicationSlug, input.Environment, input.Channel).Scan(
		&source.Application.ID,
		&source.Application.Slug,
		&source.Application.Name,
		&source.Environment.ID,
		&source.Environment.Name,
		&source.Channel,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ArtifactManifestSource{}, domain.ErrArtifactNotFound
		}
		return domain.ArtifactManifestSource{}, fmt.Errorf("query artifact application environment: %w", err)
	}
	return source, nil
}
