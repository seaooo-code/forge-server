package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

func TestCreateRelease(t *testing.T) {
	service := &fakeReleaseService{
		record: testHTTPReleaseRecord(domain.ReleaseTypeFull),
		route:  testHTTPReleaseRoute(domain.ReleaseRouteTypeDefault, ""),
	}
	e := New(config.Config{}, Dependencies{
		ReleaseService: service,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/releases", jsonBody(releaseCreateRequest{
		BuildRecordID:   "build-1",
		ReleaseType:     "full",
		Description:     "QA 全量",
		CreatedByUserID: "user-1",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var response releaseDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Release.ID != "rel-1" || response.Release.ReleaseType != "full" {
		t.Fatalf("unexpected release response: %#v", response.Release)
	}
	if response.Route.RouteType != "default" || response.Route.ArtifactKey == "" {
		t.Fatalf("unexpected route response: %#v", response.Route)
	}
	if service.lastCreateInput.BuildRecordID != "build-1" ||
		service.lastCreateInput.ReleaseType != domain.ReleaseTypeFull ||
		service.lastCreateInput.CreatedByUserID != "user-1" {
		t.Fatalf("unexpected service input: %+v", service.lastCreateInput)
	}
}

func TestCreateTargetedRelease(t *testing.T) {
	service := &fakeReleaseService{
		record: testHTTPReleaseRecord(domain.ReleaseTypeTargeted),
		route:  testHTTPReleaseRoute(domain.ReleaseRouteTypeIdentifier, "staff-1"),
	}
	e := New(config.Config{}, Dependencies{
		ReleaseService: service,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/releases", jsonBody(releaseCreateRequest{
		BuildRecordID:   "build-1",
		ReleaseType:     "targeted",
		Identifier:      "staff-1",
		CreatedByUserID: "user-1",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var response releaseDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Route.RouteType != "identifier" || response.Route.Identifier != "staff-1" {
		t.Fatalf("unexpected targeted route response: %#v", response.Route)
	}
	if service.lastCreateInput.Identifier != "staff-1" ||
		service.lastCreateInput.ReleaseType != domain.ReleaseTypeTargeted {
		t.Fatalf("unexpected service input: %+v", service.lastCreateInput)
	}
}

func TestListReleases(t *testing.T) {
	service := &fakeReleaseService{
		records: []domain.ReleaseRecord{testHTTPReleaseRecord(domain.ReleaseTypeFull)},
	}
	e := New(config.Config{}, Dependencies{
		ReleaseService: service,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/releases?moduleId=module-profile&environment=qa", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response releasesResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Releases) != 1 || response.Releases[0].ID != "rel-1" {
		t.Fatalf("unexpected releases response: %#v", response.Releases)
	}
	if service.lastListModuleID != "module-profile" || service.lastListEnvironment != "qa" {
		t.Fatalf("unexpected service filters: module=%q environment=%q", service.lastListModuleID, service.lastListEnvironment)
	}
}

func TestResolveReleaseRoute(t *testing.T) {
	service := &fakeReleaseService{
		route: testHTTPReleaseRoute(domain.ReleaseRouteTypeIdentifier, "staff-1"),
	}
	e := New(config.Config{}, Dependencies{
		ReleaseService: service,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/releases/resolve?moduleId=module-profile&environment=qa&identifier=staff-1", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response releaseResolveResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Route.ID != "route-1" || response.Route.ArtifactKey == "" {
		t.Fatalf("unexpected resolve response: %#v", response.Route)
	}
	if service.lastResolveInput.ModuleID != "module-profile" ||
		service.lastResolveInput.Environment != "qa" ||
		service.lastResolveInput.Identifier != "staff-1" {
		t.Fatalf("unexpected resolve input: %+v", service.lastResolveInput)
	}
}

func TestCreateReleaseRejectsInvalidRequest(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ReleaseService: &fakeReleaseService{
			err: domain.ErrInvalidReleaseInput,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/releases", jsonBody(releaseCreateRequest{
		BuildRecordID:   "build-1",
		ReleaseType:     "full",
		Identifier:      "staff-1",
		CreatedByUserID: "user-1",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

type fakeReleaseService struct {
	record              domain.ReleaseRecord
	route               domain.ReleaseRoute
	records             []domain.ReleaseRecord
	err                 error
	lastCreateInput     domain.ReleaseCreateInput
	lastListModuleID    string
	lastListEnvironment string
	lastResolveInput    domain.ReleaseResolveInput
}

func (s *fakeReleaseService) CreateRelease(_ context.Context, input domain.ReleaseCreateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	s.lastCreateInput = input
	if s.err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, s.err
	}
	return s.record, s.route, nil
}

func (s *fakeReleaseService) ListReleases(_ context.Context, moduleID string, environment string) ([]domain.ReleaseRecord, error) {
	s.lastListModuleID = moduleID
	s.lastListEnvironment = environment
	if s.err != nil {
		return nil, s.err
	}
	return s.records, nil
}

func (s *fakeReleaseService) ResolveReleaseRoute(_ context.Context, input domain.ReleaseResolveInput) (domain.ReleaseRoute, error) {
	s.lastResolveInput = input
	if s.err != nil {
		return domain.ReleaseRoute{}, s.err
	}
	return s.route, nil
}

func testHTTPReleaseRecord(releaseType domain.ReleaseType) domain.ReleaseRecord {
	return domain.ReleaseRecord{
		ID:              "rel-1",
		ApplicationName: "seaooo",
		ModuleID:        "module-profile",
		ModuleName:      "profile",
		Environment:     "qa",
		EnvironmentName: "QA",
		BuildRecordID:   "build-1",
		ReleaseType:     releaseType,
		Status:          domain.ReleaseStatusActive,
		ArtifactKey:     "rn-bundles/seaooo/profile/QA/build-1",
		ArtifactURL:     "https://cdn.example.com/rn-bundles/seaooo/profile/QA/build-1",
		Description:     "QA 全量",
		CreatedByUserID: "user-1",
		CreatedBy:       "海鸥",
		CreatedAt:       time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC),
	}
}

func testHTTPReleaseRoute(routeType domain.ReleaseRouteType, identifier string) domain.ReleaseRoute {
	return domain.ReleaseRoute{
		ID:              "route-1",
		ApplicationName: "seaooo",
		ModuleID:        "module-profile",
		ModuleName:      "profile",
		Environment:     "qa",
		EnvironmentName: "QA",
		ReleaseID:       "rel-1",
		BuildRecordID:   "build-1",
		RouteType:       routeType,
		Identifier:      identifier,
		Status:          domain.ReleaseRouteStatusActive,
		ArtifactKey:     "rn-bundles/seaooo/profile/QA/build-1",
		ArtifactURL:     "https://cdn.example.com/rn-bundles/seaooo/profile/QA/build-1",
		CreatedByUserID: "user-1",
		CreatedBy:       "海鸥",
		CreatedAt:       time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC),
	}
}
