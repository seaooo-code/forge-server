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

func TestListCodeSources(t *testing.T) {
	installedAt := time.Date(2026, 6, 10, 11, 24, 0, 0, time.UTC)
	service := &fakeCodeSourceService{
		apps: []domain.CodeSourceApp{
			{
				ConfigurationStatus: domain.CodeSourceAppConfigurationStatusConfigured,
				Description:         "安装后可读取 GitHub Organization 下授权仓库，用于后续仓库绑定。",
				ID:                  domain.CodeSourceProviderGitHub,
				InstalledAt:         &installedAt,
				Name:                "GitHub App",
				Provider:            domain.CodeSourceProviderGitHub,
				Status:              domain.CodeSourceAppStatusInstalled,
			},
		},
	}
	e := New(config.Config{}, Dependencies{
		CodeSourceService: service,
		FeishuAuthService: testCodeSourceAuthService(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-sources", nil)
	addTestCodeSourceSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response codeSourcesResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(response.Apps))
	}
	if response.Apps[0].Provider != "github" || response.Apps[0].Status != "installed" {
		t.Fatalf("unexpected github app: %+v", response.Apps[0])
	}
	if response.Apps[0].ConfigurationStatus != "configured" {
		t.Fatalf("unexpected github app configuration status: %+v", response.Apps[0])
	}
	if response.Apps[0].InstalledAt != "2026-06-10T11:24:00Z" {
		t.Fatalf("unexpected installed at %q", response.Apps[0].InstalledAt)
	}
	if service.lastListOwnerUnionID != "on-user-1" {
		t.Fatalf("expected owner union id, got %q", service.lastListOwnerUnionID)
	}
}

func TestGetCodeSourceAppManifest(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		CodeSourceService: &fakeCodeSourceService{
			manifest: domain.CodeSourceAppManifest{
				ActionURL: "https://github.com/settings/apps/new",
				Payload:   `{"name":"Forge Console"}`,
			},
		},
		FeishuAuthService: testCodeSourceAuthService(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-sources/github/manifest", nil)
	addTestCodeSourceSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response codeSourceAppManifestResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ActionURL != "https://github.com/settings/apps/new" || response.Manifest != `{"name":"Forge Console"}` {
		t.Fatalf("unexpected manifest response: %+v", response)
	}
}

func TestGetCodeSourceInstallURL(t *testing.T) {
	service := &fakeCodeSourceService{
		installURL: domain.CodeSourceInstallURL{URL: "https://github.com/apps/forge/installations/new"},
	}
	e := New(config.Config{}, Dependencies{
		CodeSourceService: service,
		FeishuAuthService: testCodeSourceAuthService(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-sources/github/install-url", nil)
	addTestCodeSourceSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response codeSourceInstallURLResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.URL != "https://github.com/apps/forge/installations/new" {
		t.Fatalf("unexpected install url %q", response.URL)
	}
	if service.lastInstallURLOwnerUnionID != "on-user-1" {
		t.Fatalf("expected owner union id, got %q", service.lastInstallURLOwnerUnionID)
	}
}

func TestCompleteCodeSourceAppManifestRedirectsToInstallURL(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		CodeSourceService: &fakeCodeSourceService{
			completeManifestURL: domain.CodeSourceInstallURL{URL: "https://github.com/apps/forge/installations/new"},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-sources/github/manifest/callback?code=manifest-code", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusFound, rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Location") != "https://github.com/apps/forge/installations/new" {
		t.Fatalf("unexpected redirect location %q", rec.Header().Get("Location"))
	}
}

func TestCompleteCodeSourceInstallation(t *testing.T) {
	service := &fakeCodeSourceService{
		app: domain.CodeSourceApp{
			ID:       domain.CodeSourceProviderGitHub,
			Name:     "GitHub App",
			Provider: domain.CodeSourceProviderGitHub,
			Status:   domain.CodeSourceAppStatusInstalled,
		},
	}
	e := New(config.Config{
		GitHub: config.GitHubConfig{InstallRedirectURL: "http://127.0.0.1:5173/code-sources"},
	}, Dependencies{
		CodeSourceService: service,
		FeishuAuthService: testCodeSourceAuthService(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-sources/github/install/callback?installation_id=1001&setup_action=install", nil)
	addTestCodeSourceSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusFound, rec.Code, rec.Body.String())
	}

	if rec.Header().Get("Location") != "http://127.0.0.1:5173/code-sources" {
		t.Fatalf("unexpected redirect location %q", rec.Header().Get("Location"))
	}
	if service.lastCompleteOwnerUnionID != "on-user-1" {
		t.Fatalf("expected owner union id, got %q", service.lastCompleteOwnerUnionID)
	}
}

func TestSearchCodeSourceRepositories(t *testing.T) {
	updatedAt := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	service := &fakeCodeSourceService{
		searchResult: domain.CodeSourceRepositorySearchResult{
			Repositories: []domain.CodeSourceRepository{
				{
					DefaultBranch:  "main",
					FullName:       "mi-car/home",
					HTMLURL:        "https://github.com/mi-car/home",
					InstallationID: 1001,
					Name:           "home",
					Owner:          "mi-car",
					Private:        true,
					Provider:       domain.CodeSourceProviderGitHub,
					RepositoryID:   2001,
					SSHURL:         "git@github.com:mi-car/home.git",
					UpdatedAt:      &updatedAt,
				},
			},
		},
	}
	e := New(config.Config{}, Dependencies{
		CodeSourceService: service,
		FeishuAuthService: testCodeSourceAuthService(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-sources/github/repositories/search?query=home&pageSize=10", nil)
	addTestCodeSourceSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	if service.lastSearchInput.OwnerUnionID != "on-user-1" || service.lastSearchInput.Provider != domain.CodeSourceProviderGitHub || service.lastSearchInput.Query != "home" || service.lastSearchInput.PageSize != 10 {
		t.Fatalf("unexpected search input: %+v", service.lastSearchInput)
	}

	var response codeSourceRepositorySearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Repositories) != 1 || response.Repositories[0].FullName != "mi-car/home" {
		t.Fatalf("unexpected repositories: %+v", response.Repositories)
	}
	if response.Repositories[0].ID != "2001" || response.Repositories[0].UpdatedAt != "2026-06-12T10:00:00Z" {
		t.Fatalf("unexpected repository response: %+v", response.Repositories[0])
	}
	if response.Repositories[0].InstallationID != "1001" {
		t.Fatalf("unexpected repository installation id: %+v", response.Repositories[0])
	}
}

func TestSearchCodeSourceRepositoriesRejectsInvalidPageSize(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		CodeSourceService: &fakeCodeSourceService{},
		FeishuAuthService: testCodeSourceAuthService(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-sources/github/repositories/search?pageSize=bad", nil)
	addTestCodeSourceSession(req)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestListCodeSourcesRejectsMissingSession(t *testing.T) {
	e := New(config.Config{}, Dependencies{
		CodeSourceService: &fakeCodeSourceService{},
		FeishuAuthService: testCodeSourceAuthService(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-sources", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

type fakeCodeSourceService struct {
	app                        domain.CodeSourceApp
	apps                       []domain.CodeSourceApp
	completeErr                error
	completeManifestURL        domain.CodeSourceInstallURL
	deleteErr                  error
	installURL                 domain.CodeSourceInstallURL
	installURLErr              error
	lastCompleteOwnerUnionID   string
	lastDeleteOwnerUnionID     string
	lastInstallURLOwnerUnionID string
	lastListOwnerUnionID       string
	lastSearchInput            domain.CodeSourceRepositorySearchInput
	listErr                    error
	manifest                   domain.CodeSourceAppManifest
	manifestErr                error
	searchErr                  error
	searchResult               domain.CodeSourceRepositorySearchResult
}

func (s *fakeCodeSourceService) CompleteAppManifest(_ context.Context, _ domain.CodeSourceProvider, _ string) (domain.CodeSourceInstallURL, error) {
	return s.completeManifestURL, nil
}

func (s *fakeCodeSourceService) CompleteInstallation(_ context.Context, _ domain.CodeSourceProvider, ownerUnionID string, _ int64, _ string) (domain.CodeSourceApp, error) {
	s.lastCompleteOwnerUnionID = ownerUnionID
	if s.completeErr != nil {
		return domain.CodeSourceApp{}, s.completeErr
	}
	return s.app, nil
}

func (s *fakeCodeSourceService) DeleteInstallations(_ context.Context, _ domain.CodeSourceProvider, ownerUnionID string) error {
	s.lastDeleteOwnerUnionID = ownerUnionID
	return s.deleteErr
}

func (s *fakeCodeSourceService) GetAppManifest(_ context.Context, _ domain.CodeSourceProvider) (domain.CodeSourceAppManifest, error) {
	if s.manifestErr != nil {
		return domain.CodeSourceAppManifest{}, s.manifestErr
	}
	return s.manifest, nil
}

func (s *fakeCodeSourceService) GetInstallURL(_ context.Context, _ domain.CodeSourceProvider, ownerUnionID string) (domain.CodeSourceInstallURL, error) {
	s.lastInstallURLOwnerUnionID = ownerUnionID
	if s.installURLErr != nil {
		return domain.CodeSourceInstallURL{}, s.installURLErr
	}
	return s.installURL, nil
}

func (s *fakeCodeSourceService) ListCodeSources(_ context.Context, ownerUnionID string) ([]domain.CodeSourceApp, error) {
	s.lastListOwnerUnionID = ownerUnionID
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.apps, nil
}

func (s *fakeCodeSourceService) SearchRepositories(_ context.Context, input domain.CodeSourceRepositorySearchInput) (domain.CodeSourceRepositorySearchResult, error) {
	s.lastSearchInput = input
	if s.searchErr != nil {
		return domain.CodeSourceRepositorySearchResult{}, s.searchErr
	}
	return s.searchResult, nil
}

func testCodeSourceAuthService() *fakeFeishuAuthService {
	return &fakeFeishuAuthService{
		result: domain.FeishuAuthResult{
			SessionExpiresAt: time.Now().Add(time.Hour),
			SessionToken:     "session-token",
			User: domain.FeishuUser{
				UnionID: "on-user-1",
				Name:    "张三",
			},
		},
	}
}

func addTestCodeSourceSession(req *http.Request) {
	req.AddCookie(&http.Cookie{Name: feishuSessionCookieName, Value: "session-token"})
}
