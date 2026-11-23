package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"forge-server/internal/domain"
)

func TestReleaseServiceCreateFullReleaseNormalizesInput(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, _, err := service.CreateRelease(context.Background(), domain.ReleaseCreateInput{
		BuildRecordID:   " build-1 ",
		ReleaseType:     " full ",
		Description:     " QA 全量 ",
		CreatedByUserID: " user-1 ",
	})
	if err != nil {
		t.Fatalf("create release: %v", err)
	}

	if repository.lastCreateInput.BuildRecordID != "build-1" ||
		repository.lastCreateInput.ReleaseType != domain.ReleaseTypeFull ||
		repository.lastCreateInput.Description != "QA 全量" ||
		repository.lastCreateInput.CreatedByUserID != "user-1" {
		t.Fatalf("unexpected normalized input: %+v", repository.lastCreateInput)
	}
}

func TestReleaseServiceCreatePercentageGrayReleaseNormalizesInput(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, _, err := service.CreateRelease(context.Background(), domain.ReleaseCreateInput{
		BuildRecordID:   "build-1",
		ReleaseType:     domain.ReleaseTypeGray,
		GrayStrategy:    domain.ReleaseGrayStrategyPercentage,
		GrayPercentage:  42,
		CreatedByUserID: "user-1",
	})
	if err != nil {
		t.Fatalf("create release: %v", err)
	}

	if repository.lastCreateInput.ReleaseType != domain.ReleaseTypeGray ||
		repository.lastCreateInput.GrayStrategy != domain.ReleaseGrayStrategyPercentage ||
		repository.lastCreateInput.GrayPercentage != 42 {
		t.Fatalf("unexpected normalized input: %+v", repository.lastCreateInput)
	}
}

func TestReleaseServiceCreatePercentageGrayReleaseRejectsHundred(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, _, err := service.CreateRelease(context.Background(), domain.ReleaseCreateInput{
		BuildRecordID:   "build-1",
		ReleaseType:     domain.ReleaseTypeGray,
		GrayStrategy:    domain.ReleaseGrayStrategyPercentage,
		GrayPercentage:  100,
		CreatedByUserID: "user-1",
	})
	if !errors.Is(err, domain.ErrInvalidReleaseInput) {
		t.Fatalf("expected invalid release input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d", repository.createCalls)
	}
}

func TestReleaseServiceCreateFullReleaseRejectsGrayFields(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, _, err := service.CreateRelease(context.Background(), domain.ReleaseCreateInput{
		BuildRecordID:   "build-1",
		ReleaseType:     domain.ReleaseTypeFull,
		GrayStrategy:    domain.ReleaseGrayStrategyPercentage,
		GrayPercentage:  10,
		CreatedByUserID: "user-1",
	})
	if !errors.Is(err, domain.ErrInvalidReleaseInput) {
		t.Fatalf("expected invalid release input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d", repository.createCalls)
	}
}

// 按 identifier 定向灰度已由提测单取代，灰度只接受比例灰度。
func TestReleaseServiceCreateRejectsTargetedGray(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, _, err := service.CreateRelease(context.Background(), domain.ReleaseCreateInput{
		BuildRecordID:   "build-1",
		ReleaseType:     domain.ReleaseTypeGray,
		GrayStrategy:    "targeted",
		CreatedByUserID: "user-1",
	})
	if !errors.Is(err, domain.ErrInvalidReleaseInput) {
		t.Fatalf("expected invalid release input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d", repository.createCalls)
	}
}

func TestReleaseServiceUpdateGrayPercentageNormalizesInput(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, _, err := service.UpdateReleaseGrayPercentage(context.Background(), domain.ReleaseGrayPercentageUpdateInput{
		ID:              " rel-1 ",
		GrayPercentage:  100,
		UpdatedByUserID: " user-1 ",
	})
	if err != nil {
		t.Fatalf("update gray percentage: %v", err)
	}
	if repository.lastUpdateInput.ID != "rel-1" ||
		repository.lastUpdateInput.GrayPercentage != 100 ||
		repository.lastUpdateInput.UpdatedByUserID != "user-1" {
		t.Fatalf("unexpected update input: %+v", repository.lastUpdateInput)
	}
}

func TestReleaseServiceUpdateGrayPercentageRejectsOutOfRange(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, _, err := service.UpdateReleaseGrayPercentage(context.Background(), domain.ReleaseGrayPercentageUpdateInput{
		ID:              "rel-1",
		GrayPercentage:  101,
		UpdatedByUserID: "user-1",
	})
	if !errors.Is(err, domain.ErrInvalidReleaseInput) {
		t.Fatalf("expected invalid release input, got %v", err)
	}
	if repository.updateCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d", repository.updateCalls)
	}
}

func TestReleaseServiceResolveRouteNormalizesInput(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, err := service.ResolveReleaseRoute(context.Background(), domain.ReleaseResolveInput{
		ModuleID:    " module-profile ",
		Environment: " qa ",
		UserID:      " user-1 ",
	})
	if err != nil {
		t.Fatalf("resolve release route: %v", err)
	}

	if repository.lastResolveInput.ModuleID != "module-profile" ||
		repository.lastResolveInput.Environment != "qa" ||
		repository.lastResolveInput.UserID != "user-1" {
		t.Fatalf("unexpected normalized input: %+v", repository.lastResolveInput)
	}
}

func TestReleaseServiceResolveRouteValidatesRuntimeVersion(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	if _, err := service.ResolveReleaseRoute(context.Background(), domain.ReleaseResolveInput{
		ModuleID:       "module-profile",
		Environment:    "qa",
		RuntimeVersion: " 1.0.0 ",
	}); err != nil || repository.lastResolveInput.RuntimeVersion != "1.0.0" {
		t.Fatalf("expected trimmed runtimeVersion, got %+v, %v", repository.lastResolveInput, err)
	}

	if _, err := service.ResolveReleaseRoute(context.Background(), domain.ReleaseResolveInput{
		ModuleID:       "module-profile",
		Environment:    "qa",
		RuntimeVersion: "1.0.0/beta",
	}); !errors.Is(err, domain.ErrInvalidReleaseInput) {
		t.Fatalf("expected invalid runtimeVersion to be rejected, got %v", err)
	}
}

func TestReleaseServiceListReleasesNormalizesFilters(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, _, err := service.ListReleases(context.Background(), domain.ReleaseRecordFilters{
		ApplicationID: " app-main ",
		Environment:   " qa ",
		ModuleID:      " module-profile ",
		ReleaseType:   " gray ",
		Status:        " active ",
	})
	if err != nil {
		t.Fatalf("list releases: %v", err)
	}
	if repository.lastListFilters.ApplicationID != "app-main" ||
		repository.lastListFilters.Environment != "qa" ||
		repository.lastListFilters.ModuleID != "module-profile" ||
		repository.lastListFilters.ReleaseType != domain.ReleaseTypeGray ||
		repository.lastListFilters.Status != domain.ReleaseStatusActive {
		t.Fatalf("unexpected filters: %+v", repository.lastListFilters)
	}
}

func TestReleaseServiceListReleasesRejectsInvalidFilters(t *testing.T) {
	tests := []domain.ReleaseRecordFilters{
		{ModuleID: "bad module"},
		{ReleaseType: "partial"},
		{Status: "pending"},
	}

	for _, filters := range tests {
		repository := fakeReleaseRepository{}
		service := NewReleaseService(&repository)

		_, _, err := service.ListReleases(context.Background(), filters)
		if !errors.Is(err, domain.ErrInvalidReleaseInput) {
			t.Fatalf("expected invalid release input for %+v, got %v", filters, err)
		}
		if repository.listCalls != 0 {
			t.Fatalf("expected repository not to be called for %+v, got %d", filters, repository.listCalls)
		}
	}
}

func TestReleaseServiceListReleasesRejectsInvalidModuleFilter(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, _, err := service.ListReleases(context.Background(), domain.ReleaseRecordFilters{ModuleID: "bad module"})
	if !errors.Is(err, domain.ErrInvalidReleaseInput) {
		t.Fatalf("expected invalid release input, got %v", err)
	}
	if repository.listCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d", repository.listCalls)
	}
}

type fakeReleaseRepository struct {
	createCalls      int
	listCalls        int
	updateCalls      int
	lastCreateInput  domain.ReleaseCreateInput
	lastListFilters  domain.ReleaseRecordFilters
	lastResolveInput domain.ReleaseResolveInput
	lastUpdateInput  domain.ReleaseGrayPercentageUpdateInput
	records          []domain.ReleaseRecord
	// 停用、回滚相关
	current         domain.ReleaseRecord
	previous        domain.ReleaseRecord
	previousErr     error
	lastDeactivate  domain.ReleaseDeactivateInput
	deactivateCalls int
}

func (r *fakeReleaseRepository) GetRelease(_ context.Context, id string) (domain.ReleaseRecord, error) {
	if r.current.ID != id {
		return domain.ReleaseRecord{}, domain.ErrReleaseNotFound
	}
	return r.current, nil
}

func (r *fakeReleaseRepository) DeactivateRelease(_ context.Context, input domain.ReleaseDeactivateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	r.deactivateCalls++
	r.lastDeactivate = input
	return domain.ReleaseRecord{ID: input.ID, Status: domain.ReleaseStatusInactive}, domain.ReleaseRoute{}, nil
}

func (r *fakeReleaseRepository) PreviousFullRelease(_ context.Context, _ string) (domain.ReleaseRecord, error) {
	return r.previous, r.previousErr
}

func (r *fakeReleaseRepository) ListReleaseEvents(_ context.Context, _ string) ([]domain.ReleaseEvent, error) {
	return []domain.ReleaseEvent{}, nil
}

func (r *fakeReleaseRepository) CreateRelease(_ context.Context, input domain.ReleaseCreateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	r.createCalls++
	r.lastCreateInput = input
	record := domain.ReleaseRecord{
		ID:              "rel-1",
		BuildRecordID:   input.BuildRecordID,
		ReleaseType:     input.ReleaseType,
		GrayStrategy:    input.GrayStrategy,
		GrayPercentage:  input.GrayPercentage,
		ArtifactKey:     "rn-bundles/app/profile/qa/build-1",
		CreatedByUserID: input.CreatedByUserID,
	}
	route := domain.ReleaseRoute{
		ID:          "route-1",
		ReleaseID:   record.ID,
		RouteType:   domain.ReleaseRouteTypeDefault,
		ArtifactKey: record.ArtifactKey,
	}
	if input.ReleaseType == domain.ReleaseTypeGray && input.GrayStrategy == domain.ReleaseGrayStrategyPercentage {
		route.RouteType = domain.ReleaseRouteTypePercentage
		route.GrayStrategy = input.GrayStrategy
		route.GrayPercentage = input.GrayPercentage
	}
	return record, route, nil
}

func (r *fakeReleaseRepository) ListReleases(_ context.Context, filters domain.ReleaseRecordFilters) ([]domain.ReleaseRecord, domain.ReleaseRecordSummary, error) {
	r.listCalls++
	r.lastListFilters = filters
	summary := domain.ReleaseRecordSummary{Total: len(r.records)}
	for _, record := range r.records {
		if record.Status == domain.ReleaseStatusActive {
			summary.Active++
		} else if record.Status == domain.ReleaseStatusInactive {
			summary.Inactive++
		}
	}
	return r.records, summary, nil
}

func (r *fakeReleaseRepository) ResolveReleaseRoute(_ context.Context, input domain.ReleaseResolveInput) (domain.ReleaseRoute, error) {
	r.lastResolveInput = input
	return domain.ReleaseRoute{
		ID:          "route-1",
		ModuleID:    input.ModuleID,
		Environment: input.Environment,
		ArtifactKey: "rn-bundles/app/profile/qa/build-1",
	}, nil
}

func (r *fakeReleaseRepository) UpdateReleaseGrayPercentage(_ context.Context, input domain.ReleaseGrayPercentageUpdateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	r.updateCalls++
	r.lastUpdateInput = input
	return domain.ReleaseRecord{
		ID:             input.ID,
		ReleaseType:    domain.ReleaseTypeGray,
		GrayStrategy:   domain.ReleaseGrayStrategyPercentage,
		GrayPercentage: input.GrayPercentage,
		ArtifactKey:    "rn-bundles/app/profile/qa/build-1",
	}, domain.ReleaseRoute{
		ID:             "route-1",
		ReleaseID:      input.ID,
		RouteType:      domain.ReleaseRouteTypePercentage,
		GrayStrategy:   domain.ReleaseGrayStrategyPercentage,
		GrayPercentage: input.GrayPercentage,
		ArtifactKey:    "rn-bundles/app/profile/qa/build-1",
	}, nil
}

const testReleaseUUID = "018f4d7a-0a6c-789b-9c12-36a4dfd9d001"

func TestReleaseServiceDeactivateValidatesInput(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)
	for name, input := range map[string]domain.ReleaseDeactivateInput{
		"not uuid":        {ID: "rel-1", ActorUserID: "ou-1"},
		"no actor":        {ID: testReleaseUUID},
		"reason too long": {ID: testReleaseUUID, ActorUserID: "ou-1", Reason: strings.Repeat("长", domain.MaxReleaseReasonLength+1)},
	} {
		if _, _, err := service.DeactivateRelease(context.Background(), input); !errors.Is(err, domain.ErrInvalidReleaseInput) {
			t.Fatalf("%s: expected invalid input, got %v", name, err)
		}
	}
	if _, _, err := service.DeactivateRelease(context.Background(), domain.ReleaseDeactivateInput{ID: " " + testReleaseUUID + " ", ActorUserID: "ou-1", Reason: " 线上白屏 "}); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if repository.lastDeactivate.ID != testReleaseUUID || repository.lastDeactivate.Reason != "线上白屏" {
		t.Fatalf("expected trimmed input, got %+v", repository.lastDeactivate)
	}
}

func TestReleaseServiceRollbackCreatesFullReleaseOfPreviousBuild(t *testing.T) {
	repository := fakeReleaseRepository{
		current:  domain.ReleaseRecord{ID: testReleaseUUID, ReleaseType: domain.ReleaseTypeFull, Status: domain.ReleaseStatusActive, BuildRecordID: "build-new"},
		previous: domain.ReleaseRecord{ID: "release-old", ReleaseType: domain.ReleaseTypeFull, BuildRecordID: "build-old"},
	}
	service := NewReleaseService(&repository)
	if _, _, err := service.RollbackRelease(context.Background(), domain.ReleaseRollbackInput{ID: testReleaseUUID, ActorUserID: "ou-1", Description: "回滚白屏"}); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	input := repository.lastCreateInput
	if input.BuildRecordID != "build-old" || input.ReleaseType != domain.ReleaseTypeFull || input.RollbackFromReleaseID != testReleaseUUID ||
		input.CreatedByUserID != "ou-1" || input.Description != "回滚白屏" {
		t.Fatalf("unexpected rollback release input: %+v", input)
	}

	repository.current.ReleaseType = domain.ReleaseTypeGray
	if _, _, err := service.RollbackRelease(context.Background(), domain.ReleaseRollbackInput{ID: testReleaseUUID, ActorUserID: "ou-1"}); !errors.Is(err, domain.ErrReleaseRollbackUnavailable) {
		t.Fatalf("expected gray release rollback to be unavailable, got %v", err)
	}
	repository.current.ReleaseType, repository.current.Status = domain.ReleaseTypeFull, domain.ReleaseStatusInactive
	if _, _, err := service.RollbackRelease(context.Background(), domain.ReleaseRollbackInput{ID: testReleaseUUID, ActorUserID: "ou-1"}); !errors.Is(err, domain.ErrReleaseNotActive) {
		t.Fatalf("expected inactive release rollback to fail, got %v", err)
	}
	repository.current.Status, repository.previousErr = domain.ReleaseStatusActive, domain.ErrReleaseRollbackUnavailable
	if _, _, err := service.RollbackRelease(context.Background(), domain.ReleaseRollbackInput{ID: testReleaseUUID, ActorUserID: "ou-1"}); !errors.Is(err, domain.ErrReleaseRollbackUnavailable) {
		t.Fatalf("expected missing previous full to be unavailable, got %v", err)
	}
}
