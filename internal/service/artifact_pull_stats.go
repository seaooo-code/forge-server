package service

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"forge-server/internal/domain"

	"github.com/google/uuid"
)

type ArtifactPullStatsRepository interface {
	IncrementArtifactPullStats(ctx context.Context, stats []domain.ArtifactPullStat) error
	ListArtifactPullStats(ctx context.Context, filters domain.ArtifactPullStatsFilters) ([]domain.ArtifactPullStatsRow, error)
	DeleteArtifactPullStatsBefore(ctx context.Context, day time.Time) (int64, error)
}

// ArtifactPullStatsService 统计客户端拉包。manifest 请求在内存里按天累加，定期批量写库：
// 拉包是高频请求，逐次写库会把每次请求变成多条 upsert。进程异常退出时最多丢失一个写库周期的计数。
// 多个 API 实例各自累加后用 upsert 叠加，结果仍然正确。
type ArtifactPullStatsService struct {
	repository ArtifactPullStatsRepository
	logger     *slog.Logger
	now        func() time.Time

	mu      sync.Mutex
	pending map[domain.ArtifactPullStatKey]int64
}

const (
	artifactPullStatsFlushInterval   = 30 * time.Second
	artifactPullStatsCleanupInterval = time.Hour
	// maxPendingArtifactPullStats 限制内存里待写入的维度数，异常流量（大量不同 runtimeVersion）下不会无限增长。
	maxPendingArtifactPullStats = 50000
)

func NewArtifactPullStatsService(repository ArtifactPullStatsRepository, logger *slog.Logger) *ArtifactPullStatsService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ArtifactPullStatsService{
		repository: repository,
		logger:     logger,
		now:        time.Now,
		pending:    map[domain.ArtifactPullStatKey]int64{},
	}
}

// RecordManifest 记录一次 manifest 请求里每个模块的解析结果。
func (s *ArtifactPullStatsService) RecordManifest(manifest domain.ArtifactApplicationManifest) {
	day := s.today()
	key := func(moduleID string, outcome string, buildRecordID string) domain.ArtifactPullStatKey {
		return domain.ArtifactPullStatKey{
			Day: day, ApplicationID: manifest.Application.ID, Environment: manifest.Environment.ID, ModuleID: moduleID,
			RuntimeVersion: manifest.RuntimeVersion, Platform: manifest.Platform, Channel: manifest.Channel, Outcome: outcome, BuildRecordID: buildRecordID,
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	add := func(k domain.ArtifactPullStatKey) {
		if _, ok := s.pending[k]; !ok && len(s.pending) >= maxPendingArtifactPullStats {
			return
		}
		s.pending[k]++
	}
	for _, module := range manifest.Modules {
		add(key(module.ID, string(module.Source), module.BuildRecordID))
	}
	for _, module := range manifest.MissingModules {
		add(key(module.ID, domain.ArtifactPullOutcomeMissingPrefix+module.Reason, ""))
	}
}

// Flush 把内存里的计数写库。写库失败时计数放回内存，下次再写。
func (s *ArtifactPullStatsService) Flush(ctx context.Context) error {
	s.mu.Lock()
	if len(s.pending) == 0 {
		s.mu.Unlock()
		return nil
	}
	batch := s.pending
	s.pending = map[domain.ArtifactPullStatKey]int64{}
	s.mu.Unlock()

	stats := make([]domain.ArtifactPullStat, 0, len(batch))
	for key, requests := range batch {
		stats = append(stats, domain.ArtifactPullStat{ArtifactPullStatKey: key, Requests: requests})
	}
	if err := s.repository.IncrementArtifactPullStats(ctx, stats); err != nil {
		s.mu.Lock()
		for key, requests := range batch {
			if _, ok := s.pending[key]; ok || len(s.pending) < maxPendingArtifactPullStats {
				s.pending[key] += requests
			}
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

// Run 定期写库并清理超过保留天数的统计，ctx 结束时返回；退出前的最后一次写库由调用方在关闭数据库前执行 Flush。
func (s *ArtifactPullStatsService) Run(ctx context.Context) {
	flush := time.NewTicker(artifactPullStatsFlushInterval)
	defer flush.Stop()
	cleanup := time.NewTicker(artifactPullStatsCleanupInterval)
	defer cleanup.Stop()
	s.deleteExpired(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-flush.C:
			if err := s.Flush(ctx); err != nil {
				s.logger.ErrorContext(ctx, "flush artifact pull stats failed", "error", err)
			}
		case <-cleanup.C:
			s.deleteExpired(ctx)
		}
	}
}

func (s *ArtifactPullStatsService) deleteExpired(ctx context.Context) {
	before := s.today().AddDate(0, 0, -domain.ArtifactPullStatsRetentionDays)
	deleted, err := s.repository.DeleteArtifactPullStatsBefore(ctx, before)
	if err != nil {
		s.logger.ErrorContext(ctx, "delete expired artifact pull stats failed", "error", err)
		return
	}
	if deleted > 0 {
		s.logger.InfoContext(ctx, "expired artifact pull stats deleted", "rows", deleted)
	}
}

// ListArtifactPullStats 返回最近 Days 天（含今天）的统计，Days 默认 7，最多 MaxArtifactPullStatsQueryDays。
func (s *ArtifactPullStatsService) ListArtifactPullStats(ctx context.Context, filters domain.ArtifactPullStatsFilters) ([]domain.ArtifactPullStatsRow, time.Time, time.Time, error) {
	normalized := domain.ArtifactPullStatsFilters{
		ApplicationID: strings.TrimSpace(filters.ApplicationID),
		Environment:   strings.TrimSpace(filters.Environment),
		ModuleID:      strings.TrimSpace(filters.ModuleID),
		BuildRecordID: strings.TrimSpace(filters.BuildRecordID),
		Days:          filters.Days,
	}
	if normalized.Days == 0 {
		normalized.Days = 7
	}
	if normalized.ApplicationID == "" && normalized.ModuleID == "" && normalized.BuildRecordID == "" || normalized.Days < 1 || normalized.Days > domain.MaxArtifactPullStatsQueryDays ||
		normalized.ModuleID != "" && !domain.IsValidModuleID(normalized.ModuleID) ||
		normalized.BuildRecordID != "" && uuid.Validate(normalized.BuildRecordID) != nil {
		return nil, time.Time{}, time.Time{}, domain.ErrInvalidArtifactPullStatsInput
	}
	to := s.today()
	from := to.AddDate(0, 0, 1-normalized.Days)
	normalized.From = from
	rows, err := s.repository.ListArtifactPullStats(ctx, normalized)
	if err != nil {
		return nil, time.Time{}, time.Time{}, err
	}
	return rows, from, to, nil
}

// today 按服务端本地时区的自然日取日期，以 UTC 零点的 time.Time 表示，和数据库 date 列对应。
func (s *ArtifactPullStatsService) today() time.Time {
	now := s.now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}
