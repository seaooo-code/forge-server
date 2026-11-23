package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"forge-server/internal/domain"
)

type fakeArtifactPullStatsRepository struct {
	err       error
	written   []domain.ArtifactPullStat
	listInput domain.ArtifactPullStatsFilters
}

func (r *fakeArtifactPullStatsRepository) IncrementArtifactPullStats(_ context.Context, stats []domain.ArtifactPullStat) error {
	if r.err != nil {
		return r.err
	}
	r.written = append(r.written, stats...)
	return nil
}

func (r *fakeArtifactPullStatsRepository) ListArtifactPullStats(_ context.Context, filters domain.ArtifactPullStatsFilters) ([]domain.ArtifactPullStatsRow, error) {
	r.listInput = filters
	return []domain.ArtifactPullStatsRow{}, nil
}

func (r *fakeArtifactPullStatsRepository) DeleteArtifactPullStatsBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func TestArtifactPullStatsAggregatesAndRetriesFailedFlush(t *testing.T) {
	repository := &fakeArtifactPullStatsRepository{err: errors.New("db down")}
	service := NewArtifactPullStatsService(repository, nil)
	service.now = func() time.Time { return time.Date(2026, 11, 23, 23, 30, 0, 0, time.Local) }
	manifest := domain.ArtifactApplicationManifest{
		Application:    domain.ArtifactApplication{ID: "app-1"},
		Environment:    domain.ArtifactEnvironment{ID: "staging"},
		Platform:       "android",
		RuntimeVersion: "725-n1-m24",
		Modules:        []domain.ArtifactModuleManifest{{ID: "common", Source: domain.ArtifactSourceFull, BuildRecordID: "build-1"}},
		MissingModules: []domain.ArtifactMissingModule{{ID: "claw", Reason: domain.ArtifactMissingReasonCommonIncompatible}},
	}
	service.RecordManifest(manifest)
	service.RecordManifest(manifest)

	if err := service.Flush(context.Background()); err == nil {
		t.Fatal("expected flush error")
	}
	repository.err = nil
	service.RecordManifest(manifest)
	if err := service.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}

	got := map[string]int64{}
	for _, stat := range repository.written {
		if !stat.Day.Equal(time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)) || stat.ApplicationID != "app-1" || stat.Platform != "android" {
			t.Fatalf("unexpected stat key: %+v", stat)
		}
		got[stat.ModuleID+"|"+stat.Outcome+"|"+stat.BuildRecordID] = stat.Requests
	}
	if len(got) != 2 || got["common|full|build-1"] != 3 || got["claw|missing:common_incompatible|"] != 3 {
		t.Fatalf("expected failed batch to be kept and merged, got %v", got)
	}
	if err := service.Flush(context.Background()); err != nil || len(repository.written) != 2 {
		t.Fatalf("expected empty flush to write nothing, got %d rows, %v", len(repository.written), err)
	}
}

func TestArtifactPullStatsListValidatesFilters(t *testing.T) {
	repository := &fakeArtifactPullStatsRepository{}
	service := NewArtifactPullStatsService(repository, nil)
	service.now = func() time.Time { return time.Date(2026, 11, 23, 10, 0, 0, 0, time.Local) }
	for name, filters := range map[string]domain.ArtifactPullStatsFilters{
		"no scope":         {Environment: "staging"},
		"too many days":    {ApplicationID: "app-1", Days: domain.MaxArtifactPullStatsQueryDays + 1},
		"negative days":    {ApplicationID: "app-1", Days: -1},
		"invalid build id": {ApplicationID: "app-1", BuildRecordID: "build-1"},
	} {
		if _, _, _, err := service.ListArtifactPullStats(context.Background(), filters); !errors.Is(err, domain.ErrInvalidArtifactPullStatsInput) {
			t.Fatalf("%s: expected invalid input, got %v", name, err)
		}
	}
	_, from, to, err := service.ListArtifactPullStats(context.Background(), domain.ArtifactPullStatsFilters{ApplicationID: " app-1 "})
	if err != nil || from.Format("2006-01-02") != "2026-11-17" || to.Format("2006-01-02") != "2026-11-23" ||
		repository.listInput.ApplicationID != "app-1" || !repository.listInput.From.Equal(from) {
		t.Fatalf("expected default 7 days, got %s..%s %+v %v", from, to, repository.listInput, err)
	}
}
