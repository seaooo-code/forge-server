package postgres

import (
	"context"
	"testing"
	"time"

	"forge-server/internal/domain"
)

func TestIntegrationArtifactPullStatsIncrementListAndDelete(t *testing.T) {
	f := newTestPlanFixture(t)
	ctx := context.Background()
	stats := NewArtifactPullStatsRepository(f.pool)
	buildID, number := f.build(t, f.feature, "main", "rt-1")
	today := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	key := domain.ArtifactPullStatKey{Day: today, ApplicationID: f.application, Environment: f.environment, ModuleID: f.feature,
		RuntimeVersion: "rt-1", Platform: "android", Outcome: "full", BuildRecordID: buildID}
	missing := key
	missing.ModuleID, missing.Outcome, missing.BuildRecordID = f.common, "missing:no_release_for_runtime", ""
	old := key
	old.Day = today.AddDate(0, 0, -100)

	for range 2 {
		if err := stats.IncrementArtifactPullStats(ctx, []domain.ArtifactPullStat{{ArtifactPullStatKey: key, Requests: 3}, {ArtifactPullStatKey: missing, Requests: 1}, {ArtifactPullStatKey: old, Requests: 9}}); err != nil {
			t.Fatalf("increment: %v", err)
		}
	}

	rows, err := stats.ListArtifactPullStats(ctx, domain.ArtifactPullStatsFilters{ApplicationID: f.application, From: today.AddDate(0, 0, -6)})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := map[string]domain.ArtifactPullStatsRow{}
	for _, row := range rows {
		got[row.ModuleID+"|"+row.Outcome] = row
	}
	if len(rows) != 2 || got[f.feature+"|full"].Requests != 6 || got[f.feature+"|full"].BuildNumber != number || got[f.feature+"|full"].ModuleName != "feature" ||
		got[f.common+"|missing:no_release_for_runtime"].Requests != 2 || got[f.common+"|missing:no_release_for_runtime"].BuildNumber != 0 {
		t.Fatalf("unexpected rows: %+v", rows)
	}

	if rows, err := stats.ListArtifactPullStats(ctx, domain.ArtifactPullStatsFilters{ApplicationID: f.application, From: today.AddDate(0, 0, -6), BuildRecordID: buildID}); err != nil || len(rows) != 1 {
		t.Fatalf("expected filter by build, got %+v %v", rows, err)
	}
	deleted, err := stats.DeleteArtifactPullStatsBefore(ctx, today.AddDate(0, 0, -domain.ArtifactPullStatsRetentionDays))
	if err != nil || deleted < 1 {
		t.Fatalf("expected the 100-day-old row to be deleted, got %d %v", deleted, err)
	}
}
