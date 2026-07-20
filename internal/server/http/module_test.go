package httpserver

import (
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

func TestListModules(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ModuleService: fakeModuleService{
			modules: []domain.Module{
				testHTTPModule("module-common", domain.ModuleKindCommon),
				testHTTPModule("module-home", domain.ModuleKindFeature),
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modules", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response modulesResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Modules) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(response.Modules))
	}
	if response.Modules[0].RepositoryFullName != "mi-car/common" {
		t.Fatalf("unexpected module response: %#v", response.Modules[0])
	}
	if response.Summary.Total != 2 || response.Summary.Common != 1 || response.Summary.Feature != 1 {
		t.Fatalf("unexpected summary: %+v", response.Summary)
	}
	if response.Pagination.Page != 1 || response.Pagination.PageSize != 20 || response.Pagination.Total != 2 {
		t.Fatalf("unexpected pagination: %+v", response.Pagination)
	}
}

func TestListModulesWithFilters(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ModuleService: fakeModuleService{
			modules: []domain.Module{
				testHTTPModule("module-common", domain.ModuleKindCommon),
				testHTTPModule("module-home", domain.ModuleKindFeature),
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modules?kind=Feature&owner=ou-li-ming&keyword=home", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response modulesResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Modules) != 1 || response.Modules[0].ID != "module-home" {
		t.Fatalf("unexpected filtered modules: %#v", response.Modules)
	}
}

func TestListModulesRejectsInvalidKindFilter(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ModuleService: fakeModuleService{
			listErr: domain.ErrInvalidModuleInput,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modules?kind=Unknown", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestGetModule(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		ModuleService: fakeModuleService{
			module: testHTTPModule("module-home", domain.ModuleKindFeature),
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modules/module-home", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response moduleDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Module.ID != "module-home" || response.Module.RepositoryID != "1001" || response.Module.RepositoryInstallationID != "2001" || response.Module.CommonModuleID != "module-common" {
		t.Fatalf("unexpected module detail: %#v", response.Module)
	}
}

func TestCreateModule(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		ModuleService: fakeModuleService{
			module: testHTTPModule("module-profile", domain.ModuleKindFeature),
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/modules", jsonBody(testModuleRequest("module-profile")))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var response moduleDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Module.ID != "module-profile" || response.Module.OwnerUserID != "ou-li-ming" {
		t.Fatalf("unexpected created module: %#v", response.Module)
	}
}

func TestCreateModuleConflict(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		ModuleService: fakeModuleService{
			createErr: domain.ErrModuleIDConflict,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/modules", jsonBody(testModuleRequest("module-home")))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d: %s", http.StatusConflict, rec.Code, rec.Body.String())
	}
}

func TestCreateModuleCommonConflict(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		ModuleService: fakeModuleService{
			createErr: domain.ErrModuleCommonConflict,
		},
	})

	request := testModuleRequest("module-common")
	request.Kind = domain.ModuleKindCommon
	request.CommonModuleID = ""
	req := httptest.NewRequest(http.MethodPost, "/api/v1/modules", jsonBody(request))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d: %s", http.StatusConflict, rec.Code, rec.Body.String())
	}
}

func TestCreateModuleRepositoryConflict(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		ModuleService: fakeModuleService{
			createErr: domain.ErrModuleRepositoryConflict,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/modules", jsonBody(testModuleRequest("module-profile")))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d: %s", http.StatusConflict, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "module repository already exists") {
		t.Fatalf("expected repository conflict response, got %s", rec.Body.String())
	}
}

func TestUpdateModule(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		ModuleService: fakeModuleService{
			module: testHTTPModule("module-home", domain.ModuleKindFeature),
		},
	})

	request := testModuleRequest("")
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/modules/module-home", jsonBody(request))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response moduleDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Module.CommonModuleName != "common" {
		t.Fatalf("expected common module name, got %q", response.Module.CommonModuleName)
	}
}

func TestDeleteModule(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		ModuleService: fakeModuleService{},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/modules/module-home", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNoContent, rec.Code, rec.Body.String())
	}
}

func TestDeleteModuleNotFound(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		ModuleService: fakeModuleService{
			deleteErr: domain.ErrModuleNotFound,
		},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/modules/unknown", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}

type fakeModuleService struct {
	modules   []domain.Module
	module    domain.Module
	listErr   error
	getErr    error
	createErr error
	updateErr error
	deleteErr error
}

func (s fakeModuleService) ListModules(_ context.Context, filters domain.ModuleListFilters) ([]domain.Module, domain.ModuleSummary, error) {
	if s.listErr != nil {
		return nil, domain.ModuleSummary{}, s.listErr
	}

	modules := make([]domain.Module, 0, len(s.modules))
	for _, module := range s.modules {
		if filters.ApplicationID != "" && module.ApplicationID != filters.ApplicationID {
			continue
		}
		if filters.Kind != "" && module.Kind != filters.Kind {
			continue
		}
		if filters.Owner != "" && module.OwnerUserID != filters.Owner {
			continue
		}
		if filters.Keyword != "" && !strings.Contains(module.Name, filters.Keyword) && !strings.Contains(module.RepositoryFullName, filters.Keyword) {
			continue
		}
		modules = append(modules, module)
	}

	summary := domain.ModuleSummary{Total: len(modules)}
	for _, module := range modules {
		if module.Kind == domain.ModuleKindCommon {
			summary.Common++
		}
		if module.Kind == domain.ModuleKindFeature {
			summary.Feature++
		}
	}
	return modules, summary, nil
}

func (s fakeModuleService) GetModule(context.Context, string) (domain.Module, error) {
	if s.getErr != nil {
		return domain.Module{}, s.getErr
	}
	return s.module, nil
}

func (s fakeModuleService) CreateModule(context.Context, domain.ModuleInput) (domain.Module, error) {
	if s.createErr != nil {
		return domain.Module{}, s.createErr
	}
	return s.module, nil
}

func (s fakeModuleService) UpdateModule(context.Context, string, domain.ModuleInput) (domain.Module, error) {
	if s.updateErr != nil {
		return domain.Module{}, s.updateErr
	}
	return s.module, nil
}

func (s fakeModuleService) DeleteModule(context.Context, string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return nil
}

func testHTTPModule(id string, kind domain.ModuleKind) domain.Module {
	name := strings.TrimPrefix(id, "module-")
	commonModuleID := ""
	commonModuleName := ""
	if kind == domain.ModuleKindFeature {
		commonModuleID = "module-common"
		commonModuleName = "common"
	}

	return domain.Module{
		ApplicationID:            "app-mi-car",
		ApplicationName:          "小米汽车 App",
		ApplicationSlug:          "mi-car",
		CommonModuleID:           commonModuleID,
		CommonModuleName:         commonModuleName,
		CreatedAt:                time.Date(2026, 6, 9, 14, 32, 18, 0, time.UTC),
		DefaultBranch:            "main",
		ID:                       id,
		Kind:                     kind,
		Name:                     name,
		Owner:                    "li.ming",
		OwnerAvatarURL:           "https://example.com/li.ming.png",
		OwnerEmail:               "li.ming@xiaomi.com",
		OwnerUserID:              "ou-li-ming",
		RepositoryCloneURL:       "https://github.com/mi-car/" + name + ".git",
		RepositoryFullName:       "mi-car/" + name,
		RepositoryHTMLURL:        "https://github.com/mi-car/" + name,
		RepositoryID:             1001,
		RepositoryInstallationID: 2001,
		RepositoryProvider:       domain.CodeSourceProviderGitHub,
		RepositorySSHURL:         "git@github.com:mi-car/" + name + ".git",
		UpdatedAt:                time.Date(2026, 6, 9, 14, 40, 18, 0, time.UTC),
	}
}

func testModuleRequest(id string) moduleRequest {
	return moduleRequest{
		ApplicationID:      "app-mi-car",
		CommonModuleID:     "module-common",
		ID:                 id,
		Kind:               domain.ModuleKindFeature,
		Name:               "profile",
		Owner:              "li.ming",
		OwnerAvatarURL:     "https://example.com/li.ming.png",
		OwnerDepartmentIDs: []string{"od-rn-platform"},
		OwnerEmail:         "li.ming@xiaomi.com",
		OwnerUserID:        "ou-li-ming",
		Repository: moduleRepositoryRequest{
			CloneURL:       "https://github.com/mi-car/profile.git",
			DefaultBranch:  "main",
			FullName:       "mi-car/profile",
			HTMLURL:        "https://github.com/mi-car/profile",
			ID:             "1001",
			InstallationID: "2001",
			Provider:       "github",
			SSHURL:         "git@github.com:mi-car/profile.git",
		},
	}
}
