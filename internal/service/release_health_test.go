package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"forge-server/internal/domain"
)

type fakeReleaseHealthRepository struct {
	increments   [][]domain.ReleaseHealthBucketIncrement
	incrementErr error
	targets      []domain.ReleaseHealthTarget
	counts       map[string]domain.LaunchCounts
	reported     map[string]bool
	saved        []domain.ReleaseHealth
	events       []domain.ReleaseEventKind
	locked       bool
}

func (r *fakeReleaseHealthRepository) ApplicationIDForEnvironment(_ context.Context, slug string, env string) (string, error) {
	if slug != "forge-demo" || env != "test" {
		return "", domain.ErrArtifactNotFound
	}
	return "app-1", nil
}

func (r *fakeReleaseHealthRepository) IncrementReleaseHealthBuckets(_ context.Context, increments []domain.ReleaseHealthBucketIncrement) error {
	if r.incrementErr != nil {
		return r.incrementErr
	}
	r.increments = append(r.increments, increments)
	return nil
}

func (r *fakeReleaseHealthRepository) DeleteReleaseHealthBucketsBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (r *fakeReleaseHealthRepository) ListReleaseHealthPolicies(context.Context, string) ([]domain.ReleaseHealthPolicy, error) {
	return nil, nil
}

func (r *fakeReleaseHealthRepository) UpsertReleaseHealthPolicy(context.Context, string, domain.ReleaseHealthPolicy) error {
	return nil
}

func (r *fakeReleaseHealthRepository) ListReleaseHealthTargets(context.Context) ([]domain.ReleaseHealthTarget, error) {
	return r.targets, nil
}

func (r *fakeReleaseHealthRepository) GetReleaseHealthTarget(_ context.Context, id string) (domain.ReleaseHealthTarget, error) {
	for _, target := range r.targets {
		if target.ReleaseID == id {
			return target, nil
		}
	}
	return domain.ReleaseHealthTarget{}, domain.ErrReleaseNotFound
}

func (r *fakeReleaseHealthRepository) LaunchCounts(_ context.Context, id string, _ time.Time) (domain.LaunchCounts, error) {
	return r.counts[id], nil
}

func (r *fakeReleaseHealthRepository) HasLaunchReports(_ context.Context, id string) (bool, error) {
	return r.reported[id], nil
}

func (r *fakeReleaseHealthRepository) SaveReleaseHealth(_ context.Context, health domain.ReleaseHealth) error {
	r.saved = append(r.saved, health)
	return nil
}

func (r *fakeReleaseHealthRepository) RecordReleaseHealthEvent(_ context.Context, _ string, kind domain.ReleaseEventKind, _ map[string]any) error {
	r.events = append(r.events, kind)
	return nil
}

func (r *fakeReleaseHealthRepository) WithEvaluationLock(ctx context.Context, fn func(context.Context) error) (bool, error) {
	if r.locked {
		return false, nil
	}
	return true, fn(ctx)
}

type fakeReleaseDeactivator struct {
	inputs []domain.ReleaseDeactivateInput
}

func (d *fakeReleaseDeactivator) DeactivateRelease(_ context.Context, input domain.ReleaseDeactivateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	d.inputs = append(d.inputs, input)
	return domain.ReleaseRecord{}, domain.ReleaseRoute{}, nil
}

const (
	healthBuildID   = "c4938f50-7fc8-4187-a322-412211b4f905"
	healthReleaseID = "28e76366-7b15-43e6-b00d-f9882fea10d0"
)

func newHealthReport(userID string, events ...domain.LaunchReportEvent) domain.LaunchReport {
	return domain.LaunchReport{ApplicationSlug: "forge-demo", Environment: "test", Platform: "android", RuntimeVersion: "rt-1", Channel: "beta", UserID: userID, Events: events}
}

// 同一个桶里同一个用户只计一次；没有 userId 的事件只计事件数；格式不对、过期的事件跳过；写库后再上报不会重复计数。
func TestReleaseHealthServiceRecordsAndDeduplicatesReports(t *testing.T) {
	repository := &fakeReleaseHealthRepository{}
	service := NewReleaseHealthService(repository, nil, nil)
	now := time.Date(2026, 11, 24, 8, 3, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	ctx := context.Background()
	launched := domain.LaunchReportEvent{ModuleID: "claw", BuildRecordID: healthBuildID, ReleaseID: healthReleaseID, Outcome: domain.LaunchOutcomeLaunched, OccurredAt: now}

	accepted, err := service.RecordLaunchReport(ctx, newHealthReport("u1", launched, launched,
		domain.LaunchReportEvent{ModuleID: "claw", BuildRecordID: "not-a-uuid", Outcome: domain.LaunchOutcomeLaunched},
		domain.LaunchReportEvent{ModuleID: "claw", BuildRecordID: healthBuildID, Outcome: "crashed"},
		domain.LaunchReportEvent{ModuleID: "claw", BuildRecordID: healthBuildID, Outcome: domain.LaunchOutcomeLaunched, OccurredAt: now.Add(-25 * time.Hour)},
		domain.LaunchReportEvent{ModuleID: "claw", BuildRecordID: healthBuildID, Outcome: domain.LaunchOutcomeLaunched, Reason: strings.Repeat("x", 65)},
	))
	if err != nil || accepted != 2 {
		t.Fatalf("expected 2 accepted events, got %d %v", accepted, err)
	}
	if _, err := service.RecordLaunchReport(ctx, newHealthReport("", launched)); err != nil {
		t.Fatalf("report without user: %v", err)
	}
	if err := service.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if len(repository.increments) != 1 || len(repository.increments[0]) != 1 {
		t.Fatalf("expected one aggregated key, got %+v", repository.increments)
	}
	item := repository.increments[0][0]
	if item.Users != 1 || item.Events != 3 || item.ApplicationID != "app-1" || item.Channel != "beta" ||
		!item.BucketStart.Equal(time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected increment: %+v", item)
	}

	// 同一个桶里同一个用户再报，写库时只多一个事件、不多一个用户。
	if _, err := service.RecordLaunchReport(ctx, newHealthReport("u1", launched)); err != nil {
		t.Fatalf("report again: %v", err)
	}
	if err := service.Flush(ctx); err != nil {
		t.Fatalf("flush again: %v", err)
	}
	if got := repository.increments[1][0]; got.Users != 0 || got.Events != 1 {
		t.Fatalf("expected no new user on repeated report, got %+v", got)
	}

	// 写库失败时计数放回内存，下次一起写。
	repository.incrementErr = errors.New("db down")
	if _, err := service.RecordLaunchReport(ctx, newHealthReport("u2", launched)); err != nil {
		t.Fatalf("report u2: %v", err)
	}
	if err := service.Flush(ctx); err == nil {
		t.Fatal("expected flush error")
	}
	repository.incrementErr = nil
	if err := service.Flush(ctx); err != nil {
		t.Fatalf("flush after recovery: %v", err)
	}
	if got := repository.increments[2][0]; got.Users != 1 || got.Events != 1 {
		t.Fatalf("expected retried increment, got %+v", got)
	}

	for _, report := range []domain.LaunchReport{
		{ApplicationSlug: "forge-demo", Environment: "test", Platform: "web", RuntimeVersion: "rt-1", Events: []domain.LaunchReportEvent{launched}},
		{ApplicationSlug: "forge-demo", Environment: "test", Platform: "android", RuntimeVersion: "rt-1"},
		{ApplicationSlug: "forge-demo", Environment: "test", Platform: "android", RuntimeVersion: "rt-1", Channel: "Beta", Events: []domain.LaunchReportEvent{launched}},
		{ApplicationSlug: "forge-demo", Environment: "test", Platform: "android", RuntimeVersion: "rt-1", Events: make([]domain.LaunchReportEvent, domain.MaxLaunchReportEvents+1)},
	} {
		if _, err := service.RecordLaunchReport(ctx, report); !errors.Is(err, domain.ErrInvalidLaunchReport) {
			t.Fatalf("expected invalid report for %+v, got %v", report.Platform, err)
		}
	}
	if _, err := service.RecordLaunchReport(ctx, domain.LaunchReport{ApplicationSlug: "other", Environment: "test", Platform: "android", RuntimeVersion: "rt-1", Events: []domain.LaunchReportEvent{launched}}); !errors.Is(err, domain.ErrArtifactNotFound) {
		t.Fatalf("expected unknown application to be rejected, got %v", err)
	}
}

// 比例灰度变为 unhealthy 时写一次操作记录并自动停用；已经是 unhealthy 时不再处理；全量只告警；拿不到锁时不判定。
func TestReleaseHealthServiceEvaluatesAndStopsGray(t *testing.T) {
	policy := domain.DefaultReleaseHealthPolicy()
	policy.Enabled = true
	gray := domain.ReleaseHealthTarget{ReleaseID: "gray-1", ReleaseType: domain.ReleaseTypeGray, GrayStrategy: domain.ReleaseGrayStrategyPercentage, Policy: policy, BaselineReleaseID: "full-1"}
	full := domain.ReleaseHealthTarget{ReleaseID: "full-2", ReleaseType: domain.ReleaseTypeFull, Policy: policy}
	stillUnhealthy := gray
	stillUnhealthy.ReleaseID, stillUnhealthy.PreviousStatus = "gray-2", domain.ReleaseHealthUnhealthy
	repository := &fakeReleaseHealthRepository{
		targets: []domain.ReleaseHealthTarget{gray, full, stillUnhealthy},
		counts: map[string]domain.LaunchCounts{
			"gray-1": {Launched: 90, LaunchFailed: 10},
			"full-1": {Launched: 99, LaunchFailed: 1},
			"full-2": {Launched: 80, LaunchFailed: 20},
			"gray-2": {Launched: 80, LaunchFailed: 20},
		},
		reported: map[string]bool{"gray-1": true, "full-2": true, "gray-2": true},
	}
	deactivator := &fakeReleaseDeactivator{}
	service := NewReleaseHealthService(repository, deactivator, nil)
	if err := service.Evaluate(context.Background()); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(repository.saved) != 3 || repository.saved[0].Status != domain.ReleaseHealthUnhealthy || *repository.saved[0].BaselineFailureRate != 0.01 {
		t.Fatalf("unexpected saved health: %+v", repository.saved)
	}
	if len(repository.events) != 2 {
		t.Fatalf("expected events for gray-1 and full-2 only, got %v", repository.events)
	}
	if len(deactivator.inputs) != 1 || deactivator.inputs[0].ID != "gray-1" || deactivator.inputs[0].Detail["auto"] != true || deactivator.inputs[0].ActorUserID != "" {
		t.Fatalf("expected only gray-1 to be stopped by the system, got %+v", deactivator.inputs)
	}

	repository.locked, repository.saved = true, nil
	if err := service.Evaluate(context.Background()); err != nil || len(repository.saved) != 0 {
		t.Fatalf("evaluation without the lock must skip, got %v %d", err, len(repository.saved))
	}

	health, err := service.GetReleaseHealth(context.Background(), "not-a-uuid")
	if !errors.Is(err, domain.ErrInvalidReleaseHealthInput) {
		t.Fatalf("expected invalid release id, got %+v %v", health, err)
	}
}
