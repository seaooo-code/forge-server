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
		route:  testHTTPReleaseRoute(domain.ReleaseRouteTypeDefault),
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

func TestCreatePercentageGrayRelease(t *testing.T) {
	service := &fakeReleaseService{
		record: testHTTPGrayReleaseRecord(domain.ReleaseGrayStrategyPercentage, 25, nil),
		route:  testHTTPReleaseRoute(domain.ReleaseRouteTypePercentage),
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
	service := &fakeReleaseService{
		records: []domain.ReleaseRecord{testHTTPGrayReleaseRecord(domain.ReleaseGrayStrategyPercentage, 30, nil)},
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
		response.Releases[0].GrayPercentage != 30 {
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
		route: testHTTPReleaseRoute(domain.ReleaseRouteTypePercentage),
	}
	e := newAuthorizedTestServer(Dependencies{
		FeishuAuthService: testReleaseAuthService(),
		ReleaseService:    service,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/releases/resolve?moduleId=module-profile&environment=qa&userId=user-1", nil)
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
		service.lastResolveInput.UserID != "user-1" {
		t.Fatalf("unexpected resolve input: %+v", service.lastResolveInput)
	}
}

func TestUpdateReleaseGrayPercentage(t *testing.T) {
	service := &fakeReleaseService{
		record: testHTTPGrayReleaseRecord(domain.ReleaseGrayStrategyPercentage, 100, nil),
		route:  testHTTPReleaseRoute(domain.ReleaseRouteTypeDefault),
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
		GrayStrategy:  "percentage",
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
	record           domain.ReleaseRecord
	route            domain.ReleaseRoute
	records          []domain.ReleaseRecord
	summary          domain.ReleaseRecordSummary
	err              error
	lastCreateInput  domain.ReleaseCreateInput
	lastListFilters  domain.ReleaseRecordFilters
	lastResolveInput domain.ReleaseResolveInput
	lastUpdateInput  domain.ReleaseGrayPercentageUpdateInput
	lastDeactivate   domain.ReleaseDeactivateInput
	lastRollback     domain.ReleaseRollbackInput
	events           []domain.ReleaseEvent
}

func (s *fakeReleaseService) DeactivateRelease(_ context.Context, input domain.ReleaseDeactivateInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	s.lastDeactivate = input
	return s.record, s.route, s.err
}

func (s *fakeReleaseService) RollbackRelease(_ context.Context, input domain.ReleaseRollbackInput) (domain.ReleaseRecord, domain.ReleaseRoute, error) {
	s.lastRollback = input
	return s.record, s.route, s.err
}

func (s *fakeReleaseService) ListReleaseEvents(_ context.Context, _ string) ([]domain.ReleaseEvent, error) {
	return s.events, s.err
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
	return record
}

func testHTTPReleaseRoute(routeType domain.ReleaseRouteType) domain.ReleaseRoute {
	grayStrategy := domain.ReleaseGrayStrategy("")
	grayPercentage := 0
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

func TestCreateReleaseReturnsCapabilityConflicts(t *testing.T) {
	provided := int64(2)
	e := newAuthorizedTestServer(Dependencies{
		FeishuAuthService: testReleaseAuthService(),
		ReleaseService: &fakeReleaseService{err: &domain.ReleaseCapabilityConflictError{Conflicts: []domain.ReleaseCapabilityConflict{{
			ModuleID: "module-common", ModuleName: "common", ReleaseID: "release-common", ReleaseType: domain.ReleaseTypeFull,
			BuildRecordID: "build-common", BuildNumber: 7, Reason: domain.ReleaseCapabilityUnmet,
			Shortfalls: []domain.CapabilityShortfall{{Name: "commonApi", Required: 3, Provided: &provided}, {Name: "bridge", Required: 1}},
		}}}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/releases", jsonBody(releaseCreateRequest{BuildRecordID: "build-1", ReleaseType: "full"}))
	req.Header.Set("Content-Type", "application/json")
	addTestReleaseSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var response releaseCapabilityConflictErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.Error != "release capability conflict" || len(response.Conflicts) != 1 || response.Conflicts[0].BuildNumber != 7 ||
		len(response.Conflicts[0].Shortfalls) != 2 || *response.Conflicts[0].Shortfalls[0].Provided != 2 || response.Conflicts[0].Shortfalls[1].Provided != nil {
		t.Fatalf("unexpected conflict response: %+v", response)
	}
}

func TestDeactivateAndRollbackRelease(t *testing.T) {
	service := &fakeReleaseService{record: testHTTPReleaseRecord(domain.ReleaseTypeFull), route: testHTTPReleaseRoute(domain.ReleaseRouteTypeDefault)}
	e := newAuthorizedTestServer(Dependencies{FeishuAuthService: testReleaseAuthService(), ReleaseService: service})
	post := func(path string, body any) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, jsonBody(body))
		req.Header.Set("Content-Type", "application/json")
		addTestReleaseSession(req)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	if rec := post("/api/v1/releases/release-1/deactivate", releaseDeactivateRequest{Reason: "白屏"}); rec.Code != http.StatusOK {
		t.Fatalf("deactivate: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if service.lastDeactivate.ID != "release-1" || service.lastDeactivate.Reason != "白屏" || service.lastDeactivate.ActorUserID == "" {
		t.Fatalf("unexpected deactivate input: %+v", service.lastDeactivate)
	}
	if rec := post("/api/v1/releases/release-1/rollback", releaseRollbackRequest{Description: "回滚"}); rec.Code != http.StatusCreated {
		t.Fatalf("rollback: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if service.lastRollback.ID != "release-1" || service.lastRollback.Description != "回滚" || service.lastRollback.ActorUserID == "" {
		t.Fatalf("unexpected rollback input: %+v", service.lastRollback)
	}

	service.err = domain.ErrReleaseRollbackUnavailable
	if rec := post("/api/v1/releases/release-1/rollback", releaseRollbackRequest{}); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "release rollback unavailable") {
		t.Fatalf("expected 409 rollback unavailable, got %d: %s", rec.Code, rec.Body.String())
	}
	service.err = domain.ErrReleaseNotActive
	if rec := post("/api/v1/releases/release-1/deactivate", releaseDeactivateRequest{}); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "release not active") {
		t.Fatalf("expected 409 not active, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListReleaseEvents(t *testing.T) {
	service := &fakeReleaseService{events: []domain.ReleaseEvent{
		{ID: 2, Kind: domain.ReleaseEventDeactivated, Detail: map[string]any{"reason": "白屏"}, ActorUserID: "ou-1", ActorName: "海鸥", CreatedAt: time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)},
		{ID: 1, Kind: domain.ReleaseEventReplaced},
	}}
	e := newAuthorizedTestServer(Dependencies{FeishuAuthService: testReleaseAuthService(), ReleaseService: service})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/releases/release-1/events", nil)
	addTestReleaseSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response releaseEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(response.Events) != 2 || response.Events[0].Actor == nil || response.Events[0].Actor.Name != "海鸥" ||
		response.Events[0].Detail["reason"] != "白屏" || response.Events[1].Actor != nil || response.Events[1].Detail == nil {
		t.Fatalf("unexpected events: %+v", response.Events)
	}
}
