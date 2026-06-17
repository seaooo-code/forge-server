package worker

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"forge-server/internal/domain"
	"forge-server/internal/service"
)

type BuildExecutor interface {
	Execute(ctx context.Context, record domain.BuildRecord) (domain.BuildRecordCompletion, error)
}

type Worker struct {
	builds        *service.BuildService
	executor      BuildExecutor
	id            string
	leaseDuration time.Duration
	pollInterval  time.Duration
	buildTimeout  time.Duration
	logger        *slog.Logger
}

type Options struct {
	BuildTimeout  time.Duration
	ID            string
	LeaseDuration time.Duration
	PollInterval  time.Duration
}

func New(builds *service.BuildService, executor BuildExecutor, logger *slog.Logger, opts Options) *Worker {
	id := strings.TrimSpace(opts.ID)
	if id == "" {
		id = defaultWorkerID()
	}
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	leaseDuration := opts.LeaseDuration
	if leaseDuration <= 0 {
		leaseDuration = 30 * time.Minute
	}
	buildTimeout := opts.BuildTimeout
	if buildTimeout <= 0 {
		buildTimeout = leaseDuration
	}
	if leaseDuration < buildTimeout {
		leaseDuration = buildTimeout
	}
	return &Worker{
		builds:        builds,
		executor:      executor,
		id:            id,
		leaseDuration: leaseDuration,
		pollInterval:  pollInterval,
		buildTimeout:  buildTimeout,
		logger:        logger,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	if w.builds == nil || w.executor == nil {
		return domain.ErrInvalidBuildInput
	}

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		if err := w.runOnce(ctx); err != nil && w.logger != nil {
			w.logger.Error("run build worker iteration failed", "error", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (w *Worker) runOnce(ctx context.Context) error {
	record, err := w.builds.AcquireNextBuildRecord(ctx, w.id, w.leaseDuration)
	if err != nil {
		if errors.Is(err, domain.ErrNoBuildRecord) {
			return nil
		}
		return err
	}

	if w.logger != nil {
		w.logger.Info("build record acquired", "build", record.ID, "module", record.ModuleID, "environment", record.Environment)
	}

	buildCtx, cancel := context.WithTimeout(ctx, w.buildTimeout)
	defer cancel()

	completion, err := w.executor.Execute(buildCtx, record)
	if err != nil {
		if _, failErr := w.builds.FailBuildRecord(context.WithoutCancel(ctx), record.ID, err.Error()); failErr != nil {
			return errors.Join(err, failErr)
		}
		if w.logger != nil {
			w.logger.Error("build record failed", "build", record.ID, "error", err)
		}
		return nil
	}

	if _, err := w.builds.CompleteBuildRecord(context.WithoutCancel(ctx), record.ID, completion); err != nil {
		return err
	}
	if w.logger != nil {
		w.logger.Info("build record completed", "build", record.ID, "artifactKey", completion.ArtifactKey)
	}
	return nil
}

func defaultWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return "forge-worker"
	}
	return "forge-worker-" + hostname
}
