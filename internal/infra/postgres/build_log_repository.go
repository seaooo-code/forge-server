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

type BuildLogRepository struct {
	pool *pgxpool.Pool
}

func NewBuildLogRepository(pool *pgxpool.Pool) *BuildLogRepository {
	return &BuildLogRepository{pool: pool}
}

func (r *BuildLogRepository) AppendBuildLogChunk(ctx context.Context, chunk domain.BuildLogChunk) error {
	if _, err := r.pool.Exec(ctx, `
		insert into build_log_chunks (build_record_id, seq, content)
		values ($1, $2, $3)
	`, chunk.BuildRecordID, chunk.Seq, chunk.Content); err != nil {
		return fmt.Errorf("append build log chunk: %w", err)
	}
	return nil
}

func (r *BuildLogRepository) NextBuildLogSeq(ctx context.Context, buildID string) (int, error) {
	var next int
	if err := r.pool.QueryRow(ctx, `
		select coalesce(max(seq) + 1, 0)::int
		from build_log_chunks
		where build_record_id = $1
	`, buildID).Scan(&next); err != nil {
		return 0, fmt.Errorf("query next build log seq: %w", err)
	}
	return next, nil
}

func (r *BuildLogRepository) ListBuildLogChunks(ctx context.Context, buildID string, afterSeq int, limit int) ([]domain.BuildLogChunk, domain.BuildStatus, error) {
	// build_records.id 是 uuid，非法 id 直接按不存在处理，避免数据库返回类型错误。
	if _, err := uuid.Parse(buildID); err != nil {
		return nil, "", domain.ErrBuildRecordNotFound
	}

	// 先读状态再读分片：worker 写完全部分片后才把构建标记为结束，读到结束状态时分片一定已经完整。
	var status string
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return nil, "", err
	}
	if err := r.pool.QueryRow(ctx, `
		select br.status from build_records br join modules m on m.id = br.module_id
		where br.id = $1 and ($2::text = '' or m.tenant_id = $2)
	`, buildID, tenantID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", domain.ErrBuildRecordNotFound
		}
		return nil, "", fmt.Errorf("query build record status: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		select build_record_id::text, seq, content, created_at
		from build_log_chunks
		where build_record_id = $1 and seq > $2
		order by seq asc
		limit $3
	`, buildID, afterSeq, limit)
	if err != nil {
		return nil, "", fmt.Errorf("list build log chunks: %w", err)
	}
	defer rows.Close()

	chunks := []domain.BuildLogChunk{}
	for rows.Next() {
		var chunk domain.BuildLogChunk
		if err := rows.Scan(&chunk.BuildRecordID, &chunk.Seq, &chunk.Content, &chunk.CreatedAt); err != nil {
			return nil, "", fmt.Errorf("scan build log chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("list build log chunks: %w", err)
	}
	return chunks, domain.BuildStatus(status), nil
}

func (r *BuildLogRepository) DeleteBuildLogChunksBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `delete from build_log_chunks where created_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("delete expired build log chunks: %w", err)
	}
	return tag.RowsAffected(), nil
}
