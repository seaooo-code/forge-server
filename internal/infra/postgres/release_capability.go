package postgres

import (
	"context"
	"fmt"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5"
)

// releaseCapabilityBuild 是本次要发布的构建，以及校验能力约束需要的模块信息。
type releaseCapabilityBuild struct {
	id             string
	moduleID       string
	moduleKind     domain.ModuleKind
	commonModuleID string
	environment    string
	runtimeVersion string
	// channel 是本次发布的渠道，默认渠道为空字符串。
	channel  string
	provides domain.Capabilities
	requires domain.Capabilities
}

// releaseCapabilityCounterpart 是同一环境、运行时版本下生效中的另一方发布。
type releaseCapabilityCounterpart struct {
	conflict domain.ReleaseCapabilityConflict
	provides domain.Capabilities
	requires domain.Capabilities
}

// checkReleaseCapabilities 保证发布后，同一环境、运行时版本下客户端可能拿到的每一对 Common 与 Feature 构建都兼容：
//   - 发布 Feature 构建：它的 requires 要被所属 Common 模块每个可能同时拿到的发布（全量和比例灰度）满足；
//     声明了 requires 而 Common 没有任何可拿到的发布时也拒绝。
//   - 发布 Common 构建：它的 provides 要满足所有绑定它的 Feature 模块每个可能同时拿到的发布。
//
// 渠道按模块回落到默认渠道（见 docs/channels.md 第 5.1 节），所以要按渠道分别算"可能同时拿到"的集合：
//   - 发布到渠道 C：只有 C 的客户端会拿到新构建，只检查 C。
//   - 发布到默认渠道：默认渠道的客户端，以及在本模块上没有生效全量、会回落到默认渠道的其他渠道客户端都会拿到新构建。
//     只需要额外检查在对方模块上有生效发布的渠道，其余渠道在对方模块上的集合和默认渠道相同。
//
// 比例灰度转全量、停用发布不会引入新的构建，不需要再校验。
// 同一个 Common 模块、环境、运行时版本下的发布用 advisory lock 串行（不区分渠道，默认渠道的发布会影响所有渠道），
// 避免 Common 和 Feature 并发发布时各自通过校验。
func checkReleaseCapabilities(ctx context.Context, tx pgx.Tx, build releaseCapabilityBuild) error {
	commonModuleID := build.commonModuleID
	if build.moduleKind == domain.ModuleKindCommon {
		commonModuleID = build.moduleID
	}
	if commonModuleID == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('forge:release_capability:' || $1 || '/' || $2 || '/' || $3, 0))`,
		commonModuleID, build.environment, build.runtimeVersion); err != nil {
		return fmt.Errorf("lock release capability scope: %w", err)
	}

	counterpartFilter := `m.id = $1`
	if build.moduleKind == domain.ModuleKindCommon {
		counterpartFilter = `m.common_module_id = $1`
	} else if len(build.requires) == 0 {
		return nil
	}
	channels := []string{build.channel}
	if build.channel == "" {
		fallbackChannels, err := fallbackCapabilityChannels(ctx, tx, counterpartFilter, commonModuleID, build)
		if err != nil {
			return err
		}
		channels = append(channels, fallbackChannels...)
	}

	conflicts := []domain.ReleaseCapabilityConflict{}
	seen := map[string]bool{}
	addConflict := func(conflict domain.ReleaseCapabilityConflict) {
		key := conflict.Reason + "/" + conflict.ModuleID + "/" + conflict.ReleaseID
		if !seen[key] {
			seen[key] = true
			conflicts = append(conflicts, conflict)
		}
	}
	for _, channel := range channels {
		counterparts, err := activeCapabilityCounterparts(ctx, tx, counterpartFilter, commonModuleID, build, channel)
		if err != nil {
			return err
		}
		if build.moduleKind == domain.ModuleKindCommon {
			for _, feature := range counterparts {
				if shortfalls := domain.UnmetCapabilities(feature.requires, build.provides); len(shortfalls) > 0 {
					feature.conflict.Reason = domain.ReleaseCapabilityUnmet
					feature.conflict.Shortfalls = shortfalls
					addConflict(feature.conflict)
				}
			}
			continue
		}
		if len(counterparts) == 0 {
			var moduleName string
			if err := tx.QueryRow(ctx, `select name from modules where id = $1`, commonModuleID).Scan(&moduleName); err != nil {
				return fmt.Errorf("query common module: %w", err)
			}
			addConflict(domain.ReleaseCapabilityConflict{
				ModuleID:   commonModuleID,
				ModuleName: moduleName,
				Reason:     domain.ReleaseCapabilityCommonMissing,
				Shortfalls: domain.UnmetCapabilities(build.requires, nil),
			})
		}
		for _, common := range counterparts {
			if shortfalls := domain.UnmetCapabilities(build.requires, common.provides); len(shortfalls) > 0 {
				common.conflict.Reason = domain.ReleaseCapabilityUnmet
				common.conflict.Shortfalls = shortfalls
				addConflict(common.conflict)
			}
		}
	}
	if len(conflicts) > 0 {
		return &domain.ReleaseCapabilityConflictError{Conflicts: conflicts}
	}
	return nil
}

// fallbackCapabilityChannels 列出发布到默认渠道时还要检查的渠道：在对方模块上有生效发布，
// 且在本模块上没有生效全量（会回落到默认渠道、拿到本次构建）的渠道。
func fallbackCapabilityChannels(ctx context.Context, tx pgx.Tx, moduleFilter string, moduleArg string, build releaseCapabilityBuild) ([]string, error) {
	rows, err := tx.Query(ctx, `
		select distinct route.channel
		from release_routes route
		join release_records rr on rr.id = route.release_id
		join modules m on m.id = route.module_id
		where `+moduleFilter+`
			and m.id <> $2
			and route.environment_code = $3
			and route.runtime_version = $4
			and route.channel <> ''
			and route.route_type in ('default', 'percentage')
			and route.status = 'active'
			and rr.status = 'active'
			and not exists (
				select 1
				from release_routes own
				join release_records own_rr on own_rr.id = own.release_id
				where own.module_id = $2
					and own.environment_code = $3
					and own.runtime_version = $4
					and own.channel = route.channel
					and own.route_type = 'default'
					and own.status = 'active'
					and own_rr.status = 'active'
			)
		order by route.channel
	`, moduleArg, build.moduleID, build.environment, build.runtimeVersion)
	if err != nil {
		return nil, fmt.Errorf("query fallback release channels: %w", err)
	}
	channels, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("collect fallback release channels: %w", err)
	}
	return channels, nil
}

// activeCapabilityCounterparts 列出 moduleFilter 选中的模块在本次发布的环境、运行时版本下，渠道 channel 的客户端可能拿到的生效发布及其构建能力：
// 该渠道自己的发布；渠道非空且在该模块上没有生效全量时，再加上默认渠道的发布。
// 本次发布自己所在模块的发布不算在内：Feature 发布不需要和自己的旧发布比较。
func activeCapabilityCounterparts(ctx context.Context, tx pgx.Tx, moduleFilter string, moduleArg string, build releaseCapabilityBuild, channel string) ([]releaseCapabilityCounterpart, error) {
	rows, err := tx.Query(ctx, `
		select m.id, m.name, rr.id::text, rr.release_type, br.id::text, br.number,
			br.provided_capabilities, br.required_capabilities
		from release_routes route
		join release_records rr on rr.id = route.release_id
		join build_records br on br.id = rr.build_record_id
		join modules m on m.id = route.module_id
		where `+moduleFilter+`
			and m.id <> $2
			and route.environment_code = $3
			and route.runtime_version = $4
			and route.route_type in ('default', 'percentage')
			and route.status = 'active'
			and rr.status = 'active'
			and (
				route.channel = $5
				or (
					route.channel = ''
					and $5 <> ''
					and not exists (
						select 1
						from release_routes channel_full
						join release_records channel_rr on channel_rr.id = channel_full.release_id
						where channel_full.module_id = route.module_id
							and channel_full.environment_code = $3
							and channel_full.runtime_version = $4
							and channel_full.channel = $5
							and channel_full.route_type = 'default'
							and channel_full.status = 'active'
							and channel_rr.status = 'active'
					)
				)
			)
		order by m.sort_order, m.created_at, route.channel, route.route_type
	`, moduleArg, build.moduleID, build.environment, build.runtimeVersion, channel)
	if err != nil {
		return nil, fmt.Errorf("query active release capabilities: %w", err)
	}
	defer rows.Close()

	counterparts := []releaseCapabilityCounterpart{}
	for rows.Next() {
		var item releaseCapabilityCounterpart
		if err := rows.Scan(&item.conflict.ModuleID, &item.conflict.ModuleName, &item.conflict.ReleaseID, &item.conflict.ReleaseType,
			&item.conflict.BuildRecordID, &item.conflict.BuildNumber, &item.provides, &item.requires); err != nil {
			return nil, fmt.Errorf("scan active release capabilities: %w", err)
		}
		counterparts = append(counterparts, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active release capabilities: %w", err)
	}
	return counterparts, nil
}
