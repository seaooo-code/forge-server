package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

type fakeReleaseHealthService struct {
	lastReport domain.LaunchReport
	err        error
}

func (s *fakeReleaseHealthService) RecordLaunchReport(_ context.Context, report domain.LaunchReport) (int, error) {
	s.lastReport = report
	return len(report.Events), s.err
}

func (s *fakeReleaseHealthService) GetReleaseHealth(context.Context, string) (domain.ReleaseHealth, error) {
	return domain.ReleaseHealth{}, nil
}

func (s *fakeReleaseHealthService) ListReleaseHealthPolicies(context.Context, string) ([]domain.ReleaseHealthPolicy, error) {
	return nil, nil
}

func (s *fakeReleaseHealthService) UpdateReleaseHealthPolicy(context.Context, string, domain.ReleaseHealthPolicy) ([]domain.ReleaseHealthPolicy, error) {
	return nil, nil
}

func TestLaunchReportRequiresTokenAndPassesEvents(t *testing.T) {
	service := &fakeReleaseHealthService{}
	deps := testArtifactDependencies(&fakeArtifactService{})
	deps.ReleaseHealthService = service
	e := New(config.Config{}, deps)
	body := `{"platform":"android","runtimeVersion":"rt-1","channel":"beta","userId":"10001","events":[
		{"moduleId":"claw","buildRecordId":"c4938f50-7fc8-4187-a322-412211b4f905","releaseId":"28e76366-7b15-43e6-b00d-f9882fea10d0","outcome":"launch_failed","reason":"js_exception","occurredAt":"2026-11-24T08:01:02Z"},
		{"moduleId":"claw","buildRecordId":"c4938f50-7fc8-4187-a322-412211b4f905","outcome":"launched","occurredAt":"yesterday"}]}`
	path := "/api/v1/artifacts/applications/forge-demo/environments/test/launch-reports"

	unauthorized := httptest.NewRecorder()
	e.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addTestArtifactBearer(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"accepted":2`) {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	report := service.lastReport
	if report.ApplicationSlug != "forge-demo" || report.Environment != "test" || report.Channel != "beta" || report.UserID != "10001" || len(report.Events) != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if event := report.Events[0]; event.Outcome != domain.LaunchOutcomeLaunchFailed || event.Reason != "js_exception" ||
		!event.OccurredAt.Equal(time.Date(2026, 11, 24, 8, 1, 2, 0, time.UTC)) {
		t.Fatalf("unexpected first event: %+v", event)
	}
	// 时间格式不对的事件交给服务层按过期丢弃。
	if !report.Events[1].OccurredAt.Before(time.Now().Add(-domain.LaunchReportMaxAge)) {
		t.Fatalf("malformed occurredAt should become an expired time, got %v", report.Events[1].OccurredAt)
	}

	service.err = domain.ErrInvalidLaunchReport
	bad := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"events":[]}`))
	bad.Header.Set("Content-Type", "application/json")
	addTestArtifactBearer(bad)
	badRec := httptest.NewRecorder()
	e.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid report, got %d", badRec.Code)
	}
}
