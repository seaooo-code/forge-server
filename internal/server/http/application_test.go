package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"
)

func TestListApplications(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ApplicationService: fakeApplicationService{
			applications: []domain.Application{
				testHTTPApplication("mi-car", domain.ApplicationStatusRunning),
				testHTTPApplication("mi-pay", domain.ApplicationStatusMaintenance),
				testHTTPApplication("mi-tv", domain.ApplicationStatusOffline),
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response applicationsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(response.Applications) != 3 {
		t.Fatalf("expected 3 applications, got %d", len(response.Applications))
	}
	if response.Applications[0].Status != "运行中" {
		t.Fatalf("expected running status label, got %q", response.Applications[0].Status)
	}
	if response.Applications[2].Status != "已下线" {
		t.Fatalf("expected offline status label, got %q", response.Applications[2].Status)
	}
	if response.Summary.Total != 3 || response.Summary.Running != 1 || response.Summary.Maintenance != 1 || response.Summary.Offline != 1 {
		t.Fatalf("unexpected summary: %+v", response.Summary)
	}
}

func TestGetApplicationDetail(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ApplicationService: fakeApplicationService{
			detail: testHTTPApplicationDetail("mi-car", domain.ApplicationStatusRunning),
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/mi-car", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response applicationDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Application.Slug != "mi-car" {
		t.Fatalf("expected mi-car application, got %q", response.Application.Slug)
	}
	if response.Detail.AppKey != "rn.mi-car" {
		t.Fatalf("expected app key, got %q", response.Detail.AppKey)
	}
	if len(response.Detail.OwnerDepartmentIDs) != 1 || response.Detail.OwnerDepartmentIDs[0] != "od-rn-platform" {
		t.Fatalf("expected owner department ids, got %#v", response.Detail.OwnerDepartmentIDs)
	}
	if len(response.Detail.Environments) != 2 {
		t.Fatalf("expected environments, got %d", len(response.Detail.Environments))
	}
	if response.Detail.Environments[1].Status != "灰度中" {
		t.Fatalf("expected gray environment label, got %q", response.Detail.Environments[1].Status)
	}
	if len(response.Detail.Members) != 1 {
		t.Fatalf("expected members, got %d", len(response.Detail.Members))
	}
	if response.Detail.Modules == nil || response.Detail.Pipelines == nil {
		t.Fatalf("expected empty module and pipeline arrays")
	}
}

func TestGetApplicationOptions(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ApplicationService: fakeApplicationService{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/options", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response applicationOptionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Platforms) != 3 {
		t.Fatalf("expected 3 platform options, got %d", len(response.Platforms))
	}
	if len(response.Owners) != 1 || response.Owners[0].Value != "li.ming" {
		t.Fatalf("expected owner options, got %#v", response.Owners)
	}
	removedPlatform := "Android" + " TV"
	for _, platform := range response.Platforms {
		if platform.Value == removedPlatform {
			t.Fatalf("did not expect removed platform option")
		}
	}
}

func TestGetApplicationDetailNotFound(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ApplicationService: fakeApplicationService{
			detailErr: domain.ErrApplicationNotFound,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/unknown", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNotFound, rec.Code, rec.Body.String())
	}

	var response map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["error"] != "application not found" {
		t.Fatalf("expected not found error, got %q", response["error"])
	}
}

func TestCreateApplication(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ApplicationService: fakeApplicationService{
			detail: testHTTPApplicationDetail("mi-car-global", domain.ApplicationStatusRunning),
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/applications", jsonBody(testApplicationRequest("mi-car-global")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var response applicationDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Application.Slug != "mi-car-global" {
		t.Fatalf("expected created slug, got %q", response.Application.Slug)
	}
}

func TestCreateApplicationSlugConflict(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ApplicationService: fakeApplicationService{
			createErr: domain.ErrApplicationSlugConflict,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/applications", jsonBody(testApplicationRequest("mi-car")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d: %s", http.StatusConflict, rec.Code, rec.Body.String())
	}
}

func TestUpdateApplication(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ApplicationService: fakeApplicationService{
			detail: testHTTPApplicationDetail("mi-car", domain.ApplicationStatusMaintenance),
		},
	})

	request := testApplicationRequest("")
	request.Status = domain.ApplicationStatusMaintenance
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/applications/mi-car", jsonBody(request))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response applicationDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Application.Status != "维护中" {
		t.Fatalf("expected maintenance status label, got %q", response.Application.Status)
	}
}

func TestDeleteApplicationSoftDeletes(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ApplicationService: fakeApplicationService{
			detail: testHTTPApplicationDetail("mi-car", domain.ApplicationStatusOffline),
		},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/applications/mi-car", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response applicationDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Application.Status != "已下线" {
		t.Fatalf("expected offline status label, got %q", response.Application.Status)
	}
}

type fakeApplicationService struct {
	applications []domain.Application
	detail       domain.ApplicationDetail
	detailErr    error
	createErr    error
	updateErr    error
	deleteErr    error
}

func (s fakeApplicationService) ListApplications(context.Context) ([]domain.Application, domain.ApplicationSummary, error) {
	summary := domain.ApplicationSummary{Total: len(s.applications)}
	for _, application := range s.applications {
		switch application.Status {
		case domain.ApplicationStatusRunning:
			summary.Running++
		case domain.ApplicationStatusMaintenance:
			summary.Maintenance++
		case domain.ApplicationStatusOffline:
			summary.Offline++
		}
	}
	return s.applications, summary, nil
}

func (s fakeApplicationService) GetApplicationDetail(context.Context, string) (domain.ApplicationDetail, error) {
	if s.detailErr != nil {
		return domain.ApplicationDetail{}, s.detailErr
	}
	return s.detail, nil
}

func (s fakeApplicationService) GetApplicationOptions(context.Context) (domain.ApplicationOptions, error) {
	return domain.ApplicationOptions{
		Owners: []domain.ApplicationOptionItem{
			{Label: "li.ming", Value: "li.ming"},
		},
		Platforms: []domain.ApplicationOptionItem{
			{Label: "iOS", Value: "iOS"},
			{Label: "Android", Value: "Android"},
			{Label: "HarmonyOS", Value: "HarmonyOS"},
		},
	}, nil
}

func (s fakeApplicationService) CreateApplication(context.Context, domain.ApplicationInput) (domain.ApplicationDetail, error) {
	if s.createErr != nil {
		return domain.ApplicationDetail{}, s.createErr
	}
	return s.detail, nil
}

func (s fakeApplicationService) UpdateApplication(context.Context, string, domain.ApplicationInput) (domain.ApplicationDetail, error) {
	if s.updateErr != nil {
		return domain.ApplicationDetail{}, s.updateErr
	}
	return s.detail, nil
}

func (s fakeApplicationService) DeleteApplication(context.Context, string) (domain.ApplicationDetail, error) {
	if s.deleteErr != nil {
		return domain.ApplicationDetail{}, s.deleteErr
	}
	return s.detail, nil
}

func testHTTPApplication(slug string, status domain.ApplicationStatus) domain.Application {
	return domain.Application{
		ID:                 "app-" + slug,
		Name:               "小米汽车",
		Slug:               slug,
		Platform:           "iOS / Android",
		Owner:              "li.ming",
		OwnerEmail:         "li.ming@xiaomi.com",
		OwnerAvatarURL:     "https://example.com/li.ming.png",
		OwnerDepartmentIDs: []string{"od-rn-platform"},
		ModuleCount:        12,
		EnvironmentCount:   5,
		CreatedAt:          time.Date(2026, 6, 9, 14, 32, 18, 0, time.UTC),
		Status:             status,
	}
}

func testHTTPApplicationDetail(slug string, status domain.ApplicationStatus) domain.ApplicationDetail {
	application := testHTTPApplication(slug, status)
	return domain.ApplicationDetail{
		Application:        application,
		ID:                 application.ID,
		AppKey:             "rn." + slug,
		Slug:               slug,
		Name:               application.Name,
		Description:        "应用详情",
		Owner:              application.Owner,
		OwnerEmail:         application.OwnerEmail,
		OwnerAvatarURL:     application.OwnerAvatarURL,
		OwnerDepartmentIDs: []string{"od-rn-platform"},
		Metrics: []domain.ApplicationDetailMetric{
			{ID: "platform", Label: "支持平台", Value: "2", Detail: "iOS / Android", Tone: "blue"},
		},
		Environments: []domain.ApplicationEnvironmentBinding{
			{
				ID:          "dev",
				Environment: "dev",
				Combination: slug + "-dev-combo",
				BundleCount: 12,
				Traffic:     "-",
				UpdatedAt:   time.Date(2026, 6, 9, 14, 32, 18, 0, time.UTC),
				Status:      domain.ApplicationEnvironmentStatusHealthy,
			},
			{
				ID:          "product",
				Environment: "product",
				Combination: slug + "-product-combo",
				BundleCount: 12,
				Traffic:     "2.4M 在线",
				UpdatedAt:   time.Date(2026, 6, 9, 14, 20, 18, 0, time.UTC),
				Status:      domain.ApplicationEnvironmentStatusGray,
			},
		},
		Modules: []domain.ApplicationModule{},
		Members: []domain.ApplicationMember{
			{
				ID:   "owner",
				Name: "li.ming",
				Role: domain.ApplicationMemberRoleOwner,
				Team: "小米汽车 - RN 平台组",
			},
		},
		Pipelines: []domain.ApplicationPipeline{},
	}
}

func testApplicationRequest(slug string) applicationRequest {
	return applicationRequest{
		Slug:               slug,
		Name:               "小米汽车海外版",
		Platform:           "iOS / Android",
		Owner:              "li.ming",
		OwnerEmail:         "li.ming@xiaomi.com",
		OwnerAvatarURL:     "https://example.com/li.ming.png",
		OwnerDepartmentIDs: []string{"od-rn-platform"},
		Description:        "海外版宿主应用",
		Status:             domain.ApplicationStatusRunning,
		Environments: []applicationEnvironmentRequest{
			{
				Environment: "dev",
			},
		},
		Members: []applicationMemberRequest{
			{
				Name: "li.ming",
				Role: domain.ApplicationMemberRoleOwner,
				Team: "小米汽车 - RN 平台组",
			},
		},
	}
}

func jsonBody(value any) *bytes.Reader {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return bytes.NewReader(data)
}
