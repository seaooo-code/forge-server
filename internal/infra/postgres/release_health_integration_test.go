package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"forge-server/internal/domain"
	"forge-server/internal/service"
)

// 策略开启后，比例灰度的启动失败率明显高于全量时被自动停用，写入 health_unhealthy 和带 auto 的 deactivated 操作记录；
// 全量只记录健康状态；没有上报的发布是 no_reports。
func TestIntegrationReleaseHealthStopsUnhealthyGray(t *testing.T) {
	f := newChannelFixture(t)
	ctx := context.Background()
	repository := NewReleaseHealthRepository(f.pool)

	fullBuild, _ := f.build(t, f.feature, "main", "rt-1")
	grayBuild, _ := f.build(t, f.feature, "main", "rt-1")
	commonBuild, _ := f.build(t, f.common, "main", "rt-1")
	full := f.mustRelease(t, fullBuild, "", 0)
	gray := f.mustRelease(t, grayBuild, "", 10)
	common := f.mustRelease(t, commonBuild, "", 0)

	policies, err := repository.ListReleaseHealthPolicies(ctx, f.slug)
	if err != nil || len(policies) != 1 || policies[0].Configured || policies[0].Enabled || policies[0].MinUsers != 50 {
		t.Fatalf("expected one default policy, got %+v %v", policies, err)
	}
	if err := repository.UpsertReleaseHealthPolicy(ctx, f.slug, domain.ReleaseHealthPolicy{Environment: "missing", WindowMinutes: 30, MinUsers: 1, MaxFailureRate: 0.05}); !errors.Is(err, domain.ErrEnvironmentNotFound) {
		t.Fatalf("expected unbound environment to be rejected, got %v", err)
	}

	healthService := service.NewReleaseHealthService(repository, f.releases, nil)
	if _, err := healthService.UpdateReleaseHealthPolicy(ctx, f.slug, domain.ReleaseHealthPolicy{
		Environment: f.environment, Enabled: true, WindowMinutes: 30, MinUsers: 10, MaxFailureRate: 0.05, MinDelta: 0.02, AutoStopGray: true, UpdatedByUserID: f.userID,
	}); err != nil {
		t.Fatalf("update policy: %v", err)
	}

	targets, err := repository.ListReleaseHealthTargets(ctx)
	if err != nil {
		t.Fatalf("list targets: %v", err)
	}
	var grayTarget domain.ReleaseHealthTarget
	for _, target := range targets {
		if target.ReleaseID == gray.ID {
			grayTarget = target
		}
	}
	if grayTarget.BaselineReleaseID != full.ID || !grayTarget.Policy.Enabled || grayTarget.Policy.MinUsers != 10 {
		t.Fatalf("gray target should use the full release as baseline, got %+v", grayTarget)
	}

	// 全量 100 个用户 1 个失败，灰度 20 个用户 5 个失败（25%）。
	bucket := time.Now().UTC().Truncate(domain.ReleaseHealthBucketSize)
	key := func(releaseID string, buildID string, outcome domain.LaunchOutcome) domain.ReleaseHealthBucketKey {
		return domain.ReleaseHealthBucketKey{BucketStart: bucket, ApplicationID: f.application, Environment: f.environment, ModuleID: f.feature,
			BuildRecordID: buildID, ReleaseID: releaseID, Platform: "android", Outcome: outcome}
	}
	if err := repository.IncrementReleaseHealthBuckets(ctx, []domain.ReleaseHealthBucketIncrement{
		{ReleaseHealthBucketKey: key(full.ID, fullBuild, domain.LaunchOutcomeLaunched), Users: 99, Events: 120},
		{ReleaseHealthBucketKey: key(full.ID, fullBuild, domain.LaunchOutcomeLaunchFailed), Users: 1, Events: 1},
		{ReleaseHealthBucketKey: key(gray.ID, grayBuild, domain.LaunchOutcomeLaunched), Users: 10, Events: 10},
		{ReleaseHealthBucketKey: key(gray.ID, grayBuild, domain.LaunchOutcomeLaunchFailed), Users: 5, Events: 5},
		{ReleaseHealthBucketKey: key(gray.ID, grayBuild, domain.LaunchOutcomeVerifyFailed), Users: 2, Events: 2},
	}); err != nil {
		t.Fatalf("increment: %v", err)
	}
	// 另一个实例写入同一个桶，计数叠加。
	if err := repository.IncrementReleaseHealthBuckets(ctx, []domain.ReleaseHealthBucketIncrement{
		{ReleaseHealthBucketKey: key(gray.ID, grayBuild, domain.LaunchOutcomeLaunched), Users: 5, Events: 6},
	}); err != nil {
		t.Fatalf("increment from another instance: %v", err)
	}
	counts, err := repository.LaunchCounts(ctx, gray.ID, bucket.Add(-time.Hour))
	if err != nil || counts != (domain.LaunchCounts{Launched: 15, LaunchFailed: 5, VerifyFailed: 2}) {
		t.Fatalf("upserted counts should add up, got %+v %v", counts, err)
	}

	if err := healthService.Evaluate(ctx); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	stopped, err := f.releases.GetRelease(ctx, gray.ID)
	if err != nil || stopped.Status != domain.ReleaseStatusInactive || stopped.HealthStatus != domain.ReleaseHealthUnhealthy {
		t.Fatalf("unhealthy gray should be stopped, got %+v %v", stopped, err)
	}
	events, err := f.releases.ListReleaseEvents(ctx, gray.ID)
	if err != nil || len(events) < 3 || events[0].Kind != domain.ReleaseEventDeactivated || events[0].Detail["auto"] != true ||
		events[0].ActorUserID != "" || events[1].Kind != domain.ReleaseEventHealthUnhealthy {
		t.Fatalf("expected health_unhealthy then auto deactivated events, got %+v %v", events, err)
	}
	if record, err := f.releases.GetRelease(ctx, full.ID); err != nil || record.Status != domain.ReleaseStatusActive || record.HealthStatus != domain.ReleaseHealthHealthy {
		t.Fatalf("full release should stay active and healthy, got %+v %v", record, err)
	}
	if record, err := f.releases.GetRelease(ctx, common.ID); err != nil || record.HealthStatus != domain.ReleaseHealthNoReports {
		t.Fatalf("release without reports should be no_reports, got %+v %v", record, err)
	}

	// 现场查询不受策略和发布状态限制，停用后仍能看到当时的数据。
	health, err := healthService.GetReleaseHealth(ctx, gray.ID)
	if err != nil || health.Status != domain.ReleaseHealthUnhealthy || health.Counts.VerifyFailed != 2 || health.BaselineFailureRate == nil {
		t.Fatalf("unexpected live health: %+v %v", health, err)
	}

	// 已停用的发布不再参与判定，再跑一次不会重复写操作记录。
	if err := healthService.Evaluate(ctx); err != nil {
		t.Fatalf("evaluate again: %v", err)
	}
	again, _ := f.releases.ListReleaseEvents(ctx, gray.ID)
	if len(again) != len(events) {
		t.Fatalf("expected no new events after the release stopped, got %d -> %d", len(events), len(again))
	}
}
