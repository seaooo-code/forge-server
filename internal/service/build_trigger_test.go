package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"forge-server/internal/domain"
)

func TestMatchBranchPattern(t *testing.T) {
	cases := []struct {
		pattern string
		branch  string
		want    bool
	}{
		{"main", "main", true},
		{"main", "main2", false},
		{"feature/*", "feature/login", true},
		{"feature/*", "feature/login/v2", false},
		{"feature/*", "feature", false},
		{"release-*", "release-1.2", true},
		{"*", "main", true},
		{"*", "feature/login", false},
		{"**", "feature/login/v2", true},
		{"release/**", "release/1.0/hotfix", true},
		{"release/**", "release", true},
		{"release/**", "hotfix/1.0", false},
		{"**/hotfix", "release/1.0/hotfix", true},
		{"**/hotfix", "hotfix", true},
		{"feature/**/ui", "feature/a/b/ui", true},
		{"feature/**/ui", "feature/a/b/api", false},
		{"[", "[", false},
		{"", "main", false},
	}
	for _, tc := range cases {
		if got := MatchBranchPattern(tc.pattern, tc.branch); got != tc.want {
			t.Errorf("MatchBranchPattern(%q, %q) = %v, want %v", tc.pattern, tc.branch, got, tc.want)
		}
	}
}

func TestBuildTriggerCreatesWebhookBuildsForMatchingModules(t *testing.T) {
	modules := &fakeAutoBuildModuleRepository{modules: []domain.Module{
		testAutoBuildModule("module-feature", []string{"feature/*"}, "qa"),
		testAutoBuildModule("module-main", []string{"main"}, "qa"),
		testAutoBuildModule("module-no-branch", nil, "qa"),
		testAutoBuildModule("module-no-env", []string{"feature/*"}, ""),
		testAutoBuildModule("module-any", []string{"**"}, "staging"),
	}}
	builds := newFakeWebhookBuildRepository()
	trigger := newTestBuildTriggerService(modules, builds, nil)

	result, err := trigger.HandlePushEvent(context.Background(), testPushEvent("feature/login", "abc123"))
	if err != nil {
		t.Fatalf("handle push event: %v", err)
	}
	if len(result.CreatedBuildIDs) != 2 || len(result.DuplicateBuildIDs) != 0 || !result.Matched() {
		t.Fatalf("expected two created builds, got %+v", result)
	}
	if modules.lastConnectionID != "conn-1" || modules.lastRepositoryID != "repo-42" {
		t.Fatalf("unexpected module lookup: %q %q", modules.lastConnectionID, modules.lastRepositoryID)
	}
	first := builds.inputs[0]
	if first.ModuleID != "module-feature" || first.Environment != "qa" || first.Branch != "feature/login" ||
		first.CommitSHA != "abc123" || first.TriggerType != domain.BuildTriggerTypeWebhook || first.TriggerID != "delivery-1" ||
		first.CreatedByUserID != "user-owner" {
		t.Fatalf("unexpected webhook build input: %+v", first)
	}
	if builds.inputs[1].ModuleID != "module-any" || builds.inputs[1].Environment != "staging" {
		t.Fatalf("unexpected second build input: %+v", builds.inputs[1])
	}
}

func TestBuildTriggerSkipsDuplicateCommit(t *testing.T) {
	modules := &fakeAutoBuildModuleRepository{modules: []domain.Module{
		testAutoBuildModule("module-feature", []string{"feature/*"}, "qa"),
	}}
	builds := newFakeWebhookBuildRepository()
	trigger := newTestBuildTriggerService(modules, builds, nil)

	first, err := trigger.HandlePushEvent(context.Background(), testPushEvent("feature/login", "abc123"))
	if err != nil || len(first.CreatedBuildIDs) != 1 {
		t.Fatalf("expected first delivery to create build, got %+v, %v", first, err)
	}
	second, err := trigger.HandlePushEvent(context.Background(), testPushEvent("feature/login", "abc123"))
	if err != nil {
		t.Fatalf("handle redelivered push event: %v", err)
	}
	if len(second.CreatedBuildIDs) != 0 || len(second.DuplicateBuildIDs) != 1 || second.DuplicateBuildIDs[0] != first.CreatedBuildIDs[0] || !second.Matched() {
		t.Fatalf("expected redelivery to be skipped as duplicate, got %+v", second)
	}
	if builds.created != 1 {
		t.Fatalf("expected one build to be created, got %d", builds.created)
	}
}

func TestBuildTriggerIgnoresUnmatchedPush(t *testing.T) {
	modules := &fakeAutoBuildModuleRepository{modules: []domain.Module{
		testAutoBuildModule("module-main", []string{"main", "release/*"}, "qa"),
	}}
	builds := newFakeWebhookBuildRepository()
	trigger := newTestBuildTriggerService(modules, builds, nil)

	result, err := trigger.HandlePushEvent(context.Background(), testPushEvent("feature/login", "abc123"))
	if err != nil || result.Matched() || builds.created != 0 {
		t.Fatalf("expected unmatched push to be ignored, got %+v, %v, created=%d", result, err, builds.created)
	}

	result, err = trigger.HandlePushEvent(context.Background(), testPushEvent("main", ""))
	if err != nil || result.Matched() {
		t.Fatalf("expected push without commit to be ignored, got %+v, %v", result, err)
	}
}

func TestBuildTriggerReturnsErrorOnlyWhenAllMatchedModulesFail(t *testing.T) {
	modules := &fakeAutoBuildModuleRepository{modules: []domain.Module{
		testAutoBuildModule("module-broken", []string{"main"}, "qa"),
		testAutoBuildModule("module-ok", []string{"main"}, "qa"),
	}}
	builds := newFakeWebhookBuildRepository()
	builds.failModules = map[string]error{"module-broken": errors.New("db down")}
	trigger := newTestBuildTriggerService(modules, builds, nil)

	result, err := trigger.HandlePushEvent(context.Background(), testPushEvent("main", "abc123"))
	if err != nil || len(result.CreatedBuildIDs) != 1 {
		t.Fatalf("expected partial success without error, got %+v, %v", result, err)
	}

	builds.failModules["module-ok"] = errors.New("db down")
	if _, err := trigger.HandlePushEvent(context.Background(), testPushEvent("main", "def456")); err == nil {
		t.Fatal("expected error when every matched module fails")
	}
}

func TestBuildTriggerCreatesPreviewReleaseAfterWebhookBuild(t *testing.T) {
	module := testAutoBuildModule("module-feature", []string{"feature/*"}, "qa")
	module.BuildConfig.AutoPreviewRelease = true
	module.BuildConfig.AutoPreviewTTLHours = 48
	modules := &fakeAutoBuildModuleRepository{modules: []domain.Module{module}}
	releases := &fakePreviewReleaseCreator{}
	trigger := newTestBuildTriggerService(modules, newFakeWebhookBuildRepository(), releases)
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	trigger.now = func() time.Time { return now }

	trigger.HandleBuildSucceeded(context.Background(), testSucceededWebhookBuild())

	if len(releases.inputs) != 1 {
		t.Fatalf("expected one preview release, got %d", len(releases.inputs))
	}
	input := releases.inputs[0]
	if input.BuildRecordID != "build-1" ||
		input.ReleaseType != domain.ReleaseTypeGray ||
		input.GrayStrategy != domain.ReleaseGrayStrategyTargeted ||
		input.Identifier != "feature/login" ||
		input.GrayPercentage != 0 ||
		input.CreatedByUserID != "user-owner" ||
		input.ExpiresAt == nil || !input.ExpiresAt.Equal(now.Add(48*time.Hour)) {
		t.Fatalf("unexpected preview release input: %+v", input)
	}
	// 预览发布参数必须能通过发布服务的校验。
	if _, err := validateReleaseCreateInput(input); err != nil {
		t.Fatalf("preview release input should be valid: %v", err)
	}
}

func TestBuildTriggerSkipsPreviewReleaseWhenNotApplicable(t *testing.T) {
	enabled := testAutoBuildModule("module-feature", []string{"feature/*"}, "qa")
	enabled.BuildConfig.AutoPreviewRelease = true
	disabled := testAutoBuildModule("module-feature", []string{"feature/*"}, "qa")

	manual := testSucceededWebhookBuild()
	manual.TriggerType = domain.BuildTriggerTypeManual
	noCreator := testSucceededWebhookBuild()
	noCreator.CreatedByUserID = ""
	for name, tc := range map[string]struct {
		module domain.Module
		record domain.BuildRecord
	}{
		"manual build":     {module: enabled, record: manual},
		"preview disabled": {module: disabled, record: testSucceededWebhookBuild()},
		"build no creator": {module: enabled, record: noCreator},
	} {
		releases := &fakePreviewReleaseCreator{}
		trigger := newTestBuildTriggerService(&fakeAutoBuildModuleRepository{modules: []domain.Module{tc.module}}, newFakeWebhookBuildRepository(), releases)
		trigger.HandleBuildSucceeded(context.Background(), tc.record)
		if len(releases.inputs) != 0 {
			t.Fatalf("%s: expected no preview release, got %+v", name, releases.inputs)
		}
	}
}

func TestBuildServiceCompleteInvokesSucceededHookAndIgnoresPreviewFailure(t *testing.T) {
	module := testAutoBuildModule("module-feature", []string{"feature/*"}, "qa")
	module.BuildConfig.AutoPreviewRelease = true
	releases := &fakePreviewReleaseCreator{err: domain.ErrInvalidReleaseInput}
	trigger := newTestBuildTriggerService(
		&fakeAutoBuildModuleRepository{modules: []domain.Module{module}},
		newFakeWebhookBuildRepository(),
		releases,
	)
	repository := &succeededBuildRepository{record: testSucceededWebhookBuild()}
	service := NewBuildService(repository).WithSucceededHook(trigger)

	record, err := service.CompleteBuildRecord(context.Background(), "build-1", domain.BuildRecordCompletion{
		ArtifactKey:    "rn-bundles/app/module/qa/build-1",
		RuntimeVersion: "1.0.0",
		Platforms:      []string{domain.PlatformAndroid},
	})
	if err != nil || record.Status != domain.BuildStatusSucceeded {
		t.Fatalf("expected build completion to succeed despite preview failure, got %+v, %v", record, err)
	}
	if len(releases.inputs) != 1 {
		t.Fatalf("expected succeeded hook to attempt preview release, got %d", len(releases.inputs))
	}
}

func newTestBuildTriggerService(modules AutoBuildModuleRepository, builds WebhookBuildRepository, releases PreviewReleaseCreator) *BuildTriggerService {
	return NewBuildTriggerService(BuildTriggerServiceDependencies{
		Builds:   builds,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modules:  modules,
		Releases: releases,
	})
}

func testAutoBuildModule(id string, branches []string, environment string) domain.Module {
	return domain.Module{
		ID: id,
		BuildConfig: domain.ModuleBuildConfig{
			AutoBuildBranches:    branches,
			AutoBuildEnvironment: environment,
			AutoPreviewTTLHours:  domain.DefaultModuleAutoPreviewTTLHours,
		},
	}
}

func testPushEvent(branch string, commitSHA string) domain.PushEvent {
	return domain.PushEvent{
		Provider:             domain.CodeSourceProviderGitLab,
		ConnectionID:         "conn-1",
		ExternalRepositoryID: "repo-42",
		Branch:               branch,
		CommitSHA:            commitSHA,
		DeliveryID:           "delivery-1",

		ConnectionCreatedByUserID: "user-owner",
	}
}

func testSucceededWebhookBuild() domain.BuildRecord {
	return domain.BuildRecord{
		ID:             "build-1",
		ModuleID:       "module-feature",
		Environment:    "qa",
		Repository:     domain.ModuleRepository{ConnectionID: "conn-1"},
		Branch:         "feature/login",
		CommitSHA:      "abc123def4567890",
		RuntimeVersion: "1.0.0",
		Status:         domain.BuildStatusSucceeded,
		TriggerType:    domain.BuildTriggerTypeWebhook,

		CreatedByUserID: "user-owner",
	}
}

type fakeAutoBuildModuleRepository struct {
	modules          []domain.Module
	lastConnectionID string
	lastRepositoryID string
}

func (r *fakeAutoBuildModuleRepository) ListAutoBuildModules(_ context.Context, connectionID string, externalRepositoryID string) ([]domain.Module, error) {
	r.lastConnectionID = connectionID
	r.lastRepositoryID = externalRepositoryID
	return r.modules, nil
}

func (r *fakeAutoBuildModuleRepository) GetModule(_ context.Context, id string) (domain.Module, error) {
	for _, module := range r.modules {
		if module.ID == id {
			return module, nil
		}
	}
	return domain.Module{}, domain.ErrModuleNotFound
}

// fakeWebhookBuildRepository 模拟仓储层按 (module, env, commit) 去重。
type fakeWebhookBuildRepository struct {
	active      map[string]string
	created     int
	failModules map[string]error
	inputs      []domain.BuildRecordInput
}

func newFakeWebhookBuildRepository() *fakeWebhookBuildRepository {
	return &fakeWebhookBuildRepository{active: map[string]string{}}
}

func (r *fakeWebhookBuildRepository) CreateBuildRecordUnlessActive(_ context.Context, input domain.BuildRecordInput) (domain.BuildRecord, bool, error) {
	if err := r.failModules[input.ModuleID]; err != nil {
		return domain.BuildRecord{}, false, err
	}
	r.inputs = append(r.inputs, input)
	key := input.ModuleID + "|" + input.Environment + "|" + input.CommitSHA
	if id, ok := r.active[key]; ok {
		return domain.BuildRecord{ID: id}, false, nil
	}
	r.created++
	id := "build-" + input.ModuleID + "-" + input.CommitSHA
	r.active[key] = id
	return domain.BuildRecord{ID: id}, true, nil
}

type fakePreviewReleaseCreator struct {
	err    error
	inputs []domain.ReleaseCreateInput
}

func (c *fakePreviewReleaseCreator) CreateRelease(_ context.Context, input domain.ReleaseCreateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	c.inputs = append(c.inputs, input)
	if c.err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, c.err
	}
	return domain.ReleaseRecord{ID: "release-1"}, domain.ReleaseRoute{ID: "route-1"}, nil
}

type succeededBuildRepository struct {
	fakeBuildRecordRepository
	record domain.BuildRecord
}

func (r *succeededBuildRepository) CompleteBuildRecord(context.Context, string, domain.BuildRecordCompletion) (domain.BuildRecord, error) {
	return r.record, nil
}
