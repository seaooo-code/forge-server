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
	provides       domain.Capabilities
	requires       domain.Capabilities
}

// releaseCapabilityCounterpart 是同一环境、运行时版本下生效中的另一方发布。
type releaseCapabilityCounterpart struct {
	conflict domain.ReleaseCapabilityConflict
	provides domain.Capabilities
	requires domain.Capabilities
}

// checkReleaseCapabilities 保证发布后，同一环境、运行时版本下客户端可能拿到的每一对 Common 与 Feature 构建都兼容：
//   - 发布 Feature 构建：它的 requires 要被所属 Common 模块每个生效中的发布（全量和比例灰度）满足；
//     声明了 requires 而 Common 没有任何生效中的发布时也拒绝。
//   - 发布 Common 构建：它的 provides 要满足所有绑定它的 Feature 模块每个生效中的发布。
//
// 比例灰度转全量、停用发布不会引入新的构建，不需要再校验。
// 同一个 Common 模块、环境、运行时版本下的发布用 advisory lock 串行，避免 Common 和 Feature 并发发布时各自通过校验。
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

	conflicts := []domain.ReleaseCapabilityConflict{}
	if build.moduleKind == domain.ModuleKindCommon {
		features, err := activeCapabilityCounterparts(ctx, tx, `m.common_module_id = $1`, commonModuleID, build)
		if err != nil {
			return err
		}
		for _, feature := range features {
			if shortfalls := domain.UnmetCapabilities(feature.requires, build.provides); len(shortfalls) > 0 {
				feature.conflict.Reason = domain.ReleaseCapabilityUnmet
				feature.conflict.Shortfalls = shortfalls
				conflicts = append(conflicts, feature.conflict)
			}
		}
	} else if len(build.requires) > 0 {
		commons, err := activeCapabilityCounterparts(ctx, tx, `m.id = $1`, commonModuleID, build)
		if err != nil {
			return err
		}
		if len(commons) == 0 {
			var moduleName string
			if err := tx.QueryRow(ctx, `select name from modules where id = $1`, commonModuleID).Scan(&moduleName); err != nil {
				return fmt.Errorf("query common module: %w", err)
			}
			conflicts = append(conflicts, domain.ReleaseCapabilityConflict{
				ModuleID:   commonModuleID,
				ModuleName: moduleName,
				Reason:     domain.ReleaseCapabilityCommonMissing,
				Shortfalls: domain.UnmetCapabilities(build.requires, nil),
			})
		}
		for _, common := range commons {
			if shortfalls := domain.UnmetCapabilities(build.requires, common.provides); len(shortfalls) > 0 {
				common.conflict.Reason = domain.ReleaseCapabilityUnmet
				common.conflict.Shortfalls = shortfalls
				conflicts = append(conflicts, common.conflict)
			}
		}
	}
	if len(conflicts) > 0 {
		return &domain.ReleaseCapabilityConflictError{Conflicts: conflicts}
	}
	return nil
}

// activeCapabilityCounterparts 列出 moduleFilter 选中的模块在本次发布的环境、运行时版本下生效中的发布及其构建能力。
// 本次发布自己所在模块的发布不算在内：Feature 发布不需要和自己的旧发布比较。
func activeCapabilityCounterparts(ctx context.Context, tx pgx.Tx, moduleFilter string, moduleArg string, build releaseCapabilityBuild) ([]releaseCapabilityCounterpart, error) {
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
		order by m.sort_order, m.created_at, route.route_type
	`, moduleArg, build.moduleID, build.environment, build.runtimeVersion)
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
