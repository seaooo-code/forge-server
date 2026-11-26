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
	if response.Module.ID != "module-home" || response.Module.RepositoryID != "1001" || response.Module.CodeSourceConnectionID != "code-source-1" ||
		response.Module.RepositoryProvider != "gitlab" || response.Module.CommonModuleID != "module-common" {
		t.Fatalf("unexpected module detail: %#v", response.Module)
	}
	if response.Module.BuildConfig.DockerfilePath != "Dockerfile" || response.Module.BuildConfig.AutoBuildEnvironment != "qa" ||
		len(response.Module.BuildConfig.AutoBuildBranches) != 1 {
		t.Fatalf("unexpected module detail: %#v", response.Module)
	}
}

func TestCreateModule(t *testing.T) {
	var lastInput domain.ModuleInput
	e := newAuthorizedTestServer(Dependencies{
		ModuleService: fakeModuleService{
			module:    testHTTPModule("module-profile", domain.ModuleKindFeature),
			lastInput: &lastInput,
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
	if lastInput.Repository.ConnectionID != "code-source-1" || lastInput.Repository.ExternalRepositoryID != "1001" ||
		lastInput.BuildConfig.DockerfilePath != "apps/home/Dockerfile" || lastInput.BuildConfig.AutoBuildEnvironment != "qa" ||
		len(lastInput.BuildConfig.AutoBuildBranches) != 2 {
		t.Fatalf("unexpected module input: %#v", lastInput)
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
	modules    []domain.Module
	module     domain.Module
	listErr    error
	getErr     error
	createErr  error
	updateErr  error
	deleteErr  error
	lastInput  *domain.ModuleInput
	branches   []string
	branchErr  error
	commit     domain.BranchCommit
	commitErr  error
	comparison domain.CommitComparison
}

func (s fakeModuleService) CompareCommits(_ context.Context, _ string, from string, to string) (domain.CommitComparison, error) {
	if from == "" || to == "" {
		return domain.CommitComparison{}, domain.ErrInvalidCodeSourceInput
	}
	return s.comparison, nil
}

func (s fakeModuleService) LatestBranchCommit(_ context.Context, _ string, branch string) (domain.BranchCommit, error) {
	if s.commitErr != nil {
		return domain.BranchCommit{}, s.commitErr
	}
	commit := s.commit
	commit.Branch = branch
	return commit, nil
}

func (s fakeModuleService) SearchBranches(context.Context, string, string) ([]string, error) {
	return s.branches, s.branchErr
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
		if filters.Keyword != "" && !strings.Contains(module.Name, filters.Keyword) && !strings.Contains(module.Repository.FullName, filters.Keyword) {
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

func (s fakeModuleService) CreateModule(_ context.Context, input domain.ModuleInput) (domain.Module, error) {
	if s.lastInput != nil {
		*s.lastInput = input
	}
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
		ApplicationID:    "app-mi-car",
		ApplicationName:  "小米汽车 App",
		ApplicationSlug:  "mi-car",
		CommonModuleID:   commonModuleID,
		CommonModuleName: commonModuleName,
		BuildConfig: domain.ModuleBuildConfig{
			AutoBuildBranches:    []string{"main"},
			AutoBuildEnvironment: "qa",
			BuildContext:         ".",
			DockerfilePath:       "Dockerfile",
		},
		CodeSourceProvider: domain.CodeSourceProviderGitLab,
		ConnectionName:     "GitLab",
		CreatedAt:          time.Date(2026, 6, 9, 14, 32, 18, 0, time.UTC),
		ID:                 id,
		Kind:               kind,
		Name:               name,
		Owner:              "li.ming",
		OwnerAvatarURL:     "https://example.com/li.ming.png",
		OwnerEmail:         "li.ming@xiaomi.com",
		OwnerUserID:        "ou-li-ming",
		Repository: domain.ModuleRepository{
			ConnectionID:         "code-source-1",
			ExternalRepositoryID: "1001",
			FullName:             "mi-car/" + name,
			WebURL:               "https://gitlab.example.com/mi-car/" + name,
			CloneURL:             "https://gitlab.example.com/mi-car/" + name + ".git",
			DefaultBranch:        "main",
		},
		UpdatedAt: time.Date(2026, 6, 9, 14, 40, 18, 0, time.UTC),
	}
}

func testModuleRequest(id string) moduleRequest {
	return moduleRequest{
		ApplicationID:  "app-mi-car",
		CommonModuleID: "module-common",
		ID:             id,
		Kind:           domain.ModuleKindFeature,
		Name:           "profile",
		OwnerUserID:    "ou-li-ming",
		Repository: moduleRepositoryRequest{
			ConnectionID:         "code-source-1",
			ExternalRepositoryID: "1001",
		},
		BuildConfig: moduleBuildConfigRequest{
			AutoBuildBranches:    []string{"main", "release/*"},
			AutoBuildEnvironment: "qa",
			DockerfilePath:       "apps/home/Dockerfile",
		},
	}
}

func TestSearchModuleBranches(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		ModuleService: fakeModuleService{branches: []string{"main", "feature/login"}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modules/module-home/branches?query=feat", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	var response moduleBranchesResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Branches) != 2 || response.Branches[1] != "feature/login" {
		t.Fatalf("unexpected branches: %#v", response.Branches)
	}
}

func TestSearchModuleBranchesMapsErrors(t *testing.T) {
	cases := []struct {
		err    error
		status int
	}{
		{domain.ErrModuleNotFound, http.StatusNotFound},
		{domain.ErrCodeSourceConnectionInactive, http.StatusBadRequest},
		{domain.ErrCodeSourceAuthorization, http.StatusBadGateway},
	}
	for _, tc := range cases {
		e := newAuthorizedTestServer(Dependencies{ModuleService: fakeModuleService{branchErr: tc.err}})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/modules/module-home/branches", nil)
		addTestConsoleSession(req)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%v: expected status %d, got %d: %s", tc.err, tc.status, rec.Code, rec.Body.String())
		}
	}
}

func TestGetModuleBranchCommit(t *testing.T) {
	committedAt := time.Date(2026, 11, 25, 6, 30, 0, 0, time.UTC)
	e := newAuthorizedTestServer(Dependencies{
		ModuleService: fakeModuleService{commit: domain.BranchCommit{
			SHA:         "7b628206c7e95429b62c3ce857b4e938a9cdd87b",
			Title:       "新增专注计时页面",
			AuthorName:  "seaooo",
			CommittedAt: committedAt,
			WebURL:      "https://git.example.com/demo/-/commit/7b62820",
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modules/module-home/branch-commit?branch=feature%2Flogin", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	var response moduleBranchCommitResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Commit.Branch != "feature/login" || response.Commit.SHA != "7b628206c7e95429b62c3ce857b4e938a9cdd87b" ||
		response.Commit.CommittedAt != "2026-11-25T06:30:00Z" || response.Commit.AuthorName != "seaooo" || response.Commit.Title == "" || response.Commit.WebURL == "" {
		t.Fatalf("unexpected commit: %+v", response.Commit)
	}
}

func TestGetModuleBranchCommitMapsErrors(t *testing.T) {
	cases := []struct {
		err    error
		status int
	}{
		{domain.ErrModuleNotFound, http.StatusNotFound},
		{domain.ErrCodeSourceBranchNotFound, http.StatusNotFound},
		{domain.ErrInvalidCodeSourceInput, http.StatusBadRequest},
		{domain.ErrCodeSourceAuthorization, http.StatusBadGateway},
	}
	for _, tc := range cases {
		e := newAuthorizedTestServer(Dependencies{ModuleService: fakeModuleService{commitErr: tc.err}})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/modules/module-home/branch-commit?branch=main", nil)
		addTestConsoleSession(req)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%v: expected status %d, got %d: %s", tc.err, tc.status, rec.Code, rec.Body.String())
		}
	}
}

func TestCompareModuleCommits(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		ModuleService: fakeModuleService{comparison: domain.CommitComparison{
			Status:       domain.CommitComparisonAhead,
			Commits:      []domain.BranchCommit{{SHA: "bbb", Title: "新增登录页", AuthorName: "Chen", CommittedAt: time.Date(2026, 11, 25, 6, 30, 0, 0, time.UTC)}},
			TotalCommits: 1,
			TotalExact:   true,
			WebURL:       "https://git.example.com/demo/-/compare/aaa...bbb",
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modules/module-home/compare?from=aaa&to=bbb", nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	var response moduleCommitComparisonResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	got := response.Comparison
	if got.Status != "ahead" || got.TotalCommits != 1 || !got.TotalExact || got.WebURL == "" || len(got.Commits) != 1 || got.BehindCommits == nil ||
		got.Commits[0].Title != "新增登录页" || got.Commits[0].CommittedAt != "2026-11-25T06:30:00Z" {
		t.Fatalf("unexpected comparison: %+v", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/modules/module-home/compare?from=aaa", nil)
	addTestConsoleSession(req)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without to, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateModuleWithBuildVariables(t *testing.T) {
	var lastInput domain.ModuleInput
	e := newAuthorizedTestServer(Dependencies{
		ModuleService: fakeModuleService{
			module:    testHTTPModule("module-profile", domain.ModuleKindFeature),
			lastInput: &lastInput,
		},
	})

	body := map[string]any{}
	raw, _ := json.Marshal(testModuleRequest("module-profile"))
	_ = json.Unmarshal(raw, &body)
	body["buildVariables"] = []map[string]any{
		{"key": "YRN_KIND", "value": "business"},
		{"key": "NPM_TOKEN", "value": "secret-token", "isSecret": true, "environmentCode": "qa"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/modules", jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}
	variables := lastInput.BuildVariables
	if len(variables) != 2 {
		t.Fatalf("expected build variables passed to the service, got %#v", variables)
	}
	if variables[0].Key != "YRN_KIND" || variables[0].Value != "business" || variables[0].IsSecret {
		t.Fatalf("unexpected plain variable: %#v", variables[0])
	}
	if variables[1].Key != "NPM_TOKEN" || !variables[1].IsSecret || variables[1].EnvironmentCode != "qa" {
		t.Fatalf("unexpected secret variable: %#v", variables[1])
	}
	for _, variable := range variables {
		if variable.UpdatedByUserID == "" {
			t.Fatalf("updated-by user must come from the console session, got %#v", variable)
		}
	}
}

func TestCreateModuleRejectsInvalidBuildVariables(t *testing.T) {
	for err, want := range map[error]string{
		domain.ErrInvalidBuildVariableInput: "invalid build variable request",
		domain.ErrEnvironmentNotFound:       "build variable environment not found",
	} {
		e := newAuthorizedTestServer(Dependencies{ModuleService: fakeModuleService{createErr: err}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/modules", jsonBody(testModuleRequest("module-profile")))
		req.Header.Set("Content-Type", "application/json")
		addTestConsoleSession(req)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("expected 400 %q for %v, got %d: %s", want, err, rec.Code, rec.Body.String())
		}
	}
}
