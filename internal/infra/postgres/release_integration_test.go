package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"forge-server/internal/domain"
)

// 发布全量会结束同一运行时版本下进行中的比例灰度，其他运行时版本的灰度不受影响。
func TestIntegrationFullReleaseEndsPercentageGray(t *testing.T) {
	f := newTestPlanFixture(t)
	ctx := context.Background()
	releases := NewReleaseRepository(f.pool)
	create := func(buildID string, releaseType domain.ReleaseType, percentage int) domain.ReleaseRecord {
		t.Helper()
		input := domain.ReleaseCreateInput{BuildRecordID: buildID, ReleaseType: releaseType, CreatedByUserID: f.userID}
		if releaseType == domain.ReleaseTypeGray {
			input.GrayStrategy = domain.ReleaseGrayStrategyPercentage
			input.GrayPercentage = percentage
		}
		record, _, err := releases.CreateRelease(ctx, input)
		if err != nil {
			t.Fatalf("create %s release for build %s: %v", releaseType, buildID, err)
		}
		return record
	}
	status := func(releaseID string) (string, string) {
		t.Helper()
		var recordStatus, routeStatus string
		if err := f.pool.QueryRow(ctx, `
			select rr.status, route.status
			from release_records rr
			join release_routes route on route.release_id = rr.id
			where rr.id = $1
		`, releaseID).Scan(&recordStatus, &routeStatus); err != nil {
			t.Fatalf("query release %s: %v", releaseID, err)
		}
		return recordStatus, routeStatus
	}

	firstFull, _ := f.build(t, f.feature, "main", "rt-1")
	grayBuild, _ := f.build(t, f.feature, "main", "rt-1")
	otherRuntimeFull, _ := f.build(t, f.feature, "main", "rt-2")
	otherRuntimeGray, _ := f.build(t, f.feature, "main", "rt-2")
	nextFull, _ := f.build(t, f.feature, "main", "rt-1")

	first := create(firstFull, domain.ReleaseTypeFull, 0)
	gray := create(grayBuild, domain.ReleaseTypeGray, 99)
	create(otherRuntimeFull, domain.ReleaseTypeFull, 0)
	otherGray := create(otherRuntimeGray, domain.ReleaseTypeGray, 99)

	// 99% 灰度下，10 个 userId 里至少有一个命中灰度构建（全部落空的概率是 1e-20）。
	userIDs := []string{"1001", "1002", "1003", "1004", "1005", "1006", "1007", "1008", "1009", "1010"}
	inGray := 0
	for _, userID := range userIDs {
		if f.manifestBuild(t, userID, "rt-1", f.feature).BuildRecordID == grayBuild {
			inGray++
		}
	}
	if inGray == 0 {
		t.Fatal("expected some users in the gray bucket before the new full release")
	}

	next := create(nextFull, domain.ReleaseTypeFull, 0)

	for name, id := range map[string]string{"previous full": first.ID, "percentage gray": gray.ID} {
		if record, route := status(id); record != "inactive" || route != "inactive" {
			t.Fatalf("expected %s to become inactive, got record=%s route=%s", name, record, route)
		}
	}
	if record, route := status(next.ID); record != "active" || route != "active" {
		t.Fatalf("expected new full release active, got record=%s route=%s", record, route)
	}
	if record, route := status(otherGray.ID); record != "active" || route != "active" {
		t.Fatalf("expected gray of another runtimeVersion to stay active, got record=%s route=%s", record, route)
	}
	for _, userID := range userIDs {
		if module := f.manifestBuild(t, userID, "rt-1", f.feature); module.BuildRecordID != nextFull {
			t.Fatalf("expected user %s on the new full release, got %+v", userID, module)
		}
	}

	// 灰度结束后可以重新开始一轮比例灰度。
	newGrayBuild, _ := f.build(t, f.feature, "main", "rt-1")
	create(newGrayBuild, domain.ReleaseTypeGray, 10)
}

// 发布时校验 Common 与 Feature 构建的能力：Feature 要求高于 Common 生效发布、Common 降级到不满足生效中的 Feature、
// Feature 声明要求但 Common 没有生效发布，都会被拒绝；没有声明能力的旧构建不受影响。
func TestIntegrationReleaseCapabilityConstraint(t *testing.T) {
	f := newTestPlanFixture(t)
	ctx := context.Background()
	releases := NewReleaseRepository(f.pool)
	build := func(moduleID string, provides string, requires string) string {
		t.Helper()
		id, _ := f.build(t, moduleID, "main", "rt-1")
		if _, err := f.pool.Exec(ctx, `update build_records set provided_capabilities = $2::jsonb, required_capabilities = $3::jsonb where id = $1`, id, provides, requires); err != nil {
			t.Fatalf("set capabilities: %v", err)
		}
		return id
	}
	release := func(buildID string, gray bool) error {
		input := domain.ReleaseCreateInput{BuildRecordID: buildID, ReleaseType: domain.ReleaseTypeFull, CreatedByUserID: f.userID}
		if gray {
			input.ReleaseType, input.GrayStrategy, input.GrayPercentage = domain.ReleaseTypeGray, domain.ReleaseGrayStrategyPercentage, 10
		}
		_, _, err := releases.CreateRelease(ctx, input)
		return err
	}
	conflict := func(err error) *domain.ReleaseCapabilityConflictError {
		t.Helper()
		var target *domain.ReleaseCapabilityConflictError
		if !errors.As(err, &target) {
			t.Fatalf("expected capability conflict, got %v", err)
		}
		return target
	}

	// 没有任何能力声明的旧构建照常发布，Common 还没发布也不拦。
	if err := release(build(f.feature, `{}`, `{}`), false); err != nil {
		t.Fatalf("release legacy feature: %v", err)
	}

	// Feature 声明了要求，但 Common 没有生效发布。
	featureNeeds2 := build(f.feature, `{}`, `{"commonApi": 2}`)
	if got := conflict(release(featureNeeds2, false)); got.Conflicts[0].Reason != domain.ReleaseCapabilityCommonMissing || got.Conflicts[0].ModuleID != f.common {
		t.Fatalf("expected common_missing, got %+v", got.Conflicts)
	}

	if err := release(build(f.common, `{"commonApi": 2}`, `{}`), false); err != nil {
		t.Fatalf("release common level 2: %v", err)
	}
	if err := release(featureNeeds2, false); err != nil {
		t.Fatalf("release feature requiring 2 on common 2: %v", err)
	}

	// Feature 要求高于 Common 全量。
	featureNeeds3 := build(f.feature, `{}`, `{"commonApi": 3}`)
	got := conflict(release(featureNeeds3, true))
	if len(got.Conflicts) != 1 || got.Conflicts[0].Reason != domain.ReleaseCapabilityUnmet || got.Conflicts[0].Shortfalls[0].Required != 3 ||
		*got.Conflicts[0].Shortfalls[0].Provided != 2 {
		t.Fatalf("unexpected conflict: %+v", got.Conflicts)
	}

	// Common 灰度升到 3 后，Feature 仍要兼容 Common 全量（没分到 Common 灰度的用户），所以还是拒绝。
	if err := release(build(f.common, `{"commonApi": 3}`, `{}`), true); err != nil {
		t.Fatalf("release common gray level 3: %v", err)
	}
	if got := conflict(release(featureNeeds3, false)); len(got.Conflicts) != 1 || got.Conflicts[0].ReleaseType != domain.ReleaseTypeFull {
		t.Fatalf("expected conflict with the common full release only, got %+v", got.Conflicts)
	}

	// Common 全量升到 3（同时结束 Common 灰度）后，Feature 可以发布。
	if err := release(build(f.common, `{"commonApi": 3}`, `{}`), false); err != nil {
		t.Fatalf("release common full level 3: %v", err)
	}
	if err := release(featureNeeds3, false); err != nil {
		t.Fatalf("release feature requiring 3: %v", err)
	}

	// Common 回退到 2 会让生效中的 Feature 不满足。
	got = conflict(release(build(f.common, `{"commonApi": 2}`, `{}`), false))
	if len(got.Conflicts) != 1 || got.Conflicts[0].ModuleID != f.feature || got.Conflicts[0].BuildRecordID != featureNeeds3 {
		t.Fatalf("expected conflict with the active feature, got %+v", got.Conflicts)
	}
}

// 发布的每次状态变化都写操作记录；停用后路由失效；上一次全量按同一运行时版本、不同构建查找。
func TestIntegrationReleaseEventsDeactivateAndPreviousFull(t *testing.T) {
	f := newTestPlanFixture(t)
	ctx := context.Background()
	releases := NewReleaseRepository(f.pool)
	create := func(buildID string, gray int) domain.ReleaseRecord {
		t.Helper()
		input := domain.ReleaseCreateInput{BuildRecordID: buildID, ReleaseType: domain.ReleaseTypeFull, CreatedByUserID: f.userID}
		if gray > 0 {
			input.ReleaseType, input.GrayStrategy, input.GrayPercentage = domain.ReleaseTypeGray, domain.ReleaseGrayStrategyPercentage, gray
		}
		record, _, err := releases.CreateRelease(ctx, input)
		if err != nil {
			t.Fatalf("create release: %v", err)
		}
		return record
	}
	kinds := func(releaseID string) []string {
		t.Helper()
		events, err := releases.ListReleaseEvents(ctx, releaseID)
		if err != nil {
			t.Fatalf("list events: %v", err)
		}
		out := []string{}
		for _, event := range events {
			if event.ActorName == "" {
				t.Fatalf("expected actor name on event %+v", event)
			}
			out = append(out, string(event.Kind))
		}
		return out
	}

	buildA, _ := f.build(t, f.feature, "main", "rt-1")
	buildB, _ := f.build(t, f.feature, "main", "rt-1")
	buildC, _ := f.build(t, f.feature, "main", "rt-1")
	a := create(buildA, 0)
	b := create(buildB, 0)
	c := create(buildC, 20)
	if _, _, err := releases.UpdateReleaseGrayPercentage(ctx, domain.ReleaseGrayPercentageUpdateInput{ID: c.ID, GrayPercentage: 40, UpdatedByUserID: f.userID}); err != nil {
		t.Fatalf("update gray: %v", err)
	}
	if _, _, err := releases.UpdateReleaseGrayPercentage(ctx, domain.ReleaseGrayPercentageUpdateInput{ID: c.ID, GrayPercentage: 100, UpdatedByUserID: f.userID}); err != nil {
		t.Fatalf("promote gray: %v", err)
	}

	if got := strings.Join(kinds(a.ID), ","); got != "replaced,created" {
		t.Fatalf("unexpected events of first full release: %s", got)
	}
	if got := strings.Join(kinds(b.ID), ","); got != "replaced,created" {
		t.Fatalf("unexpected events of second full release: %s", got)
	}
	if got := strings.Join(kinds(c.ID), ","); got != "promoted_to_full,gray_percentage_updated,created" {
		t.Fatalf("unexpected events of promoted gray: %s", got)
	}

	previous, err := releases.PreviousFullRelease(ctx, c.ID)
	if err != nil || previous.ID != b.ID {
		t.Fatalf("expected previous full release %s, got %+v %v", b.ID, previous, err)
	}
	if _, err := releases.PreviousFullRelease(ctx, a.ID); !errors.Is(err, domain.ErrReleaseRollbackUnavailable) {
		t.Fatalf("expected no previous full before the first release, got %v", err)
	}

	record, route, err := releases.DeactivateRelease(ctx, domain.ReleaseDeactivateInput{ID: c.ID, Reason: "白屏", ActorUserID: f.userID})
	if err != nil || record.Status != domain.ReleaseStatusInactive || route.Status != domain.ReleaseRouteStatusInactive {
		t.Fatalf("expected deactivated release, got %+v %+v %v", record, route, err)
	}
	if _, _, err := releases.DeactivateRelease(ctx, domain.ReleaseDeactivateInput{ID: c.ID, ActorUserID: f.userID}); !errors.Is(err, domain.ErrReleaseNotActive) {
		t.Fatalf("expected second deactivation to fail, got %v", err)
	}
	if got := kinds(c.ID)[0]; got != "deactivated" {
		t.Fatalf("expected latest event to be deactivated, got %s", got)
	}
	if module := f.manifestBuild(t, "1001", "rt-1", f.feature); module.BuildRecordID != "" {
		t.Fatalf("expected no release after deactivating the only full release, got %+v", module)
	}
}

// 发布记录带上所用构建的构建号、分支和提交详情，没有提交详情的旧构建对应字段为空。
func TestIntegrationReleaseRecordIncludesBuildCommit(t *testing.T) {
	f := newTestPlanFixture(t)
	ctx := context.Background()
	releases := NewReleaseRepository(f.pool)

	legacyBuild, _ := f.build(t, f.feature, "main", "rt-1")
	detailedBuild, detailedNumber := f.build(t, f.feature, "feature/login", "rt-1")
	if _, err := f.pool.Exec(ctx, `
		update build_records set commit_title = '新增登录页', commit_author_name = 'Chen', committed_at = '2026-11-25T06:30:00Z' where id = $1
	`, detailedBuild); err != nil {
		t.Fatalf("set commit details: %v", err)
	}

	legacy, _, err := releases.CreateRelease(ctx, domain.ReleaseCreateInput{BuildRecordID: legacyBuild, ReleaseType: domain.ReleaseTypeFull, CreatedByUserID: f.userID})
	if err != nil {
		t.Fatalf("create legacy release: %v", err)
	}
	if legacy.Build.CommitSHA == "" || legacy.Build.CommitTitle != "" || legacy.Build.CommittedAt != nil {
		t.Fatalf("unexpected legacy build summary: %+v", legacy.Build)
	}
	detailed, _, err := releases.CreateRelease(ctx, domain.ReleaseCreateInput{BuildRecordID: detailedBuild, ReleaseType: domain.ReleaseTypeFull, CreatedByUserID: f.userID})
	if err != nil {
		t.Fatalf("create detailed release: %v", err)
	}
	listed, _, err := releases.ListReleases(ctx, domain.ReleaseRecordFilters{ModuleID: f.feature, Pagination: domain.Pagination{Page: 1, PageSize: 20}})
	if err != nil {
		t.Fatalf("list releases: %v", err)
	}
	var found *domain.ReleaseRecord
	for index := range listed {
		if listed[index].ID == detailed.ID {
			found = &listed[index]
		}
	}
	if found == nil {
		t.Fatalf("detailed release missing from list: %+v", listed)
	}
	build := found.Build
	if build.Number != detailedNumber || build.Branch != "feature/login" || build.CommitTitle != "新增登录页" || build.CommitAuthorName != "Chen" ||
		build.CommittedAt == nil || !build.CommittedAt.Equal(time.Date(2026, 11, 25, 6, 30, 0, 0, time.UTC)) {
		t.Fatalf("unexpected build summary: %+v", build)
	}
}
