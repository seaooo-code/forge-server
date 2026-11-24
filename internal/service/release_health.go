package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"forge-server/internal/domain"

	"github.com/google/uuid"
)

type ReleaseHealthRepository interface {
	ApplicationIDForEnvironment(ctx context.Context, applicationSlug string, environment string) (string, error)
	IncrementReleaseHealthBuckets(ctx context.Context, increments []domain.ReleaseHealthBucketIncrement) error
	DeleteReleaseHealthBucketsBefore(ctx context.Context, before time.Time) (int64, error)
	ListReleaseHealthPolicies(ctx context.Context, applicationSlug string) ([]domain.ReleaseHealthPolicy, error)
	UpsertReleaseHealthPolicy(ctx context.Context, applicationSlug string, policy domain.ReleaseHealthPolicy) error
	ListReleaseHealthTargets(ctx context.Context) ([]domain.ReleaseHealthTarget, error)
	GetReleaseHealthTarget(ctx context.Context, releaseID string) (domain.ReleaseHealthTarget, error)
	LaunchCounts(ctx context.Context, releaseID string, since time.Time) (domain.LaunchCounts, error)
	HasLaunchReports(ctx context.Context, releaseID string) (bool, error)
	SaveReleaseHealth(ctx context.Context, health domain.ReleaseHealth) error
	RecordReleaseHealthEvent(ctx context.Context, releaseID string, kind domain.ReleaseEventKind, detail map[string]any) error
	WithEvaluationLock(ctx context.Context, fn func(context.Context) error) (bool, error)
}

// ReleaseDeactivator 停用发布，由 ReleaseRepository 实现；健康判定自动停用比例灰度时使用。
type ReleaseDeactivator interface {
	DeactivateRelease(ctx context.Context, input domain.ReleaseDeactivateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error)
}

// ReleaseHealthService 接收客户端的启动结果上报、按发布判定健康状态，并在比例灰度不健康时自动停用，规则见 docs/release-health.md。
//
// 上报在内存里按 5 分钟桶聚合，同一个桶里同一个 userId 对同一个构建、同一种结果只计一次，定期批量写库。
// 去重集合只在单个进程里，多个 API 实例之间会有少量重复计数。
type ReleaseHealthService struct {
	repository  ReleaseHealthRepository
	deactivator ReleaseDeactivator
	logger      *slog.Logger
	now         func() time.Time

	mu      sync.Mutex
	pending map[domain.ReleaseHealthBucketKey]*releaseHealthAggregate
	// trackedUsers 是所有去重集合里的 userId 总数，用来限制内存。
	trackedUsers int
}

type releaseHealthAggregate struct {
	users     map[string]struct{}
	newUsers  int64
	newEvents int64
}

const (
	releaseHealthFlushInterval    = 30 * time.Second
	releaseHealthEvaluateInterval = time.Minute
	releaseHealthCleanupInterval  = time.Hour
	// 内存上限：维度数和去重集合里的 userId 总数。超过后新维度不再计数、新用户不再去重计入，避免异常流量耗尽内存。
	maxPendingReleaseHealthKeys  = 50000
	maxTrackedReleaseHealthUsers = 500000
)

func NewReleaseHealthService(repository ReleaseHealthRepository, deactivator ReleaseDeactivator, logger *slog.Logger) *ReleaseHealthService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReleaseHealthService{
		repository:  repository,
		deactivator: deactivator,
		logger:      logger,
		now:         time.Now,
		pending:     map[domain.ReleaseHealthBucketKey]*releaseHealthAggregate{},
	}
}

// RecordLaunchReport 校验并累加一批启动结果，返回计入的事件数。格式不对的单条事件跳过，不让整批失败。
func (s *ReleaseHealthService) RecordLaunchReport(ctx context.Context, report domain.LaunchReport) (int, error) {
	report.ApplicationSlug = strings.TrimSpace(report.ApplicationSlug)
	report.Environment = strings.TrimSpace(report.Environment)
	report.Platform = strings.TrimSpace(report.Platform)
	report.RuntimeVersion = strings.TrimSpace(report.RuntimeVersion)
	report.Channel = strings.TrimSpace(report.Channel)
	report.UserID = strings.TrimSpace(report.UserID)
	if !domain.IsValidApplicationSlug(report.ApplicationSlug) ||
		report.Environment == "" ||
		!domain.IsValidPlatform(report.Platform) ||
		!domain.IsValidRuntimeVersion(report.RuntimeVersion) ||
		report.Channel != "" && !domain.IsValidReleaseChannelCode(report.Channel) ||
		len(report.UserID) > 128 ||
		len(report.Events) == 0 || len(report.Events) > domain.MaxLaunchReportEvents {
		return 0, domain.ErrInvalidLaunchReport
	}
	applicationID, err := s.repository.ApplicationIDForEnvironment(ctx, report.ApplicationSlug, report.Environment)
	if err != nil {
		return 0, err
	}

	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	accepted := 0
	for _, event := range report.Events {
		event.ModuleID = strings.TrimSpace(event.ModuleID)
		event.BuildRecordID = strings.TrimSpace(event.BuildRecordID)
		event.ReleaseID = strings.TrimSpace(event.ReleaseID)
		event.Reason = strings.TrimSpace(event.Reason)
		if event.OccurredAt.IsZero() {
			event.OccurredAt = now
		}
		if !domain.IsValidModuleID(event.ModuleID) ||
			uuid.Validate(event.BuildRecordID) != nil ||
			event.ReleaseID != "" && uuid.Validate(event.ReleaseID) != nil ||
			!domain.IsValidLaunchOutcome(event.Outcome) ||
			len(event.Reason) > domain.MaxLaunchReportReasonBytes ||
			event.OccurredAt.Before(now.Add(-domain.LaunchReportMaxAge)) ||
			event.OccurredAt.After(now.Add(domain.LaunchReportMaxClockSkew)) {
			continue
		}
		key := domain.ReleaseHealthBucketKey{
			BucketStart:   event.OccurredAt.UTC().Truncate(domain.ReleaseHealthBucketSize),
			ApplicationID: applicationID,
			Environment:   report.Environment,
			ModuleID:      event.ModuleID,
			BuildRecordID: event.BuildRecordID,
			ReleaseID:     event.ReleaseID,
			Channel:       report.Channel,
			Platform:      report.Platform,
			Outcome:       event.Outcome,
			Reason:        event.Reason,
		}
		aggregate, ok := s.pending[key]
		if !ok {
			if len(s.pending) >= maxPendingReleaseHealthKeys {
				continue
			}
			aggregate = &releaseHealthAggregate{users: map[string]struct{}{}}
			s.pending[key] = aggregate
		}
		aggregate.newEvents++
		accepted++
		if report.UserID == "" || s.trackedUsers >= maxTrackedReleaseHealthUsers {
			continue
		}
		if _, seen := aggregate.users[report.UserID]; !seen {
			aggregate.users[report.UserID] = struct{}{}
			aggregate.newUsers++
			s.trackedUsers++
		}
	}
	return accepted, nil
}

// Flush 把新增计数写库。去重集合保留到桶结束之后一个桶，期间重复上报的用户不会再计数；写库失败时计数放回内存。
func (s *ReleaseHealthService) Flush(ctx context.Context) error {
	s.mu.Lock()
	increments := make([]domain.ReleaseHealthBucketIncrement, 0, len(s.pending))
	for key, aggregate := range s.pending {
		if aggregate.newEvents == 0 && aggregate.newUsers == 0 {
			continue
		}
		increments = append(increments, domain.ReleaseHealthBucketIncrement{ReleaseHealthBucketKey: key, Users: aggregate.newUsers, Events: aggregate.newEvents})
		aggregate.newUsers, aggregate.newEvents = 0, 0
	}
	expiredBefore := s.now().UTC().Truncate(domain.ReleaseHealthBucketSize).Add(-domain.ReleaseHealthBucketSize)
	for key, aggregate := range s.pending {
		if key.BucketStart.Before(expiredBefore) && aggregate.newEvents == 0 && aggregate.newUsers == 0 {
			s.trackedUsers -= len(aggregate.users)
			delete(s.pending, key)
		}
	}
	s.mu.Unlock()

	if err := s.repository.IncrementReleaseHealthBuckets(ctx, increments); err != nil {
		s.mu.Lock()
		for _, item := range increments {
			aggregate, ok := s.pending[item.ReleaseHealthBucketKey]
			if !ok {
				if len(s.pending) >= maxPendingReleaseHealthKeys {
					continue
				}
				aggregate = &releaseHealthAggregate{users: map[string]struct{}{}}
				s.pending[item.ReleaseHealthBucketKey] = aggregate
			}
			aggregate.newUsers += item.Users
			aggregate.newEvents += item.Events
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

// Run 定期写库、判定和清理过期统计，ctx 结束时返回；退出前的最后一次写库由调用方在关闭数据库前执行 Flush。
func (s *ReleaseHealthService) Run(ctx context.Context) {
	flush := time.NewTicker(releaseHealthFlushInterval)
	defer flush.Stop()
	evaluate := time.NewTicker(releaseHealthEvaluateInterval)
	defer evaluate.Stop()
	cleanup := time.NewTicker(releaseHealthCleanupInterval)
	defer cleanup.Stop()
	s.deleteExpired(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-flush.C:
			if err := s.Flush(ctx); err != nil {
				s.logger.ErrorContext(ctx, "flush release health buckets failed", "error", err)
			}
		case <-evaluate.C:
			if err := s.Evaluate(ctx); err != nil {
				s.logger.ErrorContext(ctx, "evaluate release health failed", "error", err)
			}
		case <-cleanup.C:
			s.deleteExpired(ctx)
		}
	}
}

func (s *ReleaseHealthService) deleteExpired(ctx context.Context) {
	before := s.now().UTC().AddDate(0, 0, -domain.ReleaseHealthRetentionDays)
	deleted, err := s.repository.DeleteReleaseHealthBucketsBefore(ctx, before)
	if err != nil {
		s.logger.ErrorContext(ctx, "delete expired release health buckets failed", "error", err)
		return
	}
	if deleted > 0 {
		s.logger.InfoContext(ctx, "expired release health buckets deleted", "rows", deleted)
	}
}

// Evaluate 判定所有策略开启的生效发布。多个实例时只有拿到锁的实例执行。
// 状态变为 unhealthy 时写一次 health_unhealthy 操作记录；比例灰度且策略开启自动停用时，停用这条发布。
func (s *ReleaseHealthService) Evaluate(ctx context.Context) error {
	_, err := s.repository.WithEvaluationLock(ctx, func(ctx context.Context) error {
		targets, err := s.repository.ListReleaseHealthTargets(ctx)
		if err != nil {
			return err
		}
		for _, target := range targets {
			if err := s.evaluateTarget(ctx, target); err != nil {
				// 一条发布判定失败不影响其他发布。
				s.logger.ErrorContext(ctx, "evaluate release health target failed", "release", target.ReleaseID, "error", err)
			}
		}
		return nil
	})
	return err
}

func (s *ReleaseHealthService) evaluateTarget(ctx context.Context, target domain.ReleaseHealthTarget) error {
	health, err := s.computeHealth(ctx, target)
	if err != nil {
		return err
	}
	if err := s.repository.SaveReleaseHealth(ctx, health); err != nil {
		return err
	}
	if health.Status != domain.ReleaseHealthUnhealthy || target.PreviousStatus == domain.ReleaseHealthUnhealthy {
		return nil
	}

	detail := map[string]any{
		"failureRate":    health.FailureRate,
		"launchedUsers":  health.Counts.Launched,
		"failedUsers":    health.Counts.LaunchFailed,
		"maxFailureRate": target.Policy.MaxFailureRate,
		"minDelta":       target.Policy.MinDelta,
		"windowMinutes":  target.Policy.WindowMinutes,
	}
	if health.BaselineFailureRate != nil {
		detail["baselineFailureRate"] = *health.BaselineFailureRate
	}
	if err := s.repository.RecordReleaseHealthEvent(ctx, target.ReleaseID, domain.ReleaseEventHealthUnhealthy, detail); err != nil {
		return err
	}
	s.logger.WarnContext(ctx, "release unhealthy", "release", target.ReleaseID, "failureRate", health.FailureRate, "users", health.Counts.Launched+health.Counts.LaunchFailed)

	isPercentageGray := target.ReleaseType == domain.ReleaseTypeGray && target.GrayStrategy == domain.ReleaseGrayStrategyPercentage
	if !isPercentageGray || !target.Policy.AutoStopGray || s.deactivator == nil {
		return nil
	}
	autoDetail := map[string]any{"auto": true}
	for key, value := range detail {
		autoDetail[key] = value
	}
	_, _, err = s.deactivator.DeactivateRelease(ctx, domain.ReleaseDeactivateInput{
		ID:     target.ReleaseID,
		Reason: fmt.Sprintf("健康判定自动停用：启动失败率 %.1f%%，阈值 %.1f%%", health.FailureRate*100, target.Policy.MaxFailureRate*100),
		Detail: autoDetail,
	})
	if errors.Is(err, domain.ErrReleaseNotActive) {
		// 判定期间已被人工停用或被新发布替换，不需要再处理。
		return nil
	}
	return err
}

func (s *ReleaseHealthService) computeHealth(ctx context.Context, target domain.ReleaseHealthTarget) (domain.ReleaseHealth, error) {
	now := s.now().UTC()
	windowStart := now.Add(-time.Duration(target.Policy.WindowMinutes) * time.Minute)
	if target.CreatedAt.After(windowStart) {
		windowStart = target.CreatedAt.UTC()
	}
	counts, err := s.repository.LaunchCounts(ctx, target.ReleaseID, windowStart)
	if err != nil {
		return domain.ReleaseHealth{}, err
	}
	hasReports, err := s.repository.HasLaunchReports(ctx, target.ReleaseID)
	if err != nil {
		return domain.ReleaseHealth{}, err
	}
	var baseline *domain.LaunchCounts
	if target.BaselineReleaseID != "" {
		value, err := s.repository.LaunchCounts(ctx, target.BaselineReleaseID, windowStart)
		if err != nil {
			return domain.ReleaseHealth{}, err
		}
		baseline = &value
	}
	status, rate, baselineRate := domain.EvaluateReleaseHealth(target.Policy, hasReports, counts, baseline)
	return domain.ReleaseHealth{
		ReleaseID:           target.ReleaseID,
		Status:              status,
		PolicyEnabled:       target.Policy.Enabled,
		Policy:              target.Policy,
		WindowStart:         windowStart,
		Counts:              counts,
		FailureRate:         rate,
		BaselineFailureRate: baselineRate,
		EvaluatedAt:         now,
	}, nil
}

// GetReleaseHealth 现场计算一条发布的健康状态，不写库、不触发动作。策略关闭时按策略的当前参数（或默认值）计算，供参考。
func (s *ReleaseHealthService) GetReleaseHealth(ctx context.Context, releaseID string) (domain.ReleaseHealth, error) {
	releaseID = strings.TrimSpace(releaseID)
	if uuid.Validate(releaseID) != nil {
		return domain.ReleaseHealth{}, domain.ErrInvalidReleaseHealthInput
	}
	target, err := s.repository.GetReleaseHealthTarget(ctx, releaseID)
	if err != nil {
		return domain.ReleaseHealth{}, err
	}
	return s.computeHealth(ctx, target)
}

func (s *ReleaseHealthService) ListReleaseHealthPolicies(ctx context.Context, applicationSlug string) ([]domain.ReleaseHealthPolicy, error) {
	applicationSlug = strings.TrimSpace(applicationSlug)
	if !domain.IsValidApplicationSlug(applicationSlug) {
		return nil, domain.ErrInvalidReleaseHealthInput
	}
	return s.repository.ListReleaseHealthPolicies(ctx, applicationSlug)
}

// UpdateReleaseHealthPolicy 保存一个环境的策略，返回保存后该应用所有环境的策略。
func (s *ReleaseHealthService) UpdateReleaseHealthPolicy(ctx context.Context, applicationSlug string, policy domain.ReleaseHealthPolicy) ([]domain.ReleaseHealthPolicy, error) {
	applicationSlug = strings.TrimSpace(applicationSlug)
	policy.Environment = strings.TrimSpace(policy.Environment)
	policy.UpdatedByUserID = strings.TrimSpace(policy.UpdatedByUserID)
	if !domain.IsValidApplicationSlug(applicationSlug) || policy.Environment == "" || !policy.Valid() {
		return nil, domain.ErrInvalidReleaseHealthInput
	}
	if err := s.repository.UpsertReleaseHealthPolicy(ctx, applicationSlug, policy); err != nil {
		return nil, err
	}
	return s.repository.ListReleaseHealthPolicies(ctx, applicationSlug)
}
