package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"forge-server/internal/domain"
)

func TestCreateRelease(t *testing.T) {
	service := &fakeReleaseService{
		record: testHTTPReleaseRecord(domain.ReleaseTypeFull),
		route:  testHTTPReleaseRoute(domain.ReleaseRouteTypeDefault, ""),
	}
	e := newAuthorizedTestServer(Dependencies{
		FeishuAuthService: testReleaseAuthService(),
		ReleaseService:    service,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/releases", jsonBody(releaseCreateRequest{
		BuildRecordID: "build-1",
		ReleaseType:   "full",
		Description:   "QA 全量",
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestReleaseSession(req)
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
		service.lastCreateInput.CreatedByUserID != "ou-user-1" {
		t.Fatalf("unexpected service input: %+v", service.lastCreateInput)
	}
}

func TestCreateTargetedGrayRelease(t *testing.T) {
	expiresAt := time.Now().Add(7 * 24 * time.Hour).UTC().Truncate(time.Second)
	service := &fakeReleaseService{
		record: testHTTPGrayReleaseRecord(domain.ReleaseGrayStrategyTargeted, 0, &expiresAt),
		route:  testHTTPReleaseRoute(domain.ReleaseRouteTypeIdentifier, "staff-1"),
	}
	e := newAuthorizedTestServer(Dependencies{
		FeishuAuthService: testReleaseAuthService(),
		ReleaseService:    service,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/releases", jsonBody(releaseCreateRequest{
		BuildRecordID: "build-1",
		ReleaseType:   "gray",
		GrayStrategy:  "targeted",
		ExpiresAt:     expiresAt.Format(time.RFC3339),
		Identifier:    "staff-1",
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestReleaseSession(req)
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
	if response.Release.ReleaseType != "gray" ||
		response.Release.GrayStrategy != "targeted" ||
		response.Release.ExpiresAt == "" ||
		response.Release.Identifier != "staff-1" {
		t.Fatalf("unexpected targeted release response: %#v", response.Release)
	}
	if service.lastCreateInput.Identifier != "staff-1" ||
		service.lastCreateInput.ReleaseType != domain.ReleaseTypeGray ||
		service.lastCreateInput.GrayStrategy != domain.ReleaseGrayStrategyTargeted ||
		service.lastCreateInput.ExpiresAt == nil ||
		!service.lastCreateInput.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("unexpected service input: %+v", service.lastCreateInput)
	}
}

func TestCreatePercentageGrayRelease(t *testing.T) {
	service := &fakeReleaseService{
		record: testHTTPGrayReleaseRecord(domain.ReleaseGrayStrategyPercentage, 25, nil),
		route:  testHTTPReleaseRoute(domain.ReleaseRouteTypePercentage, ""),
	}
	e := newAuthorizedTestServer(Dependencies{
		FeishuAuthService: testReleaseAuthService(),
		ReleaseService:    service,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/releases", jsonBody(releaseCreateRequest{
		BuildRecordID:  "build-1",
		ReleaseType:    "gray",
		GrayStrategy:   "percentage",
		GrayPercentage: 25,
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestReleaseSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var response releaseDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Route.RouteType != "percentage" || response.Release.GrayPercentage != 25 {
		t.Fatalf("unexpected percentage release response: %#v route=%#v", response.Release, response.Route)
	}
	if service.lastCreateInput.ReleaseType != domain.ReleaseTypeGray ||
		service.lastCreateInput.GrayStrategy != domain.ReleaseGrayStrategyPercentage ||
		service.lastCreateInput.GrayPercentage != 25 {
		t.Fatalf("unexpected service input: %+v", service.lastCreateInput)
	}
}

func TestListReleases(t *testing.T) {
	expiresAt := time.Now().Add(7 * 24 * time.Hour).UTC().Truncate(time.Second)
	service := &fakeReleaseService{
		records: []domain.ReleaseRecord{testHTTPGrayReleaseRecord(domain.ReleaseGrayStrategyTargeted, 0, &expiresAt)},
		summary: domain.ReleaseRecordSummary{Active: 7, Inactive: 3, Total: 10},
	}
	e := newAuthorizedTestServer(Dependencies{
		FeishuAuthService: testReleaseAuthService(),
		ReleaseService:    service,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/releases?applicationId=app-main&moduleId=module-profile&environment=qa&status=active&releaseType=gray&page=2&pageSize=50", nil)
	addTestReleaseSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response releasesResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Releases) != 1 ||
		response.Releases[0].ID != "rel-1" ||
		response.Releases[0].Identifier != "staff-1" ||
		response.Releases[0].ExpiresAt == "" {
		t.Fatalf("unexpected releases response: %#v", response.Releases)
	}
	if response.Pagination.Page != 2 || response.Pagination.PageSize != 50 || response.Pagination.Total != 10 || response.Summary.Active != 7 {
		t.Fatalf("unexpected release pagination or summary: pagination=%+v summary=%+v", response.Pagination, response.Summary)
	}
	if service.lastListFilters.ApplicationID != "app-main" ||
		service.lastListFilters.ModuleID != "module-profile" ||
		service.lastListFilters.Environment != "qa" ||
		service.lastListFilters.Status != domain.ReleaseStatusActive ||
		service.lastListFilters.ReleaseType != domain.ReleaseTypeGray ||
		service.lastListFilters.Pagination != (domain.Pagination{Page: 2, PageSize: 50}) {
		t.Fatalf("unexpected service filters: %+v", service.lastListFilters)
	}
}

func TestResolveReleaseRoute(t *testing.T) {
	service := &fakeReleaseService{
		route: testHTTPReleaseRoute(domain.ReleaseRouteTypeIdentifier, "staff-1"),
	}
	e := newAuthorizedTestServer(Dependencies{
		FeishuAuthService: testReleaseAuthService(),
		ReleaseService:    service,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/releases/resolve?moduleId=module-profile&environment=qa&identifier=staff-1&userId=user-1", nil)
	addTestReleaseSession(req)
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
		service.lastResolveInput.Identifier != "staff-1" ||
		service.lastResolveInput.UserID != "user-1" {
		t.Fatalf("unexpected resolve input: %+v", service.lastResolveInput)
	}
}

func TestUpdateReleaseGrayPercentage(t *testing.T) {
	service := &fakeReleaseService{
		record: testHTTPGrayReleaseRecord(domain.ReleaseGrayStrategyPercentage, 100, nil),
		route:  testHTTPReleaseRoute(domain.ReleaseRouteTypeDefault, ""),
	}
	e := newAuthorizedTestServer(Dependencies{
		FeishuAuthService: testReleaseAuthService(),
		ReleaseService:    service,
	})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/releases/rel-1/gray-percentage", jsonBody(releaseGrayPercentageUpdateRequest{
		GrayPercentage: 100,
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestReleaseSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if service.lastUpdateInput.ID != "rel-1" ||
		service.lastUpdateInput.GrayPercentage != 100 ||
		service.lastUpdateInput.UpdatedByUserID != "ou-user-1" {
		t.Fatalf("unexpected update input: %+v", service.lastUpdateInput)
	}
}

func TestInactivateTargetedGrayRelease(t *testing.T) {
	record := testHTTPGrayReleaseRecord(domain.ReleaseGrayStrategyTargeted, 0, nil)
	record.Status = domain.ReleaseStatusInactive
	route := testHTTPReleaseRoute(domain.ReleaseRouteTypeIdentifier, "staff-1")
	route.Status = domain.ReleaseRouteStatusInactive
	service := &fakeReleaseService{
		record: record,
		route:  route,
	}
	e := newAuthorizedTestServer(Dependencies{
		FeishuAuthService: testReleaseAuthService(),
		ReleaseService:    service,
	})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/releases/rel-1/inactivate", nil)
	addTestReleaseSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	var response releaseDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Release.Status != "inactive" || response.Route.Status != "inactive" {
		t.Fatalf("unexpected inactivate response: %#v route=%#v", response.Release, response.Route)
	}
	if service.lastInactivateInput.ID != "rel-1" ||
		service.lastInactivateInput.UpdatedByUserID != "ou-user-1" {
		t.Fatalf("unexpected inactivate input: %+v", service.lastInactivateInput)
	}
}

func TestCreateReleaseRejectsInvalidExpiresAt(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		FeishuAuthService: testReleaseAuthService(),
		ReleaseService:    &fakeReleaseService{},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/releases", jsonBody(releaseCreateRequest{
		BuildRecordID: "build-1",
		ReleaseType:   "gray",
		GrayStrategy:  "targeted",
		ExpiresAt:     "not-a-time",
		Identifier:    "staff-1",
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestReleaseSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestCreateReleaseRejectsInvalidRequest(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		FeishuAuthService: testReleaseAuthService(),
		ReleaseService: &fakeReleaseService{
			err: domain.ErrInvalidReleaseInput,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/releases", jsonBody(releaseCreateRequest{
		BuildRecordID: "build-1",
		ReleaseType:   "full",
		Identifier:    "staff-1",
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestReleaseSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestCreateReleaseRejectsAlreadyActiveRelease(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		FeishuAuthService: testReleaseAuthService(),
		ReleaseService: &fakeReleaseService{
			err: domain.ErrReleaseAlreadyActive,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/releases", jsonBody(releaseCreateRequest{
		BuildRecordID: "build-1",
		ReleaseType:   "gray",
		GrayStrategy:  "percentage",
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestReleaseSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d: %s", http.StatusConflict, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "release already active") {
		t.Fatalf("expected release already active error, got %s", rec.Body.String())
	}
}

func testReleaseAuthService() *fakeFeishuAuthService {
	return &fakeFeishuAuthService{result: domain.FeishuAuthResult{
		SessionExpiresAt: time.Now().Add(time.Hour),
		SessionToken:     "session-token",
		User: domain.FeishuUser{
			UnionID: "on-user-1",
			OpenID:  "ou-user-1",
			Name:    "海鸥",
		},
	}}
}

func addTestReleaseSession(req *http.Request) {
	req.AddCookie(&http.Cookie{Name: feishuSessionCookieName, Value: "session-token"})
}

type fakeReleaseService struct {
	record              domain.ReleaseRecord
	route               domain.ReleaseRoute
	records             []domain.ReleaseRecord
	summary             domain.ReleaseRecordSummary
	err                 error
	lastCreateInput     domain.ReleaseCreateInput
	lastListFilters     domain.ReleaseRecordFilters
	lastInactivateInput domain.ReleaseInactivateInput
	lastResolveInput    domain.ReleaseResolveInput
	lastUpdateInput     domain.ReleaseGrayPercentageUpdateInput
}

func (s *fakeReleaseService) CreateRelease(_ context.Context, input domain.ReleaseCreateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	s.lastCreateInput = input
	if s.err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, s.err
	}
	return s.record, s.route, nil
}

func (s *fakeReleaseService) ListReleases(_ context.Context, filters domain.ReleaseRecordFilters) ([]domain.ReleaseRecord, domain.ReleaseRecordSummary, error) {
	s.lastListFilters = filters
	if s.err != nil {
		return nil, domain.ReleaseRecordSummary{}, s.err
	}
	if s.summary.Total == 0 && len(s.records) > 0 {
		s.summary.Total = len(s.records)
	}
	return s.records, s.summary, nil
}

func (s *fakeReleaseService) ResolveReleaseRoute(_ context.Context, input domain.ReleaseResolveInput) (domain.ReleaseRoute, error) {
	s.lastResolveInput = input
	if s.err != nil {
		return domain.ReleaseRoute{}, s.err
	}
	return s.route, nil
}

func (s *fakeReleaseService) UpdateReleaseGrayPercentage(_ context.Context, input domain.ReleaseGrayPercentageUpdateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	s.lastUpdateInput = input
	if s.err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, s.err
	}
	return s.record, s.route, nil
}

func (s *fakeReleaseService) InactivateTargetedGrayRelease(_ context.Context, input domain.ReleaseInactivateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	s.lastInactivateInput = input
	if s.err != nil {
		return domain.ReleaseRecord{}, domain.ReleaseRoute{}, s.err
	}
	return s.record, s.route, nil
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

func testHTTPGrayReleaseRecord(strategy domain.ReleaseGrayStrategy, percentage int, expiresAt *time.Time) domain.ReleaseRecord {
	record := testHTTPReleaseRecord(domain.ReleaseTypeGray)
	record.GrayStrategy = strategy
	record.GrayPercentage = percentage
	record.ExpiresAt = expiresAt
	if strategy == domain.ReleaseGrayStrategyTargeted {
		record.Identifier = "staff-1"
	}
	return record
}

func testHTTPReleaseRoute(routeType domain.ReleaseRouteType, identifier string) domain.ReleaseRoute {
	grayStrategy := domain.ReleaseGrayStrategy("")
	grayPercentage := 0
	if routeType == domain.ReleaseRouteTypeIdentifier {
		grayStrategy = domain.ReleaseGrayStrategyTargeted
	}
	if routeType == domain.ReleaseRouteTypePercentage {
		grayStrategy = domain.ReleaseGrayStrategyPercentage
		grayPercentage = 25
	}
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
		GrayStrategy:    grayStrategy,
		GrayPercentage:  grayPercentage,
		Status:          domain.ReleaseRouteStatusActive,
		ArtifactKey:     "rn-bundles/seaooo/profile/QA/build-1",
		ArtifactURL:     "https://cdn.example.com/rn-bundles/seaooo/profile/QA/build-1",
		CreatedByUserID: "user-1",
		CreatedBy:       "海鸥",
		CreatedAt:       time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC),
	}
}
