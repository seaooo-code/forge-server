package postgres

import (
	"context"
	"fmt"

	"forge-server/internal/domain"
)

// ListAutoBuildModules 返回绑定到该连接和仓库、并且配置了自动构建分支与环境的模块。
// 分支 glob 匹配在 service 层完成。
func (r *ModuleRepository) ListAutoBuildModules(ctx context.Context, connectionID string, externalRepositoryID string) ([]domain.Module, error) {
	rows, err := r.pool.Query(ctx, moduleSelectSQL()+`
		where m.code_source_connection_id = $1
			and m.external_repository_id = $2
			and cardinality(m.auto_build_branches) > 0
			and m.auto_build_environment_id is not null
		order by m.sort_order asc, m.created_at asc
	`, connectionID, externalRepositoryID)
	if err != nil {
		return nil, fmt.Errorf("query auto build modules: %w", err)
	}
	defer rows.Close()

	modules := make([]domain.Module, 0)
	for rows.Next() {
		module, err := scanModule(rows)
		if err != nil {
			return nil, err
		}
		modules = append(modules, module)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate auto build modules: %w", err)
	}
	return modules, nil
}
