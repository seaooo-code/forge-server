package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"forge-server/internal/domain"
)

func TestBuildServiceCreateBuildRecordNormalizesInput(t *testing.T) {
	repository := fakeBuildRecordRepository{}
	service := NewBuildService(&repository)

	_, err := service.CreateBuildRecord(context.Background(), domain.BuildRecordInput{
		ModuleID:    " module-profile ",
		Environment: " qa ",
		Branch:      " release/1.2.0 ",
		TriggerID:   " pipeline-main ",
		TriggerType: " pipeline ",
	})
	if err != nil {
		t.Fatalf("create build record: %v", err)
	}

	if repository.lastCreateInput.ModuleID != "module-profile" ||
		repository.lastCreateInput.Environment != "qa" ||
		repository.lastCreateInput.Branch != "release/1.2.0" ||
		repository.lastCreateInput.TriggerID != "pipeline-main" ||
		repository.lastCreateInput.TriggerType != domain.BuildTriggerTypePipeline {
		t.Fatalf("unexpected normalized input: %+v", repository.lastCreateInput)
	}
}

func TestBuildServiceCreateBuildRecordDefaultsTriggerTypeToManual(t *testing.T) {
	repository := fakeBuildRecordRepository{}
	service := NewBuildService(&repository)

	_, err := service.CreateBuildRecord(context.Background(), domain.BuildRecordInput{
		ModuleID:    "module-profile",
		Environment: "qa",
	})
	if err != nil {
		t.Fatalf("create build record: %v", err)
	}

	if repository.lastCreateInput.TriggerType != domain.BuildTriggerTypeManual || repository.lastCreateInput.TriggerID != "" {
		t.Fatalf("expected manual trigger default, got %+v", repository.lastCreateInput)
	}
}

func TestBuildServiceCreateBuildRecordRejectsInvalidInput(t *testing.T) {
	repository := fakeBuildRecordRepository{}
	service := NewBuildService(&repository)

	_, err := service.CreateBuildRecord(context.Background(), domain.BuildRecordInput{
		ModuleID:    "bad module",
		Environment: "qa",
	})
	if !errors.Is(err, domain.ErrInvalidBuildInput) {
		t.Fatalf("expected invalid build input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d", repository.createCalls)
	}
}

func TestBuildServiceCreateBuildRecordRejectsPipelineWithoutTriggerID(t *testing.T) {
	repository := fakeBuildRecordRepository{}
	service := NewBuildService(&repository)

	_, err := service.CreateBuildRecord(context.Background(), domain.BuildRecordInput{
		ModuleID:    "module-profile",
		Environment: "qa",
		TriggerType: domain.BuildTriggerTypePipeline,
	})
	if !errors.Is(err, domain.ErrInvalidBuildInput) {
		t.Fatalf("expected invalid build input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d", repository.createCalls)
	}
}

func TestBuildServiceAcquireRequiresWorkerAndLease(t *testing.T) {
	repository := fakeBuildRecordRepository{}
	service := NewBuildService(&repository)

	_, err := service.AcquireNextBuildRecord(context.Background(), " ", time.Minute)
	if !errors.Is(err, domain.ErrInvalidBuildInput) {
		t.Fatalf("expected invalid build input, got %v", err)
	}

	_, err = service.AcquireNextBuildRecord(context.Background(), "worker-1", 0)
	if !errors.Is(err, domain.ErrInvalidBuildInput) {
		t.Fatalf("expected invalid build input, got %v", err)
	}
}

func TestBuildServiceCompleteRequiresArtifactKey(t *testing.T) {
	repository := fakeBuildRecordRepository{}
	service := NewBuildService(&repository)

	_, err := service.CompleteBuildRecord(context.Background(), "build-1", domain.BuildRecordCompletion{})
	if !errors.Is(err, domain.ErrInvalidBuildInput) {
		t.Fatalf("expected invalid build input, got %v", err)
	}
}

func TestBuildServiceListBuildRecords(t *testing.T) {
	repository := fakeBuildRecordRepository{
		records: []domain.BuildRecord{{ID: "build-1"}},
		summary: domain.BuildRecordSummary{Total: 1, Succeeded: 1},
	}
	service := NewBuildService(&repository)

	records, summary, err := service.ListBuildRecords(context.Background())
	if err != nil {
		t.Fatalf("list build records: %v", err)
	}
	if len(records) != 1 || records[0].ID != "build-1" || summary.Total != 1 {
		t.Fatalf("unexpected list result: records=%+v summary=%+v", records, summary)
	}
}

func TestBuildServiceGetBuildRecord(t *testing.T) {
	repository := fakeBuildRecordRepository{
		record: domain.BuildRecord{ID: "build-1"},
	}
	service := NewBuildService(&repository)

	record, err := service.GetBuildRecord(context.Background(), " build-1 ")
	if err != nil {
		t.Fatalf("get build record: %v", err)
	}
	if record.ID != "build-1" || repository.lastGetID != "build-1" {
		t.Fatalf("unexpected get result: record=%+v id=%q", record, repository.lastGetID)
	}
}

func TestBuildServiceGetBuildRecordRejectsEmptyID(t *testing.T) {
	repository := fakeBuildRecordRepository{}
	service := NewBuildService(&repository)

	_, err := service.GetBuildRecord(context.Background(), " ")
	if !errors.Is(err, domain.ErrBuildRecordNotFound) {
		t.Fatalf("expected build record not found, got %v", err)
	}
	if repository.getCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d", repository.getCalls)
	}
}

type fakeBuildRecordRepository struct {
	createCalls     int
	getCalls        int
	lastCreateInput domain.BuildRecordInput
	lastGetID       string
	record          domain.BuildRecord
	records         []domain.BuildRecord
	summary         domain.BuildRecordSummary
}

func (r *fakeBuildRecordRepository) CreateBuildRecord(_ context.Context, input domain.BuildRecordInput) (domain.BuildRecord, error) {
	r.createCalls++
	r.lastCreateInput = input
	return domain.BuildRecord{
		ID:          "build-1",
		ModuleID:    input.ModuleID,
		Environment: input.Environment,
		Branch:      input.Branch,
		TriggerID:   input.TriggerID,
		TriggerType: input.TriggerType,
	}, nil
}

func (r *fakeBuildRecordRepository) AcquireNextBuildRecord(context.Context, string, time.Duration) (domain.BuildRecord, error) {
	return domain.BuildRecord{ID: "build-1"}, nil
}

func (r *fakeBuildRecordRepository) CompleteBuildRecord(_ context.Context, id string, completion domain.BuildRecordCompletion) (domain.BuildRecord, error) {
	return domain.BuildRecord{ID: id, ArtifactKey: completion.ArtifactKey, ArtifactURL: completion.ArtifactURL}, nil
}

func (r *fakeBuildRecordRepository) FailBuildRecord(_ context.Context, id string, message string) (domain.BuildRecord, error) {
	return domain.BuildRecord{ID: id, ErrorMessage: message}, nil
}

func (r *fakeBuildRecordRepository) GetBuildRecord(_ context.Context, id string) (domain.BuildRecord, error) {
	r.getCalls++
	r.lastGetID = id
	return r.record, nil
}

func (r *fakeBuildRecordRepository) ListBuildRecords(context.Context) ([]domain.BuildRecord, domain.BuildRecordSummary, error) {
	return r.records, r.summary, nil
}
