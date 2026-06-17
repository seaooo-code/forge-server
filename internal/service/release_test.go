package service

import (
	"context"
	"errors"
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
		repository.lastCreateInput.Identifier != "" ||
		repository.lastCreateInput.Description != "QA 全量" ||
		repository.lastCreateInput.CreatedByUserID != "user-1" {
		t.Fatalf("unexpected normalized input: %+v", repository.lastCreateInput)
	}
}

func TestReleaseServiceCreateTargetedReleaseRequiresIdentifier(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, _, err := service.CreateRelease(context.Background(), domain.ReleaseCreateInput{
		BuildRecordID:   "build-1",
		ReleaseType:     domain.ReleaseTypeTargeted,
		CreatedByUserID: "user-1",
	})
	if !errors.Is(err, domain.ErrInvalidReleaseInput) {
		t.Fatalf("expected invalid release input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d", repository.createCalls)
	}
}

func TestReleaseServiceCreateFullReleaseRejectsIdentifier(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, _, err := service.CreateRelease(context.Background(), domain.ReleaseCreateInput{
		BuildRecordID:   "build-1",
		ReleaseType:     domain.ReleaseTypeFull,
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

func TestReleaseServiceResolveRouteNormalizesInput(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, err := service.ResolveReleaseRoute(context.Background(), domain.ReleaseResolveInput{
		ModuleID:    " module-profile ",
		Environment: " qa ",
		Identifier:  " staff-1 ",
	})
	if err != nil {
		t.Fatalf("resolve release route: %v", err)
	}

	if repository.lastResolveInput.ModuleID != "module-profile" ||
		repository.lastResolveInput.Environment != "qa" ||
		repository.lastResolveInput.Identifier != "staff-1" {
		t.Fatalf("unexpected normalized input: %+v", repository.lastResolveInput)
	}
}

func TestReleaseServiceListReleasesRejectsInvalidModuleFilter(t *testing.T) {
	repository := fakeReleaseRepository{}
	service := NewReleaseService(&repository)

	_, err := service.ListReleases(context.Background(), "bad module", "")
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
	lastCreateInput  domain.ReleaseCreateInput
	lastResolveInput domain.ReleaseResolveInput
	records          []domain.ReleaseRecord
}

func (r *fakeReleaseRepository) CreateRelease(_ context.Context, input domain.ReleaseCreateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	r.createCalls++
	r.lastCreateInput = input
	record := domain.ReleaseRecord{
		ID:              "rel-1",
		BuildRecordID:   input.BuildRecordID,
		ReleaseType:     input.ReleaseType,
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
	if input.ReleaseType == domain.ReleaseTypeTargeted {
		route.RouteType = domain.ReleaseRouteTypeIdentifier
	}
	return record, route, nil
}

func (r *fakeReleaseRepository) ListReleases(context.Context, string, string) ([]domain.ReleaseRecord, error) {
	r.listCalls++
	return r.records, nil
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
