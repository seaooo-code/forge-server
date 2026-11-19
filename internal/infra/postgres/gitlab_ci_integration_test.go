package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"forge-server/internal/domain"
)

func resetBuildQueue(t *testing.T, ctx context.Context, repository *BuildRepository) {
	t.Helper()
	// 测试库里可能有其他测试遗留的 pending 构建，先清掉，保证领取到的是本测试创建的构建。
	if _, err := repository.pool.Exec(ctx, `update build_records set status = 'failed', lease_expires_at = null where status in ('pending', 'running')`); err != nil {
		t.Fatalf("reset queue: %v", err)
	}
}

// gitlab_ci 模块：构建记录带出 job 和产物路径；worker 按构建方式领取；pipeline 只有持有者能写，并且放回队列后保留。
func TestIntegrationGitLabCIBuildLifecycle(t *testing.T) {
	pool := newIntegrationPool(t)
	ctx := context.Background()
	moduleID, environment := seedIntegrationModule(t, pool)
	repository := NewBuildRepository(pool)
	resetBuildQueue(t, ctx, repository)

	if _, err := pool.Exec(ctx, `update modules set build_mode = 'gitlab_ci', ci_job = 'forge_build', ci_artifact_path = 'dist' where id = $1`, moduleID); err != nil {
		t.Fatalf("switch module to gitlab_ci: %v", err)
	}
	created, err := repository.CreateBuildRecord(ctx, domain.BuildRecordInput{ModuleID: moduleID, Environment: environment, TriggerType: domain.BuildTriggerTypeManual})
	if err != nil {
		t.Fatalf("create build: %v", err)
	}

	// 只处理 docker 构建的 worker 领不到 gitlab_ci 构建。
	if _, err := repository.AcquireNextBuildRecord(ctx, "docker-worker", time.Minute, []domain.BuildMode{domain.BuildModeDockerfile, domain.BuildModeAdapter}); !errors.Is(err, domain.ErrNoBuildRecord) {
		t.Fatalf("expected no build for a docker-only worker, got %v", err)
	}

	acquired, err := repository.AcquireNextBuildRecord(ctx, "ci-worker", time.Minute, []domain.BuildMode{domain.BuildModeGitLabCI})
	if err != nil || acquired.ID != created.ID {
		t.Fatalf("acquire by ci worker: id=%s err=%v", acquired.ID, err)
	}
	if acquired.BuildMode != domain.BuildModeGitLabCI || acquired.CIJob != "forge_build" || acquired.CIArtifactPath != "dist" || acquired.WorkerID != "ci-worker" {
		t.Fatalf("unexpected acquired record: mode=%s job=%q path=%q worker=%q", acquired.BuildMode, acquired.CIJob, acquired.CIArtifactPath, acquired.WorkerID)
	}
	if acquired.CIPipelineID != 0 || acquired.CIPipelineURL != "" {
		t.Fatalf("expected no pipeline before it is created, got %d %q", acquired.CIPipelineID, acquired.CIPipelineURL)
	}

	if err := repository.RecordBuildCIPipeline(ctx, created.ID, "other-worker", 42, "https://gitlab.example.com/p/42"); !errors.Is(err, domain.ErrBuildLeaseLost) {
		t.Fatalf("expected a non-holder to be rejected, got %v", err)
	}
	if err := repository.RecordBuildCIPipeline(ctx, created.ID, "ci-worker", 42, "https://gitlab.example.com/p/42"); err != nil {
		t.Fatalf("record pipeline by holder: %v", err)
	}

	// 放回队列后 pipeline 保留，下一个持有者据此续接。
	if err := repository.RequeueBuildRecord(ctx, created.ID, "ci-worker"); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	resumed, err := repository.AcquireNextBuildRecord(ctx, "ci-worker-2", time.Minute, []domain.BuildMode{domain.BuildModeGitLabCI})
	if err != nil || resumed.ID != created.ID {
		t.Fatalf("acquire after requeue: id=%s err=%v", resumed.ID, err)
	}
	if resumed.CIPipelineID != 42 || resumed.CIPipelineURL != "https://gitlab.example.com/p/42" {
		t.Fatalf("expected the recorded pipeline to survive a requeue, got %d %q", resumed.CIPipelineID, resumed.CIPipelineURL)
	}

	if err := repository.RecordBuildCIPipeline(ctx, created.ID, "ci-worker", 43, "stale"); !errors.Is(err, domain.ErrBuildLeaseLost) {
		t.Fatalf("expected the previous holder to be rejected, got %v", err)
	}
}

func TestIntegrationModuleGitLabCIConfigAndConstraints(t *testing.T) {
	pool := newIntegrationPool(t)
	ctx := context.Background()
	moduleID, _ := seedIntegrationModule(t, pool)

	if _, err := pool.Exec(ctx, `update modules set build_mode = 'gitlab_ci', ci_job = 'forge_build', ci_artifact_path = 'out' where id = $1`, moduleID); err != nil {
		t.Fatalf("switch module to gitlab_ci: %v", err)
	}
	module, err := NewModuleRepository(pool).GetModule(ctx, moduleID)
	if err != nil {
		t.Fatalf("get module: %v", err)
	}
	if module.BuildConfig.BuildMode != domain.BuildModeGitLabCI || module.BuildConfig.CIJob != "forge_build" || module.BuildConfig.CIArtifactPath != "out" {
		t.Fatalf("unexpected build config %#v", module.BuildConfig)
	}

	for name, statement := range map[string]string{
		"gitlab_ci without job":      `update modules set ci_job = '' where id = $1`,
		"gitlab_ci without path":     `update modules set ci_artifact_path = '' where id = $1`,
		"dockerfile mode with a job": `update modules set build_mode = 'dockerfile' where id = $1`,
	} {
		if _, err := pool.Exec(ctx, statement, moduleID); err == nil || !isCheckViolation(err) {
			t.Fatalf("%s: expected a check violation, got %v", name, err)
		}
	}
}
