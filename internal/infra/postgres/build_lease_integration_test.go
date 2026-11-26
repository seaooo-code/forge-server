package postgres

import (
	"errors"
	"testing"
	"time"

	"forge-server/internal/domain"
)

// 租约归属：只有当前持有者能续租、写回结果或放回队列；租约过期被接管后，原持有者的写入一律返回 ErrBuildLeaseLost。
func TestIntegrationBuildLeaseFencing(t *testing.T) {
	pool := newIntegrationPool(t)
	ctx := integrationCtx()
	moduleID, environment := seedIntegrationModule(t, pool)
	repository := NewBuildRepository(pool)

	// 测试库里可能有其他测试遗留的 pending 构建，先清掉，保证领取到的是本测试创建的构建。
	if _, err := pool.Exec(ctx, `update build_records set status = 'failed', lease_expires_at = null where status in ('pending', 'running')`); err != nil {
		t.Fatalf("reset queue: %v", err)
	}
	created, err := repository.CreateBuildRecord(ctx, domain.BuildRecordInput{ModuleID: moduleID, Environment: environment, TriggerType: domain.BuildTriggerTypeManual})
	if err != nil {
		t.Fatalf("create build: %v", err)
	}

	acquired, err := repository.AcquireNextBuildRecord(ctx, "worker-a", time.Minute, domain.BuildModes)
	if err != nil || acquired.ID != created.ID {
		t.Fatalf("acquire: id=%s err=%v", acquired.ID, err)
	}
	if err := repository.RenewBuildLease(ctx, created.ID, "worker-a", time.Minute); err != nil {
		t.Fatalf("renew by holder: %v", err)
	}
	if err := repository.RenewBuildLease(ctx, created.ID, "worker-b", time.Minute); !errors.Is(err, domain.ErrBuildLeaseLost) {
		t.Fatalf("expected renew by other worker to be rejected, got %v", err)
	}

	// 模拟 worker-a 心跳中断、租约过期，worker-b 接管。
	if _, err := pool.Exec(ctx, `update build_records set lease_expires_at = now() - interval '1 second' where id = $1`, created.ID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	stats, err := repository.GetBuildQueueStats(ctx)
	if err != nil || stats.Waiting != 1 || stats.Running != 0 {
		t.Fatalf("expected expired lease counted as waiting, got %+v err=%v", stats, err)
	}
	if taken, err := repository.AcquireNextBuildRecord(ctx, "worker-b#1", time.Minute, domain.BuildModes); err != nil || taken.ID != created.ID {
		t.Fatalf("take over: id=%s err=%v", taken.ID, err)
	}
	stats, err = repository.GetBuildQueueStats(ctx)
	if err != nil || stats.Waiting != 0 || stats.Running != 1 || stats.ActiveWorkers != 1 {
		t.Fatalf("expected one running build on one worker, got %+v err=%v", stats, err)
	}

	committedAt := time.Date(2026, 11, 25, 6, 30, 0, 0, time.UTC)
	completion := domain.BuildRecordCompletion{CommitSHA: "3a9e000000000000000000000000000000000000", CommitTitle: "新增登录页", CommitAuthorName: "Chen", CommittedAt: &committedAt, RuntimeVersion: "1.0.0", ArtifactKey: "k/" + created.ID, ManifestKey: "k/" + created.ID + "/manifest.json", Platforms: []string{"android"}}
	if _, err := repository.CompleteBuildRecord(ctx, created.ID, "worker-a", completion); !errors.Is(err, domain.ErrBuildLeaseLost) {
		t.Fatalf("expected stale complete rejected, got %v", err)
	}
	if _, err := repository.FailBuildRecord(ctx, created.ID, "worker-a", "stale"); !errors.Is(err, domain.ErrBuildLeaseLost) {
		t.Fatalf("expected stale fail rejected, got %v", err)
	}
	if err := repository.RequeueBuildRecord(ctx, created.ID, "worker-a"); !errors.Is(err, domain.ErrBuildLeaseLost) {
		t.Fatalf("expected stale requeue rejected, got %v", err)
	}

	// 新持有者中断后放回队列：回到 pending，queued_at 不变，worker_id 清空。
	if err := repository.RequeueBuildRecord(ctx, created.ID, "worker-b#1"); err != nil {
		t.Fatalf("requeue by holder: %v", err)
	}
	requeued, err := repository.GetBuildRecord(ctx, created.ID)
	if err != nil {
		t.Fatalf("get build: %v", err)
	}
	if requeued.Status != domain.BuildStatusPending || requeued.WorkerID != "" || !requeued.QueuedAt.Equal(created.QueuedAt) {
		t.Fatalf("unexpected requeued build: status=%s worker=%q queuedAt=%s (was %s)", requeued.Status, requeued.WorkerID, requeued.QueuedAt, created.QueuedAt)
	}

	if _, err := repository.AcquireNextBuildRecord(ctx, "worker-c", time.Minute, domain.BuildModes); err != nil {
		t.Fatalf("acquire after requeue: %v", err)
	}
	record, err := repository.CompleteBuildRecord(ctx, created.ID, "worker-c", completion)
	if err != nil || record.Status != domain.BuildStatusSucceeded {
		t.Fatalf("complete by holder: status=%s err=%v", record.Status, err)
	}
	if record.CommitTitle != "新增登录页" || record.CommitAuthorName != "Chen" || record.CommittedAt == nil || !record.CommittedAt.Equal(committedAt) {
		t.Fatalf("expected commit details saved with completion, got title=%q author=%q at=%v", record.CommitTitle, record.CommitAuthorName, record.CommittedAt)
	}
	if err := repository.RenewBuildLease(ctx, created.ID, "worker-c", time.Minute); !errors.Is(err, domain.ErrBuildLeaseLost) {
		t.Fatalf("expected renew after completion rejected, got %v", err)
	}
}
