package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if response.Applications[0].Status != "running" {
		t.Fatalf("expected running status, got %q", response.Applications[0].Status)
	}
	if response.Applications[0].OwnerUserID != "ou-li-ming" || response.Applications[0].OwnerEmail == "" || response.Applications[0].OwnerAvatarURL == "" {
		t.Fatalf("expected owner profile fields, got %#v", response.Applications[0])
	}
	if response.Applications[2].Status != "offline" {
		t.Fatalf("expected offline status, got %q", response.Applications[2].Status)
	}
	if response.Summary.Total != 3 || response.Summary.Running != 1 || response.Summary.Maintenance != 1 || response.Summary.Offline != 1 {
		t.Fatalf("unexpected summary: %+v", response.Summary)
	}
}

func TestListApplicationsWithFilters(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ApplicationService: fakeApplicationService{
			applications: []domain.Application{
				testHTTPApplication("mi-car", domain.ApplicationStatusRunning),
				testHTTPApplication("mi-pay", domain.ApplicationStatusMaintenance),
				testHTTPApplication("mi-tv", domain.ApplicationStatusOffline),
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications?platform=iOS&status=running&owner=ou-li-ming&keyword=汽车", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response applicationsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(response.Applications) != 1 {
		t.Fatalf("expected 1 application, got %d", len(response.Applications))
	}
	if response.Applications[0].Slug != "mi-car" {
		t.Fatalf("expected mi-car application, got %q", response.Applications[0].Slug)
	}
	if response.Summary.Total != 1 || response.Summary.Running != 1 {
		t.Fatalf("unexpected summary: %+v", response.Summary)
	}
}

func TestListApplicationsRejectsInvalidStatusFilter(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ApplicationService: fakeApplicationService{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications?status=unknown", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
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
	if response.Detail.OwnerUserID != "ou-li-ming" || response.Detail.OwnerEmail == "" || response.Detail.OwnerAvatarURL == "" {
		t.Fatalf("expected owner profile fields, got %#v", response.Detail)
	}
	if len(response.Detail.OwnerDepartmentIDs) != 1 || response.Detail.OwnerDepartmentIDs[0] != "od-rn-platform" {
		t.Fatalf("expected owner department ids, got %#v", response.Detail.OwnerDepartmentIDs)
	}
	if len(response.Detail.Environments) != 2 {
		t.Fatalf("expected environments, got %d", len(response.Detail.Environments))
	}
	if response.Detail.Environments[1].Status != "gray" {
		t.Fatalf("expected gray environment status, got %q", response.Detail.Environments[1].Status)
	}
	if len(response.Detail.Members) != 1 {
		t.Fatalf("expected members, got %d", len(response.Detail.Members))
	}
	if response.Detail.Modules == nil {
		t.Fatalf("expected empty module array")
	}
	if response.Detail.ClientIntegration.Endpoint.Path != "/api/v1/artifacts/applications/mi-car/environments/{environment}/manifest" {
		t.Fatalf("expected client manifest path, got %#v", response.Detail.ClientIntegration.Endpoint)
	}
	if len(response.Detail.ClientIntegration.Endpoint.RequestFields) != 1 || response.Detail.ClientIntegration.Endpoint.RequestFields[0].Name != "identifier" {
		t.Fatalf("expected client request field descriptions, got %#v", response.Detail.ClientIntegration.Endpoint.RequestFields)
	}
	if len(response.Detail.ClientIntegration.Endpoint.ResponseFields) != 1 || response.Detail.ClientIntegration.Endpoint.ResponseFields[0].Name != "modules" {
		t.Fatalf("expected client response field descriptions, got %#v", response.Detail.ClientIntegration.Endpoint.ResponseFields)
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
	if len(response.Owners) != 1 || response.Owners[0].Value != "ou-li-ming" {
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
	if response.Application.OwnerUserID != "ou-li-ming" {
		t.Fatalf("expected owner user id, got %q", response.Application.OwnerUserID)
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
	if response.Application.Status != "maintenance" {
		t.Fatalf("expected maintenance status, got %q", response.Application.Status)
	}
}

func TestUpdateApplicationStatus(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ApplicationService: fakeApplicationService{
			detail: testHTTPApplicationDetail("mi-car", domain.ApplicationStatusRunning),
		},
	})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/applications/mi-car/status", jsonBody(applicationStatusRequest{
		Status: domain.ApplicationStatusRunning,
	}))
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
	if response.Application.Status != "running" {
		t.Fatalf("expected running status, got %q", response.Application.Status)
	}
}

func TestCheckApplicationDelete(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ApplicationService: fakeApplicationService{
			deleteCheck: domain.ApplicationDeleteCheck{
				Deletable: false,
				Dependencies: []domain.ApplicationDeleteDependency{
					{
						ID:          "app-mi-car:modules",
						Name:        "接入模块",
						Type:        "module",
						Description: "当前应用仍有 2 个接入模块",
					},
				},
				Message: "应用仍有关联模块，不能删除",
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/mi-car/delete-check", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response applicationDeleteCheckResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Deletable || len(response.Dependencies) != 1 || response.Dependencies[0].Type != "module" {
		t.Fatalf("unexpected delete check response: %#v", response)
	}
}

func TestDeleteApplicationHardDeletes(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ApplicationService: fakeApplicationService{},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/applications/mi-car", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNoContent, rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty response body, got %q", rec.Body.String())
	}
}

func TestDeleteApplicationRejectsDependencies(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ApplicationService: fakeApplicationService{
			deleteErr: domain.ErrApplicationInUse,
		},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/applications/mi-car", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d: %s", http.StatusConflict, rec.Code, rec.Body.String())
	}
}

type fakeApplicationService struct {
	applications []domain.Application
	detail       domain.ApplicationDetail
	deleteCheck  domain.ApplicationDeleteCheck
	detailErr    error
	createErr    error
	updateErr    error
	statusErr    error
	deleteErr    error
}

func (s fakeApplicationService) ListApplications(_ context.Context, filters domain.ApplicationListFilters) ([]domain.Application, domain.ApplicationSummary, error) {
	if filters.Status != "" && !domain.IsValidApplicationStatus(filters.Status) {
		return nil, domain.ApplicationSummary{}, domain.ErrInvalidApplicationInput
	}

	applications := make([]domain.Application, 0, len(s.applications))
	for _, application := range s.applications {
		if filters.Platform != "" && !applicationSupportsPlatform(application.Platform, filters.Platform) {
			continue
		}
		if filters.Status != "" && application.Status != filters.Status {
			continue
		}
		if filters.Owner != "" && application.OwnerUserID != filters.Owner {
			continue
		}
		if filters.Keyword != "" && !strings.Contains(application.Name, filters.Keyword) {
			continue
		}
		applications = append(applications, application)
	}

	summary := domain.ApplicationSummary{Total: len(applications)}
	for _, application := range applications {
		switch application.Status {
		case domain.ApplicationStatusRunning:
			summary.Running++
		case domain.ApplicationStatusMaintenance:
			summary.Maintenance++
		case domain.ApplicationStatusOffline:
			summary.Offline++
		}
	}
	return applications, summary, nil
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
			{Label: "li.ming", Value: "ou-li-ming"},
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

func (s fakeApplicationService) UpdateApplicationStatus(context.Context, string, domain.ApplicationStatus) (domain.ApplicationDetail, error) {
	if s.statusErr != nil {
		return domain.ApplicationDetail{}, s.statusErr
	}
	return s.detail, nil
}

func (s fakeApplicationService) CheckApplicationDelete(context.Context, string) (domain.ApplicationDeleteCheck, error) {
	return s.deleteCheck, nil
}

func (s fakeApplicationService) DeleteApplication(context.Context, string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return nil
}

func testHTTPApplication(slug string, status domain.ApplicationStatus) domain.Application {
	name := "小米汽车"
	if slug != "mi-car" {
		name = slug
	}

	return domain.Application{
		ID:                 "app-" + slug,
		Name:               name,
		Slug:               slug,
		Platform:           "iOS / Android",
		OwnerUserID:        "ou-li-ming",
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

func applicationSupportsPlatform(platforms string, filter string) bool {
	for _, platform := range strings.Split(platforms, "/") {
		if strings.TrimSpace(platform) == filter {
			return true
		}
	}
	return false
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
		OwnerUserID:        application.OwnerUserID,
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
				Traffic:     "-",
				UpdatedAt:   time.Date(2026, 6, 9, 14, 32, 18, 0, time.UTC),
				Status:      domain.ApplicationEnvironmentStatusHealthy,
			},
			{
				ID:          "product",
				Environment: "product",
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
		ClientIntegration: domain.ClientIntegrationInfo{
			Endpoint: domain.ClientIntegrationEndpoint{
				BaseURL:     "https://forge.example.com",
				Path:        "/api/v1/artifacts/applications/mi-car/environments/{environment}/manifest",
				Method:      http.MethodGet,
				Description: "客户端获取指定应用环境下模块 manifest 的接口",
				ExampleURL:  "https://forge.example.com/api/v1/artifacts/applications/mi-car/environments/product/manifest?identifier=user-1",
				RequestFields: []domain.ClientIntegrationField{
					{Name: "identifier", Description: "定向灰度标识", Required: false, Type: "string", Example: "user-1"},
				},
				ResponseFields: []domain.ClientIntegrationField{
					{Name: "modules", Description: "模块 manifest 列表", Required: true, Type: "array", Example: "[]"},
				},
			},
		},
	}
}

func testApplicationRequest(slug string) applicationRequest {
	return applicationRequest{
		Slug:               slug,
		Name:               "小米汽车海外版",
		Platform:           "iOS / Android",
		OwnerUserID:        "ou-li-ming",
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
