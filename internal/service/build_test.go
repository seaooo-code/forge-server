package service

import (
	"context"
	"errors"
	"strings"
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
		CommitSHA:   " abc123 ",
		TriggerID:   " delivery-main ",
		TriggerType: " webhook ",
	})
	if err != nil {
		t.Fatalf("create build record: %v", err)
	}

	if repository.lastCreateInput.ModuleID != "module-profile" ||
		repository.lastCreateInput.Environment != "qa" ||
		repository.lastCreateInput.Branch != "release/1.2.0" ||
		repository.lastCreateInput.CommitSHA != "abc123" ||
		repository.lastCreateInput.TriggerID != "delivery-main" ||
		repository.lastCreateInput.TriggerType != domain.BuildTriggerTypeWebhook {
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

func TestBuildServiceCreateBuildRecordRejectsWebhookWithoutCommit(t *testing.T) {
	repository := fakeBuildRecordRepository{}
	service := NewBuildService(&repository)

	_, err := service.CreateBuildRecord(context.Background(), domain.BuildRecordInput{
		ModuleID:    "module-profile",
		Environment: "qa",
		Branch:      "main",
		TriggerType: domain.BuildTriggerTypeWebhook,
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

func TestBuildServiceCompleteNormalizesManifestSummary(t *testing.T) {
	repository := fakeBuildRecordRepository{}
	service := NewBuildService(&repository)

	_, err := service.CompleteBuildRecord(context.Background(), "build-1", domain.BuildRecordCompletion{
		ArtifactKey:    " /rn-bundles/app/module/qa/build-1/ ",
		ArtifactURL:    " https://cdn.example.com/build-1 ",
		ManifestKey:    " /rn-bundles/app/module/qa/build-1/manifest.json ",
		FileCount:      2,
		TotalSizeBytes: 12,
	})
	if err != nil {
		t.Fatalf("complete build record: %v", err)
	}

	if repository.lastCompletion.ArtifactKey != "rn-bundles/app/module/qa/build-1" ||
		repository.lastCompletion.ManifestKey != "rn-bundles/app/module/qa/build-1/manifest.json" ||
		repository.lastCompletion.FileCount != 2 ||
		repository.lastCompletion.TotalSizeBytes != 12 {
		t.Fatalf("unexpected completion: %+v", repository.lastCompletion)
	}
}

func TestBuildServiceListBuildRecords(t *testing.T) {
	repository := fakeBuildRecordRepository{
		records: []domain.BuildRecord{{ID: "build-1"}},
		summary: domain.BuildRecordSummary{Total: 1, Succeeded: 1},
	}
	service := NewBuildService(&repository)

	records, summary, err := service.ListBuildRecords(context.Background(), domain.BuildRecordFilters{
		ApplicationID: " app-main ",
		Environment:   " qa ",
		ModuleID:      " module-profile ",
		Status:        " pending ",
		TriggerType:   " webhook ",
	})
	if err != nil {
		t.Fatalf("list build records: %v", err)
	}
	if len(records) != 1 || records[0].ID != "build-1" || summary.Total != 1 {
		t.Fatalf("unexpected list result: records=%+v summary=%+v", records, summary)
	}
	if repository.lastListFilters.ApplicationID != "app-main" ||
		repository.lastListFilters.Environment != "qa" ||
		repository.lastListFilters.ModuleID != "module-profile" ||
		repository.lastListFilters.Status != domain.BuildStatusPending ||
		repository.lastListFilters.TriggerType != domain.BuildTriggerTypeWebhook {
		t.Fatalf("unexpected filters: %+v", repository.lastListFilters)
	}
}

func TestBuildServiceListBuildRecordsRejectsInvalidFilters(t *testing.T) {
	tests := []domain.BuildRecordFilters{
		{ModuleID: "bad module"},
		{Status: "queued"},
		{TriggerType: "cron"},
	}

	for _, filters := range tests {
		repository := fakeBuildRecordRepository{}
		service := NewBuildService(&repository)

		_, _, err := service.ListBuildRecords(context.Background(), filters)
		if !errors.Is(err, domain.ErrInvalidBuildInput) {
			t.Fatalf("expected invalid build input for %+v, got %v", filters, err)
		}
		if repository.listCalls != 0 {
			t.Fatalf("expected repository not to be called for %+v, got %d", filters, repository.listCalls)
		}
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

func TestBuildServiceGetBuildRecordBuildsArtifactTree(t *testing.T) {
	repository := fakeBuildRecordRepository{
		record: domain.BuildRecord{
			ID:             "build-1",
			ManifestKey:    "rn-bundles/app/module/qa/build-1/manifest.json",
			FileCount:      2,
			TotalSizeBytes: 12,
		},
	}
	reader := fakeArtifactManifestReader{
		manifest: domain.ArtifactManifest{
			FileCount:      2,
			TotalSizeBytes: 12,
			Files: []domain.ArtifactManifestFile{
				{
					Path:        "index.bundle",
					ObjectKey:   "rn-bundles/app/module/qa/build-1/index.bundle",
					SizeBytes:   6,
					ContentType: "application/javascript",
					SHA256:      "hash-1",
				},
				{
					Path:        "assets/images/icon.png",
					ObjectKey:   "rn-bundles/app/module/qa/build-1/assets/images/icon.png",
					SizeBytes:   6,
					ContentType: "image/png",
					SHA256:      "hash-2",
				},
			},
		},
	}
	service := NewBuildServiceWithManifestReader(&repository, &reader)

	record, err := service.GetBuildRecord(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build record: %v", err)
	}

	if record.ArtifactTree.FileCount != 2 || record.ArtifactTree.TotalSizeBytes != 12 {
		t.Fatalf("unexpected artifact tree summary: %+v", record.ArtifactTree)
	}
	if len(record.ArtifactTree.Nodes) != 2 || record.ArtifactTree.Nodes[0].Name != "assets" || record.ArtifactTree.Nodes[1].Name != "index.bundle" {
		t.Fatalf("unexpected artifact tree nodes: %+v", record.ArtifactTree.Nodes)
	}
	assets := record.ArtifactTree.Nodes[0]
	if assets.SizeBytes != 6 || len(assets.Children) != 1 || assets.Children[0].Name != "images" {
		t.Fatalf("unexpected nested directory: %+v", assets)
	}
	if repository.lastGetID != "build-1" || reader.calls != 1 {
		t.Fatalf("expected repository and reader calls, got id=%q calls=%d", repository.lastGetID, reader.calls)
	}
}

func TestBuildServiceGetBuildRecordKeepsEmptyTreeWhenManifestReadFails(t *testing.T) {
	repository := fakeBuildRecordRepository{
		record: domain.BuildRecord{
			ID:             "build-1",
			ManifestKey:    "rn-bundles/app/module/qa/build-1/manifest.json",
			FileCount:      2,
			TotalSizeBytes: 12,
		},
	}
	reader := fakeArtifactManifestReader{err: errors.New("read failed")}
	service := NewBuildServiceWithManifestReader(&repository, &reader)

	record, err := service.GetBuildRecord(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build record: %v", err)
	}
	if record.ArtifactTree.FileCount != 2 || record.ArtifactTree.TotalSizeBytes != 12 || len(record.ArtifactTree.Nodes) != 0 {
		t.Fatalf("expected empty tree with summary, got %+v", record.ArtifactTree)
	}
}

type fakeBuildRecordRepository struct {
	createCalls     int
	getCalls        int
	listCalls       int
	lastCreateInput domain.BuildRecordInput
	lastCompletion  domain.BuildRecordCompletion
	lastGetID       string
	lastListFilters domain.BuildRecordFilters
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
	r.lastCompletion = completion
	return domain.BuildRecord{
		ID:             id,
		ArtifactKey:    completion.ArtifactKey,
		ArtifactURL:    completion.ArtifactURL,
		ManifestKey:    completion.ManifestKey,
		FileCount:      completion.FileCount,
		TotalSizeBytes: completion.TotalSizeBytes,
	}, nil
}

func (r *fakeBuildRecordRepository) FailBuildRecord(_ context.Context, id string, message string) (domain.BuildRecord, error) {
	return domain.BuildRecord{ID: id, ErrorMessage: message}, nil
}

func (r *fakeBuildRecordRepository) GetBuildRecord(_ context.Context, id string) (domain.BuildRecord, error) {
	r.getCalls++
	r.lastGetID = id
	return r.record, nil
}

func (r *fakeBuildRecordRepository) ListBuildRecords(_ context.Context, filters domain.BuildRecordFilters) ([]domain.BuildRecord, domain.BuildRecordSummary, error) {
	r.listCalls++
	r.lastListFilters = filters
	return r.records, r.summary, nil
}

type fakeArtifactManifestReader struct {
	calls    int
	err      error
	manifest domain.ArtifactManifest
}

func (r *fakeArtifactManifestReader) ReadArtifactManifest(context.Context, domain.BuildRecord) (domain.ArtifactManifest, error) {
	r.calls++
	if r.err != nil {
		return domain.ArtifactManifest{}, r.err
	}
	return r.manifest, nil
}

func TestBuildServiceCompleteBuildRecordNormalizesBuildContract(t *testing.T) {
	repository := fakeBuildRecordRepository{}
	service := NewBuildService(&repository)

	_, err := service.CompleteBuildRecord(context.Background(), "build-1", domain.BuildRecordCompletion{
		ArtifactKey:    "rn-bundles/app/module/qa/build-1",
		CommitSHA:      " abc123 ",
		RuntimeVersion: " 1.2.0-android.725 ",
		Platforms:      []string{"android", "ios", "android"},
	})
	if err != nil {
		t.Fatalf("complete build record: %v", err)
	}
	completion := repository.lastCompletion
	if completion.CommitSHA != "abc123" || completion.RuntimeVersion != "1.2.0-android.725" ||
		len(completion.Platforms) != 2 || completion.Platforms[0] != "android" || completion.Platforms[1] != "ios" {
		t.Fatalf("unexpected completion: %+v", completion)
	}

	for _, invalid := range []domain.BuildRecordCompletion{
		{ArtifactKey: "key", RuntimeVersion: "1.0 beta"},
		{ArtifactKey: "key", RuntimeVersion: strings.Repeat("a", 65)},
		{ArtifactKey: "key", RuntimeVersion: "1.0", Platforms: []string{"web"}},
	} {
		if _, err := service.CompleteBuildRecord(context.Background(), "build-1", invalid); !errors.Is(err, domain.ErrInvalidBuildInput) {
			t.Fatalf("expected invalid build input for %+v, got %v", invalid, err)
		}
	}
}
