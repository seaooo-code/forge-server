package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5"
)

// queryReleaseIDs 执行返回一列发布 ID 的语句（例如带 returning 的失效语句），读完结果后才能在同一事务里执行下一条语句。
func queryReleaseIDs(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]string, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func insertReleaseEvent(ctx context.Context, tx pgx.Tx, releaseID string, kind domain.ReleaseEventKind, detail map[string]any, actorUserID string) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("encode release event detail: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into release_events (release_id, kind, detail, actor_user_id)
		values ($1, $2, $3, nullif($4, ''))
	`, releaseID, kind, raw, actorUserID); err != nil {
		return fmt.Errorf("insert release event: %w", err)
	}
	return nil
}

// DeactivateRelease 停用一条生效中的发布：发布记录和路由都标为失效，不会自动恢复其他发布。
func (r *ReleaseRepository) DeactivateRelease(ctx context.Context, input domain.ReleaseDeactivateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("begin deactivate release: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	var routeID string
	var status domain.ReleaseStatus
	if err := tx.QueryRow(ctx, `
		select route.id, rr.status
		from release_records rr
		join release_routes route on route.release_id = rr.id
		where rr.id = $1
		for update of rr, route
	`, input.ID).Scan(&routeID, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrReleaseNotFound
		}
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("query release for deactivation: %w", err)
	}
	if status != domain.ReleaseStatusActive {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, domain.ErrReleaseNotActive
	}
	if _, err := tx.Exec(ctx, `update release_records set status = 'inactive', updated_at = now() where id = $1`, input.ID); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("deactivate release record: %w", err)
	}
	if _, err := tx.Exec(ctx, `update release_routes set status = 'inactive', updated_at = now() where id = $1`, routeID); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("deactivate release route: %w", err)
	}
	if err := insertReleaseEvent(ctx, tx, input.ID, domain.ReleaseEventDeactivated, map[string]any{"reason": input.Reason}, input.ActorUserID); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, fmt.Errorf("commit deactivate release: %w", err)
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

// PreviousFullRelease 返回同一模块、环境、运行时版本下，早于该发布、构建不同的最近一次全量发布。没有时返回 ErrReleaseRollbackUnavailable。
// 比例灰度转成的全量也算全量。
func (r *ReleaseRepository) PreviousFullRelease(ctx context.Context, releaseID string) (domain.ReleaseRecord, error) {
	var previousID string
	if err := r.pool.QueryRow(ctx, `
		select previous.id::text
		from release_records current
		join release_routes current_route on current_route.release_id = current.id
		join release_records previous on previous.module_id = current.module_id
			and previous.environment_code = current.environment_code
			and previous.release_type = 'full'
			and previous.build_record_id <> current.build_record_id
			and previous.created_at < current.created_at
		join release_routes previous_route on previous_route.release_id = previous.id
			and previous_route.runtime_version = current_route.runtime_version
		where current.id = $1
		order by previous.created_at desc
		limit 1
	`, releaseID).Scan(&previousID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ReleaseRecord{}, domain.ErrReleaseRollbackUnavailable
		}
		return domain.ReleaseRecord{}, fmt.Errorf("query previous full release: %w", err)
	}
	return r.GetRelease(ctx, previousID)
}

// ListReleaseEvents 按时间倒序返回发布的操作记录，最多 MaxReleaseEvents 条。
func (r *ReleaseRepository) ListReleaseEvents(ctx context.Context, releaseID string) ([]domain.ReleaseEvent, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `select exists(select 1 from release_records where id = $1)`, releaseID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("query release existence: %w", err)
	}
	if !exists {
		return nil, domain.ErrReleaseNotFound
	}
	rows, err := r.pool.Query(ctx, `
		select event.id, event.release_id::text, event.kind, event.detail, coalesce(event.actor_user_id, ''), coalesce(u.name, ''), event.created_at
		from release_events event
		left join users u on u.id = event.actor_user_id
		where event.release_id = $1
		order by event.created_at desc, event.id desc
		limit $2
	`, releaseID, domain.MaxReleaseEvents)
	if err != nil {
		return nil, fmt.Errorf("query release events: %w", err)
	}
	defer rows.Close()

	events := []domain.ReleaseEvent{}
	for rows.Next() {
		var event domain.ReleaseEvent
		var createdAt time.Time
		if err := rows.Scan(&event.ID, &event.ReleaseID, &event.Kind, &event.Detail, &event.ActorUserID, &event.ActorName, &createdAt); err != nil {
			return nil, fmt.Errorf("scan release event: %w", err)
		}
		event.CreatedAt = createdAt
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate release events: %w", err)
	}
	return events, nil
}
