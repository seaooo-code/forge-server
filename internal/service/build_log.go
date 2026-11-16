package service

import (
	"context"
	"strings"
	"time"

	"forge-server/internal/domain"
)

// buildLogPageSize 限制单次查询返回的分片数。单个分片最大 64KB，worker 侧总量受 FORGE_WORKER_LOG_MAX_BYTES 限制。
const buildLogPageSize = 100

type BuildLogRepository interface {
	AppendBuildLogChunk(ctx context.Context, chunk domain.BuildLogChunk) error
	DeleteBuildLogChunksBefore(ctx context.Context, before time.Time) (int64, error)
	// ListBuildLogChunks 先读取构建状态再读取 seq > afterSeq 的分片，最多返回 limit 条；构建不存在时返回 domain.ErrBuildRecordNotFound。
	ListBuildLogChunks(ctx context.Context, buildID string, afterSeq int, limit int) ([]domain.BuildLogChunk, domain.BuildStatus, error)
	// NextBuildLogSeq 返回下一个可用的 seq。构建被其他 worker 重新抢占时，日志接着已有分片继续写。
	NextBuildLogSeq(ctx context.Context, buildID string) (int, error)
}

type BuildLogService struct {
	repository BuildLogRepository
	now        func() time.Time
}

func NewBuildLogService(repository BuildLogRepository) *BuildLogService {
	return &BuildLogService{repository: repository, now: time.Now}
}

// ListBuildLogs 返回 seq > afterSeq 的分片；afterSeq 传 -1 表示从头读取。
// finished 只在构建已结束且没有更多分片时为 true，worker 会在写完全部日志后才把构建标记为结束。
func (s *BuildLogService) ListBuildLogs(ctx context.Context, buildID string, afterSeq int) (domain.BuildLogPage, error) {
	buildID = strings.TrimSpace(buildID)
	if buildID == "" {
		return domain.BuildLogPage{}, domain.ErrBuildRecordNotFound
	}
	if afterSeq < -1 {
		return domain.BuildLogPage{}, domain.ErrInvalidBuildInput
	}

	chunks, status, err := s.repository.ListBuildLogChunks(ctx, buildID, afterSeq, buildLogPageSize+1)
	if err != nil {
		return domain.BuildLogPage{}, err
	}
	hasMore := len(chunks) > buildLogPageSize
	if hasMore {
		chunks = chunks[:buildLogPageSize]
	}
	if chunks == nil {
		chunks = []domain.BuildLogChunk{}
	}
	terminal := status == domain.BuildStatusSucceeded || status == domain.BuildStatusFailed
	return domain.BuildLogPage{Chunks: chunks, Finished: terminal && !hasMore}, nil
}

func (s *BuildLogService) NextBuildLogSeq(ctx context.Context, buildID string) (int, error) {
	return s.repository.NextBuildLogSeq(ctx, buildID)
}

func (s *BuildLogService) AppendBuildLogChunk(ctx context.Context, chunk domain.BuildLogChunk) error {
	if strings.TrimSpace(chunk.BuildRecordID) == "" || chunk.Seq < 0 || chunk.Content == "" {
		return domain.ErrInvalidBuildInput
	}
	return s.repository.AppendBuildLogChunk(ctx, chunk)
}

// DeleteExpiredBuildLogs 删除早于保留期的日志分片，由 API 进程的后台巡检调用。
func (s *BuildLogService) DeleteExpiredBuildLogs(ctx context.Context, retention time.Duration) (int64, error) {
	if retention <= 0 {
		return 0, domain.ErrInvalidBuildInput
	}
	return s.repository.DeleteBuildLogChunksBefore(ctx, s.now().Add(-retention))
}
