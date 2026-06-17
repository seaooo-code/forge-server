package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"forge-server/internal/domain"
	"forge-server/internal/service"
)

func TestWorkerRunOnceCompletesBuildRecord(t *testing.T) {
	repository := &fakeWorkerBuildRepository{
		record: domain.BuildRecord{
			ID:          "build-1",
			ModuleID:    "module-profile",
			Environment: "qa",
		},
	}
	builds := service.NewBuildService(repository)
	worker := New(builds, fakeWorkerExecutor{
		completion: domain.BuildRecordCompletion{
			ArtifactKey: "rn-bundles/module-profile/qa/build-1",
			ArtifactURL: "https://cdn.example.com/rn-bundles/module-profile/qa/build-1",
		},
	}, nil, Options{
		ID:            "worker-1",
		LeaseDuration: time.Minute,
		BuildTimeout:  time.Minute,
		PollInterval:  time.Millisecond,
	})

	if err := worker.runOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if repository.completeCalls != 1 || repository.failCalls != 0 {
		t.Fatalf("expected complete only, got complete=%d fail=%d", repository.completeCalls, repository.failCalls)
	}
	if repository.lastWorkerID != "worker-1" {
		t.Fatalf("expected worker id, got %q", repository.lastWorkerID)
	}
}

func TestWorkerRunOnceFailsBuildRecord(t *testing.T) {
	repository := &fakeWorkerBuildRepository{
		record: domain.BuildRecord{
			ID:          "build-1",
			ModuleID:    "module-profile",
			Environment: "qa",
		},
	}
	builds := service.NewBuildService(repository)
	worker := New(builds, fakeWorkerExecutor{
		err: errors.New("docker build failed"),
	}, nil, Options{
		ID:            "worker-1",
		LeaseDuration: time.Minute,
		BuildTimeout:  time.Minute,
		PollInterval:  time.Millisecond,
	})

	if err := worker.runOnce(context.Background()); err != nil {
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
	repository := &fakeWorkerBuildRepository{acquireErr: domain.ErrNoBuildRecord}
	builds := service.NewBuildService(repository)
	worker := New(builds, fakeWorkerExecutor{}, nil, Options{
		ID:            "worker-1",
		LeaseDuration: time.Minute,
	})

	if err := worker.runOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}
}

func TestWorkerLeaseCoversBuildTimeout(t *testing.T) {
	repository := &fakeWorkerBuildRepository{acquireErr: domain.ErrNoBuildRecord}
	builds := service.NewBuildService(repository)
	worker := New(builds, fakeWorkerExecutor{}, nil, Options{
		ID:            "worker-1",
		LeaseDuration: time.Minute,
		BuildTimeout:  2 * time.Minute,
	})

	if worker.leaseDuration != 2*time.Minute {
		t.Fatalf("expected lease to cover build timeout, got %s", worker.leaseDuration)
	}
}

type fakeWorkerBuildRepository struct {
	record             domain.BuildRecord
	acquireErr         error
	completeCalls      int
	failCalls          int
	lastWorkerID       string
	lastFailureMessage string
}

func (r *fakeWorkerBuildRepository) AcquireNextBuildRecord(_ context.Context, workerID string, _ time.Duration) (domain.BuildRecord, error) {
	r.lastWorkerID = workerID
	if r.acquireErr != nil {
		return domain.BuildRecord{}, r.acquireErr
	}
	return r.record, nil
}

func (r *fakeWorkerBuildRepository) CreateBuildRecord(_ context.Context, input domain.BuildRecordInput) (domain.BuildRecord, error) {
	return domain.BuildRecord{ID: "build-1", ModuleID: input.ModuleID, Environment: input.Environment}, nil
}

func (r *fakeWorkerBuildRepository) CompleteBuildRecord(_ context.Context, id string, completion domain.BuildRecordCompletion) (domain.BuildRecord, error) {
	r.completeCalls++
	return domain.BuildRecord{ID: id, ArtifactKey: completion.ArtifactKey, ArtifactURL: completion.ArtifactURL}, nil
}

func (r *fakeWorkerBuildRepository) FailBuildRecord(_ context.Context, id string, message string) (domain.BuildRecord, error) {
	r.failCalls++
	r.lastFailureMessage = message
	return domain.BuildRecord{ID: id, ErrorMessage: message}, nil
}

func (r *fakeWorkerBuildRepository) ListBuildRecords(context.Context) ([]domain.BuildRecord, domain.BuildRecordSummary, error) {
	return nil, domain.BuildRecordSummary{}, nil
}

type fakeWorkerExecutor struct {
	completion domain.BuildRecordCompletion
	err        error
}

func (e fakeWorkerExecutor) Execute(context.Context, domain.BuildRecord) (domain.BuildRecordCompletion, error) {
	if e.err != nil {
		return domain.BuildRecordCompletion{}, e.err
	}
	return e.completion, nil
}
