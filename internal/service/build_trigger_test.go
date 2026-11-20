package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

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

func TestBuildTriggerFollowsBuildInTestPlans(t *testing.T) {
	followers := &fakeTestPlanFollower{planIDs: []string{"plan-1"}}
	trigger := newTestBuildTriggerService(&fakeAutoBuildModuleRepository{}, newFakeWebhookBuildRepository(), followers)

	manual := testSucceededWebhookBuild()
	manual.TriggerType = domain.BuildTriggerTypeManual
	trigger.HandleBuildSucceeded(context.Background(), manual)

	if len(followers.records) != 1 || followers.records[0].ID != "build-1" || followers.records[0].Branch != "feature/login" {
		t.Fatalf("expected the succeeded build to be followed regardless of trigger type, got %+v", followers.records)
	}
}

func TestBuildTriggerSkipsFollowWithoutBranch(t *testing.T) {
	followers := &fakeTestPlanFollower{}
	trigger := newTestBuildTriggerService(&fakeAutoBuildModuleRepository{}, newFakeWebhookBuildRepository(), followers)

	record := testSucceededWebhookBuild()
	record.Branch = " "
	trigger.HandleBuildSucceeded(context.Background(), record)

	if len(followers.records) != 0 {
		t.Fatalf("expected no follow for a build without branch, got %+v", followers.records)
	}
}

func TestBuildServiceCompleteInvokesSucceededHookAndIgnoresFollowFailure(t *testing.T) {
	followers := &fakeTestPlanFollower{err: errors.New("database unavailable")}
	trigger := newTestBuildTriggerService(&fakeAutoBuildModuleRepository{}, newFakeWebhookBuildRepository(), followers)
	repository := &succeededBuildRepository{record: testSucceededWebhookBuild()}
	service := NewBuildService(repository).WithSucceededHook(trigger)

	record, err := service.CompleteBuildRecord(context.Background(), "build-1", "worker-1", domain.BuildRecordCompletion{
		ArtifactKey:    "rn-bundles/app/module/qa/build-1",
		RuntimeVersion: "1.0.0",
		Platforms:      []string{domain.PlatformAndroid},
	})
	if err != nil || record.Status != domain.BuildStatusSucceeded {
		t.Fatalf("expected build completion to succeed despite follow failure, got %+v, %v", record, err)
	}
	if len(followers.records) != 1 {
		t.Fatalf("expected succeeded hook to attempt follow, got %d", len(followers.records))
	}
}

func newTestBuildTriggerService(modules AutoBuildModuleRepository, builds WebhookBuildRepository, testPlans TestPlanBuildFollower) *BuildTriggerService {
	return NewBuildTriggerService(BuildTriggerServiceDependencies{
		Builds:    builds,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modules:   modules,
		TestPlans: testPlans,
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

type fakeTestPlanFollower struct {
	err     error
	planIDs []string
	records []domain.BuildRecord
}

func (f *fakeTestPlanFollower) FollowBuild(_ context.Context, record domain.BuildRecord) ([]string, error) {
	f.records = append(f.records, record)
	return f.planIDs, f.err
}

type succeededBuildRepository struct {
	fakeBuildRecordRepository
	record domain.BuildRecord
}

func (r *succeededBuildRepository) CompleteBuildRecord(context.Context, string, string, domain.BuildRecordCompletion) (domain.BuildRecord, error) {
	return r.record, nil
}

func TestBuildTriggerSkipsModulesWithDisabledAdapter(t *testing.T) {
	modules := &fakeAutoBuildModuleRepository{modules: []domain.Module{
		testAutoBuildModule("module-disabled-adapter", []string{"main"}, "qa"),
	}}
	builds := newFakeWebhookBuildRepository()
	builds.failModules = map[string]error{"module-disabled-adapter": domain.ErrBuildAdapterDisabled}
	trigger := newTestBuildTriggerService(modules, builds, nil)

	result, err := trigger.HandlePushEvent(context.Background(), testPushEvent("main", "abc123"))
	if err != nil {
		t.Fatalf("expected disabled adapter to be skipped without webhook retry, got %v", err)
	}
	if len(result.CreatedBuildIDs) != 0 || len(result.DuplicateBuildIDs) != 0 {
		t.Fatalf("expected no builds, got %#v", result)
	}
}
