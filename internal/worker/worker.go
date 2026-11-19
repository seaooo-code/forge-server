package worker

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"forge-server/internal/domain"
	"forge-server/internal/service"
)

const (
	defaultLeaseDuration = 2 * time.Minute
	defaultShutdownGrace = 10 * time.Minute
	// leaseRenewTimeout 是单次续租请求的超时，续租失败（非 ErrBuildLeaseLost）时等下一次心跳重试。
	leaseRenewTimeout = 10 * time.Second
)

var (
	// errBuildLeaseLost 是构建上下文的取消原因：租约已被其他 worker 接管，当前执行结果作废。
	errBuildLeaseLost = errors.New("构建租约已失效，已被其他 worker 接管")
	// errWorkerShutdown 是构建上下文的取消原因：worker 退出宽限期已到，构建会被放回队列。
	errWorkerShutdown = errors.New("worker 退出，构建被中断")
)

type BuildExecutor interface {
	Execute(ctx context.Context, record domain.BuildRecord) (domain.BuildRecordCompletion, error)
}

type Worker struct {
	builds        *service.BuildService
	executor      BuildExecutor
	id            string
	concurrency   int
	leaseDuration time.Duration
	pollInterval  time.Duration
	buildTimeout  time.Duration
	shutdownGrace time.Duration
	modes         []domain.BuildMode
	logger        *slog.Logger
}

type Options struct {
	BuildTimeout time.Duration
	// Concurrency 是同时执行的构建数，默认 1。
	Concurrency int
	ID          string
	// LeaseDuration 是单次租约时长。执行期间每 LeaseDuration/3 续租一次，worker 异常退出后最多 LeaseDuration 就会被其他 worker 接管。
	LeaseDuration time.Duration
	PollInterval  time.Duration
	// ShutdownGrace 是收到退出信号后等待进行中构建完成的时长，超时后中断构建并放回队列。
	ShutdownGrace time.Duration
	// Modes 是本 worker 处理的构建方式，为空时处理全部。只处理 gitlab_ci 的 worker 不需要 Docker 和 git。
	Modes []domain.BuildMode
}

func New(builds *service.BuildService, executor BuildExecutor, logger *slog.Logger, opts Options) *Worker {
	id := strings.TrimSpace(opts.ID)
	if id == "" {
		id = defaultWorkerID()
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	leaseDuration := opts.LeaseDuration
	if leaseDuration <= 0 {
		leaseDuration = defaultLeaseDuration
	}
	buildTimeout := opts.BuildTimeout
	if buildTimeout <= 0 {
		buildTimeout = 30 * time.Minute
	}
	shutdownGrace := opts.ShutdownGrace
	if shutdownGrace <= 0 {
		shutdownGrace = defaultShutdownGrace
	}
	return &Worker{
		builds:        builds,
		executor:      executor,
		id:            id,
		concurrency:   concurrency,
		leaseDuration: leaseDuration,
		pollInterval:  pollInterval,
		buildTimeout:  buildTimeout,
		shutdownGrace: shutdownGrace,
		modes:         opts.Modes,
		logger:        logger,
	}
}

// Run 启动 concurrency 个执行槽位，每个槽位独立领取并执行构建，直到 ctx 结束。
// ctx 结束后不再领取新构建，等待进行中的构建最多 shutdownGrace；超时后中断它们并放回队列，再返回。
func (w *Worker) Run(ctx context.Context) error {
	if w.builds == nil || w.executor == nil {
		return domain.ErrInvalidBuildInput
	}

	// 构建上下文不随 ctx 取消，只在宽限期结束时以 errWorkerShutdown 取消。
	buildsCtx, cancelBuilds := context.WithCancelCause(context.WithoutCancel(ctx))
	defer cancelBuilds(nil)

	var slots sync.WaitGroup
	for slot := range w.concurrency {
		slots.Add(1)
		go func() {
			defer slots.Done()
			w.runSlot(ctx, buildsCtx, slot)
		}()
	}

	<-ctx.Done()
	done := make(chan struct{})
	go func() {
		slots.Wait()
		close(done)
	}()
	w.log(slog.LevelInfo, "worker draining", "grace", w.shutdownGrace.String())

	timer := time.NewTimer(w.shutdownGrace)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		w.log(slog.LevelWarn, "worker shutdown grace expired, interrupting builds")
		cancelBuilds(errWorkerShutdown)
		<-done
	}
	return nil
}

func (w *Worker) runSlot(ctx context.Context, buildsCtx context.Context, slot int) {
	holder := w.slotHolderID(slot)
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		acquired, err := w.runOnce(ctx, buildsCtx, holder)
		if err != nil {
			w.log(slog.LevelError, "run build worker iteration failed", "holder", holder, "error", err)
		}
		if ctx.Err() != nil {
			return
		}
		// 刚执行完一个构建时立即尝试领取下一个，队列空闲时才等待轮询间隔。
		if acquired {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// slotHolderID 是槽位写入 build_records.worker_id 的持有者 ID。并发大于 1 时每个槽位带 #序号，
// 保证同一进程内另一个槽位接管了过期租约时，原槽位的写回仍会被归属校验拦下。
func (w *Worker) slotHolderID(slot int) string {
	if w.concurrency == 1 {
		return w.id
	}
	return w.id + "#" + strconv.Itoa(slot)
}

// runOnce 领取并执行一个构建，返回是否领到了构建。acquireCtx 结束后不再领取；buildsCtx 控制执行中的构建；
// holder 是写入 worker_id 的持有者 ID。
func (w *Worker) runOnce(acquireCtx context.Context, buildsCtx context.Context, holder string) (bool, error) {
	if acquireCtx.Err() != nil {
		return false, nil
	}
	record, err := w.builds.AcquireNextBuildRecord(acquireCtx, holder, w.leaseDuration, w.modes...)
	if err != nil {
		if errors.Is(err, domain.ErrNoBuildRecord) || acquireCtx.Err() != nil {
			return false, nil
		}
		return false, err
	}
	w.log(slog.LevelInfo, "build record acquired", "holder", holder, "build", record.ID, "module", record.ModuleID, "environment", record.Environment)

	leaseCtx, cancelLease := context.WithCancelCause(buildsCtx)
	defer cancelLease(nil)
	buildCtx, cancelTimeout := context.WithTimeout(leaseCtx, w.buildTimeout)
	defer cancelTimeout()

	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		w.keepLease(buildCtx, record.ID, holder, cancelLease)
	}()
	completion, execErr := w.executor.Execute(buildCtx, record)
	cancelTimeout()
	<-heartbeatDone

	// 状态写回不受构建上下文取消影响。
	writeCtx := context.WithoutCancel(buildsCtx)
	switch cause := context.Cause(leaseCtx); {
	case execErr == nil:
		if _, err := w.builds.CompleteBuildRecord(writeCtx, record.ID, holder, completion); err != nil {
			return true, w.ignoreLeaseLost(record.ID, err)
		}
		w.log(slog.LevelInfo, "build record completed", "build", record.ID, "artifactKey", completion.ArtifactKey)
	case errors.Is(cause, errBuildLeaseLost):
		w.log(slog.LevelWarn, "build lease lost, result discarded", "build", record.ID, "error", execErr)
	case errors.Is(cause, errWorkerShutdown):
		if err := w.builds.RequeueBuildRecord(writeCtx, record.ID, holder); err != nil {
			return true, w.ignoreLeaseLost(record.ID, err)
		}
		w.log(slog.LevelWarn, "build interrupted by shutdown, requeued", "build", record.ID)
	default:
		if _, err := w.builds.FailBuildRecord(writeCtx, record.ID, holder, execErr.Error()); err != nil {
			return true, errors.Join(execErr, w.ignoreLeaseLost(record.ID, err))
		}
		w.log(slog.LevelError, "build record failed", "build", record.ID, "error", execErr)
	}
	return true, nil
}

// keepLease 每 leaseDuration/3 续租一次，直到 ctx 结束。租约被接管时以 errBuildLeaseLost 取消构建。
func (w *Worker) keepLease(ctx context.Context, buildID string, holder string, cancel context.CancelCauseFunc) {
	ticker := time.NewTicker(w.leaseDuration / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		renewCtx, cancelRenew := context.WithTimeout(ctx, leaseRenewTimeout)
		err := w.builds.RenewBuildLease(renewCtx, buildID, holder, w.leaseDuration)
		cancelRenew()
		switch {
		case err == nil:
		case errors.Is(err, domain.ErrBuildLeaseLost):
			cancel(errBuildLeaseLost)
			return
		case ctx.Err() != nil:
			return
		default:
			// 暂时性错误（如数据库抖动）：租约还没过期，下一次心跳重试。
			w.log(slog.LevelWarn, "renew build lease failed", "build", buildID, "error", err)
		}
	}
}

// ignoreLeaseLost 把 ErrBuildLeaseLost 降级为警告：构建已由其他 worker 接管，当前结果不再写回。
func (w *Worker) ignoreLeaseLost(buildID string, err error) error {
	if errors.Is(err, domain.ErrBuildLeaseLost) {
		w.log(slog.LevelWarn, "build lease lost before writing result", "build", buildID)
		return nil
	}
	return err
}

func (w *Worker) log(level slog.Level, msg string, args ...any) {
	if w.logger != nil {
		w.logger.Log(context.Background(), level, msg, append([]any{"worker", w.id}, args...)...)
	}
}

func defaultWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return "forge-worker"
	}
	return "forge-worker-" + hostname
}
