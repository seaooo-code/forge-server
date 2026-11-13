package service

import (
	"context"
	"errors"
	"testing"
	"time"

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
		repository.lastCreateInput.Identifier != "" ||
		repository.lastCreateInput.Description != "QA 全量" ||
		repository.lastCreateInput.CreatedByUserID != "user-1" {
		t.Fatalf("unexpected normalized input: %+v", repository.lastCreateInput)
	}
}

func TestReleaseServiceCreateTargetedGrayReleaseNormalizesInput(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	_, _, err := service.CreateRelease(context.Background(), domain.ReleaseCreateInput{
		BuildRecordID:   " build-1 ",
		ReleaseType:     " gray ",
		GrayStrategy:    " targeted ",
		ExpiresAt:       &expiresAt,
		Identifier:      " staff-1 ",
		CreatedByUserID: " user-1 ",
	})
	if err != nil {
		t.Fatalf("create release: %v", err)
	}

	if repository.lastCreateInput.ReleaseType != domain.ReleaseTypeGray ||
		repository.lastCreateInput.GrayStrategy != domain.ReleaseGrayStrategyTargeted ||
		repository.lastCreateInput.ExpiresAt == nil ||
		repository.lastCreateInput.Identifier != "staff-1" ||
		repository.lastCreateInput.GrayPercentage != 0 {
		t.Fatalf("unexpected normalized input: %+v", repository.lastCreateInput)
	}
	if repository.expireCalls != 1 {
		t.Fatalf("expected targeted gray cleanup before create, got %d", repository.expireCalls)
	}
}

func TestReleaseServiceCreateTargetedGrayReleaseRequiresIdentifier(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	_, _, err := service.CreateRelease(context.Background(), domain.ReleaseCreateInput{
		BuildRecordID:   "build-1",
		ReleaseType:     domain.ReleaseTypeGray,
		GrayStrategy:    domain.ReleaseGrayStrategyTargeted,
		ExpiresAt:       &expiresAt,
		CreatedByUserID: "user-1",
	})
	if !errors.Is(err, domain.ErrInvalidReleaseInput) {
		t.Fatalf("expected invalid release input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d", repository.createCalls)
	}
}

func TestReleaseServiceCreateTargetedGrayReleaseRequiresFutureExpiry(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)
	expiredAt := time.Now().Add(-time.Minute)

	tests := []struct {
		name      string
		expiresAt *time.Time
	}{
		{name: "missing", expiresAt: nil},
		{name: "expired", expiresAt: &expiredAt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := service.CreateRelease(context.Background(), domain.ReleaseCreateInput{
				BuildRecordID:   "build-1",
				ReleaseType:     domain.ReleaseTypeGray,
				GrayStrategy:    domain.ReleaseGrayStrategyTargeted,
				Identifier:      "staff-1",
				ExpiresAt:       tt.expiresAt,
				CreatedByUserID: "user-1",
			})
			if !errors.Is(err, domain.ErrInvalidReleaseInput) {
				t.Fatalf("expected invalid release input, got %v", err)
			}
		})
	}
	if repository.createCalls != 0 || repository.expireCalls != 0 {
		t.Fatalf("expected repository not to be called, create=%d expire=%d", repository.createCalls, repository.expireCalls)
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
		repository.lastCreateInput.GrayPercentage != 42 ||
		repository.lastCreateInput.Identifier != "" {
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
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	_, _, err := service.CreateRelease(context.Background(), domain.ReleaseCreateInput{
		BuildRecordID:   "build-1",
		ReleaseType:     domain.ReleaseTypeFull,
		GrayStrategy:    domain.ReleaseGrayStrategyTargeted,
		ExpiresAt:       &expiresAt,
		Identifier:      "staff-1",
		CreatedByUserID: "user-1",
	})
	if !errors.Is(err, domain.ErrInvalidReleaseInput) {
		t.Fatalf("expected invalid release input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d", repository.createCalls)
	}
}

func TestReleaseServiceCreatePercentageGrayReleaseRejectsExpiry(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	_, _, err := service.CreateRelease(context.Background(), domain.ReleaseCreateInput{
		BuildRecordID:   "build-1",
		ReleaseType:     domain.ReleaseTypeGray,
		GrayStrategy:    domain.ReleaseGrayStrategyPercentage,
		GrayPercentage:  25,
		ExpiresAt:       &expiresAt,
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

func TestReleaseServiceInactivateTargetedGrayReleaseNormalizesInput(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, _, err := service.InactivateTargetedGrayRelease(context.Background(), domain.ReleaseInactivateInput{
		ID:              " rel-1 ",
		UpdatedByUserID: " user-1 ",
	})
	if err != nil {
		t.Fatalf("inactivate targeted gray release: %v", err)
	}
	if repository.lastInactivateInput.ID != "rel-1" ||
		repository.lastInactivateInput.UpdatedByUserID != "user-1" {
		t.Fatalf("unexpected inactivate input: %+v", repository.lastInactivateInput)
	}
}

func TestReleaseServiceInactivateTargetedGrayReleaseRejectsInvalidInput(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, _, err := service.InactivateTargetedGrayRelease(context.Background(), domain.ReleaseInactivateInput{
		ID: "rel-1",
	})
	if !errors.Is(err, domain.ErrInvalidReleaseInput) {
		t.Fatalf("expected invalid release input, got %v", err)
	}
	if repository.inactivateCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d", repository.inactivateCalls)
	}
}

func TestReleaseServiceResolveRouteNormalizesInput(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, err := service.ResolveReleaseRoute(context.Background(), domain.ReleaseResolveInput{
		ModuleID:    " module-profile ",
		Environment: " qa ",
		Identifier:  " staff-1 ",
		UserID:      " user-1 ",
	})
	if err != nil {
		t.Fatalf("resolve release route: %v", err)
	}

	if repository.lastResolveInput.ModuleID != "module-profile" ||
		repository.lastResolveInput.Environment != "qa" ||
		repository.lastResolveInput.Identifier != "staff-1" ||
		repository.lastResolveInput.UserID != "user-1" {
		t.Fatalf("unexpected normalized input: %+v", repository.lastResolveInput)
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
	createCalls         int
	expireCalls         int
	inactivateCalls     int
	listCalls           int
	updateCalls         int
	lastCreateInput     domain.ReleaseCreateInput
	lastInactivateInput domain.ReleaseInactivateInput
	lastListFilters     domain.ReleaseRecordFilters
	lastResolveInput    domain.ReleaseResolveInput
	lastUpdateInput     domain.ReleaseGrayPercentageUpdateInput
	records             []domain.ReleaseRecord
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
		ExpiresAt:       input.ExpiresAt,
		ArtifactKey:     "rn-bundles/app/profile/qa/build-1",
		CreatedByUserID: input.CreatedByUserID,
	}
	route := domain.ReleaseRoute{
		ID:          "route-1",
		ReleaseID:   record.ID,
		RouteType:   domain.ReleaseRouteTypeDefault,
		Identifier:  input.Identifier,
		ArtifactKey: record.ArtifactKey,
	}
	if input.ReleaseType == domain.ReleaseTypeGray && input.GrayStrategy == domain.ReleaseGrayStrategyTargeted {
		route.RouteType = domain.ReleaseRouteTypeIdentifier
	}
	if input.ReleaseType == domain.ReleaseTypeGray && input.GrayStrategy == domain.ReleaseGrayStrategyPercentage {
		route.RouteType = domain.ReleaseRouteTypePercentage
		route.GrayStrategy = input.GrayStrategy
		route.GrayPercentage = input.GrayPercentage
	}
	return record, route, nil
}

func (r *fakeReleaseRepository) ExpireTargetedGrayReleases(context.Context) (int64, error) {
	r.expireCalls++
	return 0, nil
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
		Identifier:  input.Identifier,
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

func (r *fakeReleaseRepository) InactivateTargetedGrayRelease(_ context.Context, input domain.ReleaseInactivateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	r.inactivateCalls++
	r.lastInactivateInput = input
	return domain.ReleaseRecord{
		ID:           input.ID,
		ReleaseType:  domain.ReleaseTypeGray,
		GrayStrategy: domain.ReleaseGrayStrategyTargeted,
		Status:       domain.ReleaseStatusInactive,
		ArtifactKey:  "rn-bundles/app/profile/qa/build-1",
	}, domain.ReleaseRoute{
		ID:           "route-1",
		ReleaseID:    input.ID,
		RouteType:    domain.ReleaseRouteTypeIdentifier,
		GrayStrategy: domain.ReleaseGrayStrategyTargeted,
		Status:       domain.ReleaseRouteStatusInactive,
		ArtifactKey:  "rn-bundles/app/profile/qa/build-1",
	}, nil
}
