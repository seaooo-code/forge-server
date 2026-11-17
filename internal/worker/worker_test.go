package worker

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"forge-server/internal/domain"
	"forge-server/internal/service"
)

func TestWorkerRunOnceCompletesBuildRecord(t *testing.T) {
	repository := newFakeWorkerBuildRepository(testWorkerBuildRecord("build-1"))
	worker := newTestWorker(repository, fakeWorkerExecutor{
		completion: domain.BuildRecordCompletion{ArtifactKey: "rn-bundles/module-profile/qa/build-1"},
	}, Options{})

	acquired, err := worker.runOnce(context.Background(), context.Background(), "worker-1")
	if err != nil || !acquired {
		t.Fatalf("run once: acquired=%v err=%v", acquired, err)
	}
	if repository.completeCalls != 1 || repository.failCalls != 0 {
		t.Fatalf("expected complete only, got complete=%d fail=%d", repository.completeCalls, repository.failCalls)
	}
	if !slices.Equal(repository.writeHolders, []string{"worker-1"}) || !slices.Equal(repository.acquireHolders, []string{"worker-1"}) {
		t.Fatalf("expected holder worker-1 for acquire and write, got acquire=%v write=%v", repository.acquireHolders, repository.writeHolders)
	}
}

func TestWorkerRunOnceFailsBuildRecord(t *testing.T) {
	repository := newFakeWorkerBuildRepository(testWorkerBuildRecord("build-1"))
	worker := newTestWorker(repository, fakeWorkerExecutor{err: errors.New("docker build failed")}, Options{})

	if _, err := worker.runOnce(context.Background(), context.Background(), "worker-1"); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if repository.completeCalls != 0 || repository.failCalls != 1 {
		t.Fatalf("expected fail only, got complete=%d fail=%d", repository.completeCalls, repository.failCalls)
	}
	if repository.lastFailureMessage != "docker build failed" {
		t.Fatalf("expected failure message, got %q", repository.lastFailureMessage)
	}
}

func TestWorkerRunOnceIgnoresEmptyQueue(t *testing.T) {
	repository := newFakeWorkerBuildRepository()
	worker := newTestWorker(repository, fakeWorkerExecutor{}, Options{})

	acquired, err := worker.runOnce(context.Background(), context.Background(), "worker-1")
	if err != nil || acquired {
		t.Fatalf("expected empty queue to be ignored, got acquired=%v err=%v", acquired, err)
	}
}

// 续租返回 ErrBuildLeaseLost 时，构建上下文被取消，结果不写回。
func TestWorkerStopsBuildWhenLeaseLost(t *testing.T) {
	repository := newFakeWorkerBuildRepository(testWorkerBuildRecord("build-1"))
	repository.renewErr = domain.ErrBuildLeaseLost
	executor := blockingWorkerExecutor{}
	worker := newTestWorker(repository, executor, Options{LeaseDuration: 30 * time.Millisecond})

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := worker.runOnce(context.Background(), context.Background(), "worker-1"); err != nil {
			t.Errorf("run once: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("build was not stopped after lease lost")
	}
	if repository.completeCalls != 0 || repository.failCalls != 0 || repository.requeueCalls != 0 {
		t.Fatalf("expected no write after lease lost, got complete=%d fail=%d requeue=%d", repository.completeCalls, repository.failCalls, repository.requeueCalls)
	}
}

// 写回时发现租约已被接管（ErrBuildLeaseLost）不算 worker 错误。
func TestWorkerIgnoresLeaseLostOnComplete(t *testing.T) {
	repository := newFakeWorkerBuildRepository(testWorkerBuildRecord("build-1"))
	repository.writeErr = domain.ErrBuildLeaseLost
	worker := newTestWorker(repository, fakeWorkerExecutor{
		completion: domain.BuildRecordCompletion{ArtifactKey: "rn-bundles/build-1"},
	}, Options{})

	if _, err := worker.runOnce(context.Background(), context.Background(), "worker-1"); err != nil {
		t.Fatalf("expected lease lost to be ignored, got %v", err)
	}
}

func TestWorkerRenewsLeaseWhileBuilding(t *testing.T) {
	repository := newFakeWorkerBuildRepository(testWorkerBuildRecord("build-1"))
	worker := newTestWorker(repository, fakeWorkerExecutor{
		completion: domain.BuildRecordCompletion{ArtifactKey: "rn-bundles/build-1"},
		delay:      100 * time.Millisecond,
	}, Options{LeaseDuration: 30 * time.Millisecond})

	if _, err := worker.runOnce(context.Background(), context.Background(), "worker-1"); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if repository.renewCount() < 2 {
		t.Fatalf("expected lease renewed during build, got %d renewals", repository.renewCount())
	}
}

// 收到退出信号后不再领取新构建，等进行中的构建完成并写回结果。
func TestWorkerRunDrainsRunningBuildOnShutdown(t *testing.T) {
	repository := newFakeWorkerBuildRepository(testWorkerBuildRecord("build-1"), testWorkerBuildRecord("build-2"))
	started := make(chan struct{}, 1)
	worker := newTestWorker(repository, fakeWorkerExecutor{
		completion: domain.BuildRecordCompletion{ArtifactKey: "rn-bundles/build"},
		delay:      100 * time.Millisecond,
		started:    started,
	}, Options{ShutdownGrace: time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- worker.Run(ctx) }()
	<-started
	cancel()

	if err := <-runDone; err != nil {
		t.Fatalf("run: %v", err)
	}
	if repository.completeCalls != 1 || repository.requeueCalls != 0 {
		t.Fatalf("expected running build completed, got complete=%d requeue=%d", repository.completeCalls, repository.requeueCalls)
	}
	if repository.remaining() != 1 {
		t.Fatalf("expected build-2 left in queue, got %d remaining", repository.remaining())
	}
}

// 宽限期结束仍未完成的构建被中断并放回队列，不标记失败。
func TestWorkerRunRequeuesBuildAfterShutdownGrace(t *testing.T) {
	repository := newFakeWorkerBuildRepository(testWorkerBuildRecord("build-1"))
	started := make(chan struct{}, 1)
	worker := newTestWorker(repository, blockingWorkerExecutor{started: started}, Options{ShutdownGrace: 50 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- worker.Run(ctx) }()
	<-started
	cancel()

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after shutdown grace")
	}
	if repository.requeueCalls != 1 || repository.failCalls != 0 || repository.completeCalls != 0 {
		t.Fatalf("expected requeue only, got requeue=%d fail=%d complete=%d", repository.requeueCalls, repository.failCalls, repository.completeCalls)
	}
}

// 并发大于 1 时多个槽位同时执行，每个槽位用 {id}#{序号} 作为持有者。
func TestWorkerRunsSlotsConcurrently(t *testing.T) {
	repository := newFakeWorkerBuildRepository(testWorkerBuildRecord("build-1"), testWorkerBuildRecord("build-2"))
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	worker := newTestWorker(repository, fakeWorkerExecutor{
		completion: domain.BuildRecordCompletion{ArtifactKey: "rn-bundles/build"},
		started:    started,
		release:    release,
	}, Options{Concurrency: 2})

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- worker.Run(ctx) }()
	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("expected two builds running at the same time")
		}
	}
	close(release)
	cancel()
	if err := <-runDone; err != nil {
		t.Fatalf("run: %v", err)
	}

	holders := repository.acquiredHolders()
	slices.Sort(holders)
	if !slices.Equal(holders, []string{"worker-1#0", "worker-1#1"}) {
		t.Fatalf("expected per-slot holders, got %v", holders)
	}
	if repository.completeCalls != 2 {
		t.Fatalf("expected two completed builds, got %d", repository.completeCalls)
	}
}

func newTestWorker(repository *fakeWorkerBuildRepository, executor BuildExecutor, opts Options) *Worker {
	opts.ID = "worker-1"
	if opts.PollInterval == 0 {
		opts.PollInterval = 5 * time.Millisecond
	}
	if opts.BuildTimeout == 0 {
		opts.BuildTimeout = time.Minute
	}
	return New(service.NewBuildService(repository), executor, nil, opts)
}

func testWorkerBuildRecord(id string) domain.BuildRecord {
	return domain.BuildRecord{ID: id, ModuleID: "module-profile", Environment: "qa"}
}

type fakeWorkerBuildRepository struct {
	mu                 sync.Mutex
	queue              []domain.BuildRecord
	acquireHolders     []string
	writeHolders       []string
	renewErr           error
	writeErr           error
	renewals           int
	completeCalls      int
	failCalls          int
	requeueCalls       int
	lastFailureMessage string
}

func newFakeWorkerBuildRepository(records ...domain.BuildRecord) *fakeWorkerBuildRepository {
	return &fakeWorkerBuildRepository{queue: records}
}

func (r *fakeWorkerBuildRepository) remaining() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queue)
}

func (r *fakeWorkerBuildRepository) renewCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.renewals
}

func (r *fakeWorkerBuildRepository) acquiredHolders() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.acquireHolders)
}

func (r *fakeWorkerBuildRepository) AcquireNextBuildRecord(_ context.Context, workerID string, _ time.Duration) (domain.BuildRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.queue) == 0 {
		return domain.BuildRecord{}, domain.ErrNoBuildRecord
	}
	record := r.queue[0]
	r.queue = r.queue[1:]
	r.acquireHolders = append(r.acquireHolders, workerID)
	return record, nil
}

func (r *fakeWorkerBuildRepository) CreateBuildRecord(_ context.Context, input domain.BuildRecordInput) (domain.BuildRecord, error) {
	return domain.BuildRecord{ID: "build-1", ModuleID: input.ModuleID, Environment: input.Environment}, nil
}

func (r *fakeWorkerBuildRepository) CompleteBuildRecord(_ context.Context, id string, workerID string, completion domain.BuildRecordCompletion) (domain.BuildRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.completeCalls++
	r.writeHolders = append(r.writeHolders, workerID)
	if r.writeErr != nil {
		return domain.BuildRecord{}, r.writeErr
	}
	return domain.BuildRecord{ID: id, ArtifactKey: completion.ArtifactKey}, nil
}

func (r *fakeWorkerBuildRepository) FailBuildRecord(_ context.Context, id string, workerID string, message string) (domain.BuildRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failCalls++
	r.writeHolders = append(r.writeHolders, workerID)
	r.lastFailureMessage = message
	if r.writeErr != nil {
		return domain.BuildRecord{}, r.writeErr
	}
	return domain.BuildRecord{ID: id, ErrorMessage: message}, nil
}

func (r *fakeWorkerBuildRepository) RenewBuildLease(context.Context, string, string, time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renewals++
	return r.renewErr
}

func (r *fakeWorkerBuildRepository) RequeueBuildRecord(_ context.Context, _ string, workerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requeueCalls++
	r.writeHolders = append(r.writeHolders, workerID)
	return r.writeErr
}

func (r *fakeWorkerBuildRepository) GetBuildQueueStats(context.Context) (domain.BuildQueueStats, error) {
	return domain.BuildQueueStats{}, nil
}

func (r *fakeWorkerBuildRepository) GetBuildRecord(context.Context, string) (domain.BuildRecord, error) {
	return domain.BuildRecord{}, domain.ErrBuildRecordNotFound
}

func (r *fakeWorkerBuildRepository) ListBuildRecords(context.Context, domain.BuildRecordFilters) ([]domain.BuildRecord, domain.BuildRecordSummary, error) {
	return nil, domain.BuildRecordSummary{}, nil
}

// fakeWorkerExecutor 按配置返回结果；started 非空时开始执行就发信号，release 非空时等它关闭再返回。
type fakeWorkerExecutor struct {
	completion domain.BuildRecordCompletion
	err        error
	delay      time.Duration
	started    chan<- struct{}
	release    <-chan struct{}
}

func (e fakeWorkerExecutor) Execute(context.Context, domain.BuildRecord) (domain.BuildRecordCompletion, error) {
	if e.started != nil {
		e.started <- struct{}{}
	}
	if e.release != nil {
		<-e.release
	}
	time.Sleep(e.delay)
	if e.err != nil {
		return domain.BuildRecordCompletion{}, e.err
	}
	return e.completion, nil
}

// blockingWorkerExecutor 一直执行到构建上下文被取消。
type blockingWorkerExecutor struct {
	started chan<- struct{}
}

func (e blockingWorkerExecutor) Execute(ctx context.Context, _ domain.BuildRecord) (domain.BuildRecordCompletion, error) {
	if e.started != nil {
		e.started <- struct{}{}
	}
	<-ctx.Done()
	return domain.BuildRecordCompletion{}, ctx.Err()
}
