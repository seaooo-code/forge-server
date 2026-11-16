package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"forge-server/internal/domain"
)

func TestListBuildLogsFinishedOnlyWhenTerminalAndComplete(t *testing.T) {
	tests := []struct {
		name     string
		status   domain.BuildStatus
		chunks   int
		finished bool
	}{
		{name: "running", status: domain.BuildStatusRunning, chunks: 3, finished: false},
		{name: "succeeded", status: domain.BuildStatusSucceeded, chunks: 3, finished: true},
		{name: "failed with more pages", status: domain.BuildStatusFailed, chunks: buildLogPageSize + 1, finished: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &fakeBuildLogRepository{status: tt.status}
			for seq := 0; seq < tt.chunks; seq++ {
				repository.chunks = append(repository.chunks, domain.BuildLogChunk{Seq: seq, Content: "line"})
			}
			page, err := NewBuildLogService(repository).ListBuildLogs(context.Background(), "build-1", -1)
			if err != nil {
				t.Fatalf("list logs: %v", err)
			}
			if page.Finished != tt.finished || len(page.Chunks) > buildLogPageSize {
				t.Fatalf("unexpected page: finished=%v chunks=%d", page.Finished, len(page.Chunks))
			}
			if repository.lastLimit != buildLogPageSize+1 || repository.lastAfterSeq != -1 {
				t.Fatalf("unexpected query: limit=%d after=%d", repository.lastLimit, repository.lastAfterSeq)
			}
		})
	}
}

func TestListBuildLogsRejectsInvalidAfterSeq(t *testing.T) {
	_, err := NewBuildLogService(&fakeBuildLogRepository{}).ListBuildLogs(context.Background(), "build-1", -2)
	if !errors.Is(err, domain.ErrInvalidBuildInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestDeleteExpiredBuildLogsUsesRetention(t *testing.T) {
	repository := &fakeBuildLogRepository{}
	service := NewBuildLogService(repository)
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	if _, err := service.DeleteExpiredBuildLogs(context.Background(), 30*24*time.Hour); err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if !repository.deletedBefore.Equal(now.Add(-30 * 24 * time.Hour)) {
		t.Fatalf("unexpected cutoff %v", repository.deletedBefore)
	}
}

type fakeBuildLogRepository struct {
	chunks        []domain.BuildLogChunk
	status        domain.BuildStatus
	lastAfterSeq  int
	lastLimit     int
	deletedBefore time.Time
}

func (r *fakeBuildLogRepository) AppendBuildLogChunk(_ context.Context, chunk domain.BuildLogChunk) error {
	r.chunks = append(r.chunks, chunk)
	return nil
}

func (r *fakeBuildLogRepository) DeleteBuildLogChunksBefore(_ context.Context, before time.Time) (int64, error) {
	r.deletedBefore = before
	return 0, nil
}

func (r *fakeBuildLogRepository) ListBuildLogChunks(_ context.Context, _ string, afterSeq int, limit int) ([]domain.BuildLogChunk, domain.BuildStatus, error) {
	r.lastAfterSeq = afterSeq
	r.lastLimit = limit
	chunks := r.chunks
	if len(chunks) > limit {
		chunks = chunks[:limit]
	}
	return chunks, r.status, nil
}

func (r *fakeBuildLogRepository) NextBuildLogSeq(context.Context, string) (int, error) {
	return len(r.chunks), nil
}
