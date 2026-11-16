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

func TestListCodeSources(t *testing.T) {
	installedAt := time.Date(2026, 6, 10, 11, 24, 0, 0, time.UTC)
	service := &fakeCodeSourceService{
		apps: []domain.CodeSourceApp{{
			ConfigurationStatus: domain.CodeSourceAppConfigurationStatusConfigured,
			ConnectionCount:     2,
			ID:                  domain.CodeSourceProviderGitHub,
			InstalledAt:         &installedAt,
			Name:                "GitHub App",
			Provider:            domain.CodeSourceProviderGitHub,
			Status:              domain.CodeSourceAppStatusInstalled,
		}},
	}
	e := newAuthorizedTestServer(Dependencies{CodeSourceService: service})

	rec := serveCodeSourceRequest(e, http.MethodGet, "/api/v1/code-sources", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	var response codeSourcesResponse
	decodeTestJSON(t, rec, &response)
	if len(response.Apps) != 1 || response.Apps[0].Status != "installed" || response.Apps[0].ConnectionCount != 2 ||
		response.Apps[0].InstalledAt != "2026-06-10T11:24:00Z" {
		t.Fatalf("unexpected apps: %+v", response.Apps)
	}
}

func TestListCodeSourcesRejectsMissingSession(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{CodeSourceService: &fakeCodeSourceService{}})

	rec := serveCodeSourceRequest(e, http.MethodGet, "/api/v1/code-sources", nil, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

func TestGitHubAppRoutesRequireManagePermission(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{
		CodeSourceService:    &fakeCodeSourceService{},
		AuthorizationService: fakeAuthorizationService{err: domain.ErrForbidden},
	})

	for _, path := range []string{
		"/api/v1/code-sources/github/manifest",
		"/api/v1/code-sources/github/install-url",
		"/api/v1/code-sources/github/install/callback?installation_id=1001",
	} {
		rec := serveCodeSourceRequest(e, http.MethodGet, path, nil, true)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected %s to be forbidden, got %d: %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestGetCodeSourceAppManifest(t *testing.T) {
	e := newAuthorizedTestServer(Dependencies{CodeSourceService: &fakeCodeSourceService{
		manifest: domain.CodeSourceAppManifest{ActionURL: "https://github.com/settings/apps/new", Payload: `{"name":"Forge"}`},
	}})

	rec := serveCodeSourceRequest(e, http.MethodGet, "/api/v1/code-sources/github/manifest", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	var response codeSourceAppManifestResponse
	decodeTestJSON(t, rec, &response)
	if response.ActionURL != "https://github.com/settings/apps/new" || response.Manifest != `{"name":"Forge"}` {
		t.Fatalf("unexpected manifest: %+v", response)
	}
}

func TestCompleteCodeSourceAppManifestRedirectsToInstallURL(t *testing.T) {
	e := New(config.Config{}, Dependencies{CodeSourceService: &fakeCodeSourceService{
		completeManifestURL: domain.CodeSourceInstallURL{URL: "https://github.com/apps/forge/installations/new"},
	}})

	rec := serveCodeSourceRequest(e, http.MethodGet, "/api/v1/code-sources/github/manifest/callback?code=temp", nil, false)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://github.com/apps/forge/installations/new" {
		t.Fatalf("unexpected redirect: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestCompleteCodeSourceInstallationRecordsPlatformUser(t *testing.T) {
	service := &fakeCodeSourceService{}
	e := newAuthorizedTestServerWithConfig(config.Config{GitHub: config.GitHubConfig{InstallRedirectURL: "http://127.0.0.1:5173/code-sources"}}, Dependencies{CodeSourceService: service})

	rec := serveCodeSourceRequest(e, http.MethodGet, "/api/v1/code-sources/github/install/callback?installation_id=1001&setup_action=install", nil, true)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "http://127.0.0.1:5173/code-sources" {
		t.Fatalf("unexpected response: %d %s", rec.Code, rec.Body.String())
	}
	if service.lastUserID != "ou-test-user" || service.lastInstallationID != 1001 || service.lastSetupAction != "install" {
		t.Fatalf("unexpected installation call: user=%q installation=%d action=%q", service.lastUserID, service.lastInstallationID, service.lastSetupAction)
	}

	rec = serveCodeSourceRequest(e, http.MethodGet, "/api/v1/code-sources/github/install/callback?installation_id=abc", nil, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for invalid installation id, got %d", rec.Code)
	}
}

func TestCreateCodeSourceConnection(t *testing.T) {
	service := &fakeCodeSourceService{connection: testHTTPCodeSourceConnection()}
	e := newAuthorizedTestServer(Dependencies{CodeSourceService: service})

	rec := serveCodeSourceRequest(e, http.MethodPost, "/api/v1/code-source-connections", codeSourceConnectionCreateRequest{
		Provider:   "gitlab",
		Name:       "公司 GitLab",
		WebBaseURL: "https://gitlab.example.com",
		Credential: "glpat-secret-token",
	}, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}
	if service.lastCreate.Provider != domain.CodeSourceProviderGitLab || service.lastCreate.Credential != "glpat-secret-token" || service.lastCreate.CreatedByUserID != "ou-test-user" {
		t.Fatalf("unexpected create input: %+v", service.lastCreate)
	}
	var response codeSourceConnectionDetailResponse
	decodeTestJSON(t, rec, &response)
	if response.Connection.ID != "code-source-1" || response.Connection.CredentialPreview != "glpa****oken" || response.Connection.CredentialExpiresAt != "2027-01-31T00:00:00Z" {
		t.Fatalf("unexpected connection response: %+v", response.Connection)
	}
	for _, secret := range []string{"glpat-secret-token", "enc:", "hook-secret"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("connection response must not expose %q: %s", secret, rec.Body.String())
		}
	}
}

func TestCodeSourceConnectionErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "invalid credential", err: domain.ErrCodeSourceCredentialInvalid, status: http.StatusBadRequest},
		{name: "duplicate", err: domain.ErrCodeSourceConnectionConflict, status: http.StatusConflict},
		{name: "in use", err: domain.ErrCodeSourceConnectionInUse, status: http.StatusConflict},
		{name: "missing", err: domain.ErrCodeSourceConnectionNotFound, status: http.StatusNotFound},
		{name: "upstream", err: domain.ErrCodeSourceAuthorization, status: http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newAuthorizedTestServer(Dependencies{CodeSourceService: &fakeCodeSourceService{err: tt.err}})
			rec := serveCodeSourceRequest(e, http.MethodDelete, "/api/v1/code-source-connections/code-source-1", nil, true)
			if rec.Code != tt.status {
				t.Fatalf("expected status %d, got %d: %s", tt.status, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUpdateCodeSourceConnectionPassesOptionalFields(t *testing.T) {
	service := &fakeCodeSourceService{connection: testHTTPCodeSourceConnection()}
	e := newAuthorizedTestServer(Dependencies{CodeSourceService: service})

	rec := serveCodeSourceRequest(e, http.MethodPatch, "/api/v1/code-source-connections/code-source-1", map[string]string{"status": "disabled"}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if service.lastID != "code-source-1" || service.lastUpdate.Status == nil || *service.lastUpdate.Status != domain.CodeSourceConnectionStatusDisabled ||
		service.lastUpdate.Name != nil || service.lastUpdate.Credential != nil {
		t.Fatalf("unexpected update input: %+v", service.lastUpdate)
	}
}

func TestListCodeSourceConnectionsRequiresUsePermission(t *testing.T) {
	authorization := &recordingAuthorizationService{}
	service := &fakeCodeSourceService{connections: []domain.CodeSourceConnection{testHTTPCodeSourceConnection()}}
	e := newAuthorizedTestServer(Dependencies{CodeSourceService: service, AuthorizationService: authorization})

	rec := serveCodeSourceRequest(e, http.MethodGet, "/api/v1/code-source-connections?provider=gitlab", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if authorization.lastAction != domain.ActionCodeSourceUse || service.lastFilters.Provider != domain.CodeSourceProviderGitLab {
		t.Fatalf("unexpected authorization or filters: %q %+v", authorization.lastAction, service.lastFilters)
	}
	var response codeSourceConnectionsResponse
	decodeTestJSON(t, rec, &response)
	if len(response.Connections) != 1 {
		t.Fatalf("unexpected connections: %+v", response.Connections)
	}

	serveCodeSourceRequest(e, http.MethodPost, "/api/v1/code-source-connections/code-source-1/verify", nil, true)
	if authorization.lastAction != domain.ActionCodeSourceManage {
		t.Fatalf("expected verify to require manage permission, got %q", authorization.lastAction)
	}
}

func TestSearchCodeSourceConnectionRepositories(t *testing.T) {
	service := &fakeCodeSourceService{page: domain.RepositoryPage{
		Repositories: []domain.CodeSourceRepository{{
			ConnectionID:         "code-source-1",
			ExternalRepositoryID: "42",
			FullName:             "rn/home",
			CloneURL:             "https://gitlab.example.com/rn/home.git",
			LastSyncedAt:         time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
		}},
		HasMore:  true,
		Page:     2,
		PageSize: 10,
	}}
	e := newAuthorizedTestServer(Dependencies{CodeSourceService: service})

	rec := serveCodeSourceRequest(e, http.MethodGet, "/api/v1/code-source-connections/code-source-1/repositories?query=home&page=2&pageSize=10", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if service.lastID != "code-source-1" || service.lastSearch != (domain.RepositorySearchInput{Query: "home", Page: 2, PageSize: 10}) {
		t.Fatalf("unexpected search call: %q %+v", service.lastID, service.lastSearch)
	}
	var response codeSourceRepositorySearchResponse
	decodeTestJSON(t, rec, &response)
	if !response.HasMore || response.Page != 2 || response.PageSize != 10 || len(response.Repositories) != 1 || response.Repositories[0].ExternalRepositoryID != "42" {
		t.Fatalf("unexpected search response: %+v", response)
	}

	rec = serveCodeSourceRequest(e, http.MethodGet, "/api/v1/code-source-connections/code-source-1/repositories?pageSize=abc", nil, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for invalid page size, got %d", rec.Code)
	}
}

func TestGetCodeSourceConnectionWebhook(t *testing.T) {
	service := &fakeCodeSourceService{webhook: domain.CodeSourceConnectionWebhook{
		Provider: domain.CodeSourceProviderGitLab,
		Path:     "/api/v1/webhooks/gitlab/code-source-1",
		Secret:   "hook-secret",
	}}
	e := newAuthorizedTestServer(Dependencies{CodeSourceService: service})

	rec := serveCodeSourceRequest(e, http.MethodGet, "/api/v1/code-source-connections/code-source-1/webhook", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	var response codeSourceConnectionWebhookResponse
	decodeTestJSON(t, rec, &response)
	if response.Path != "/api/v1/webhooks/gitlab/code-source-1" || response.Secret != "hook-secret" || response.Provider != "gitlab" {
		t.Fatalf("unexpected webhook response: %+v", response)
	}
}

func serveCodeSourceRequest(e http.Handler, method string, target string, body any, withSession bool) *httptest.ResponseRecorder {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, jsonBody(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	if withSession {
		addTestConsoleSession(req)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func decodeTestJSON(t *testing.T, rec *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func testHTTPCodeSourceConnection() domain.CodeSourceConnection {
	expiresAt := time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)
	return domain.CodeSourceConnection{
		ID:                     "code-source-1",
		Provider:               domain.CodeSourceProviderGitLab,
		Name:                   "公司 GitLab",
		WebBaseURL:             "https://gitlab.example.com",
		APIBaseURL:             "https://gitlab.example.com",
		ExternalAccount:        "group_bot",
		CredentialEncrypted:    "enc:glpat-secret-token",
		CredentialPreview:      "glpa****oken",
		CredentialExpiresAt:    &expiresAt,
		WebhookSecretEncrypted: "enc:hook-secret",
		Status:                 domain.CodeSourceConnectionStatusActive,
		CreatedByUserID:        "ou-test-user",
		CreatedAt:              time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
		UpdatedAt:              time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
	}
}

type recordingAuthorizationService struct {
	fakeAuthorizationService
	lastAction domain.AuthorizationAction
}

func (s *recordingAuthorizationService) Authorize(_ context.Context, _ string, action domain.AuthorizationAction, _ domain.AuthorizationResource) error {
	s.lastAction = action
	return nil
}

type fakeCodeSourceService struct {
	apps                []domain.CodeSourceApp
	completeManifestURL domain.CodeSourceInstallURL
	connection          domain.CodeSourceConnection
	connections         []domain.CodeSourceConnection
	err                 error
	installURL          domain.CodeSourceInstallURL
	manifest            domain.CodeSourceAppManifest
	page                domain.RepositoryPage
	webhook             domain.CodeSourceConnectionWebhook

	lastCreate         domain.CodeSourceConnectionCreateInput
	lastFilters        domain.CodeSourceConnectionFilters
	lastID             string
	lastInstallationID int64
	lastSearch         domain.RepositorySearchInput
	lastSetupAction    string
	lastUpdate         domain.CodeSourceConnectionUpdateInput
	lastUserID         string
}

func (s *fakeCodeSourceService) CompleteAppManifest(context.Context, string) (domain.CodeSourceInstallURL, error) {
	return s.completeManifestURL, s.err
}

func (s *fakeCodeSourceService) CompleteInstallation(_ context.Context, userID string, installationID int64, setupAction string) (domain.CodeSourceConnection, error) {
	s.lastUserID = userID
	s.lastInstallationID = installationID
	s.lastSetupAction = setupAction
	return s.connection, s.err
}

func (s *fakeCodeSourceService) GetAppManifest(context.Context) (domain.CodeSourceAppManifest, error) {
	return s.manifest, s.err
}

func (s *fakeCodeSourceService) GetInstallURL(context.Context) (domain.CodeSourceInstallURL, error) {
	return s.installURL, s.err
}

func (s *fakeCodeSourceService) ListCodeSources(context.Context) ([]domain.CodeSourceApp, error) {
	return s.apps, s.err
}

func (s *fakeCodeSourceService) CreateConnection(_ context.Context, input domain.CodeSourceConnectionCreateInput) (domain.CodeSourceConnection, error) {
	s.lastCreate = input
	return s.connection, s.err
}

func (s *fakeCodeSourceService) DeleteConnection(_ context.Context, id string) error {
	s.lastID = id
	return s.err
}

func (s *fakeCodeSourceService) GetConnection(_ context.Context, id string) (domain.CodeSourceConnection, error) {
	s.lastID = id
	return s.connection, s.err
}

func (s *fakeCodeSourceService) GetConnectionWebhook(_ context.Context, id string) (domain.CodeSourceConnectionWebhook, error) {
	s.lastID = id
	return s.webhook, s.err
}

func (s *fakeCodeSourceService) ListConnections(_ context.Context, filters domain.CodeSourceConnectionFilters) ([]domain.CodeSourceConnection, error) {
	s.lastFilters = filters
	return s.connections, s.err
}

func (s *fakeCodeSourceService) SearchRepositories(_ context.Context, connectionID string, input domain.RepositorySearchInput) (domain.RepositoryPage, error) {
	s.lastID = connectionID
	s.lastSearch = input
	return s.page, s.err
}

func (s *fakeCodeSourceService) UpdateConnection(_ context.Context, id string, input domain.CodeSourceConnectionUpdateInput) (domain.CodeSourceConnection, error) {
	s.lastID = id
	s.lastUpdate = input
	return s.connection, s.err
}

func (s *fakeCodeSourceService) VerifyConnection(_ context.Context, id string) (domain.CodeSourceConnection, error) {
	s.lastID = id
	return s.connection, s.err
}

func TestCodeSourceConnectionRoutesStopAfterAuthorizationFailure(t *testing.T) {
	service := &fakeCodeSourceService{connection: testHTTPCodeSourceConnection()}
	forbidden := newAuthorizedTestServer(Dependencies{
		CodeSourceService:    service,
		AuthorizationService: fakeAuthorizationService{err: domain.ErrForbidden},
	})

	rec := serveCodeSourceRequest(forbidden, http.MethodPost, "/api/v1/code-source-connections", codeSourceConnectionCreateRequest{Provider: "gitlab"}, true)
	if rec.Code != http.StatusForbidden || service.lastCreate.Provider != "" {
		t.Fatalf("expected forbidden without service call, got %d (input=%+v)", rec.Code, service.lastCreate)
	}
	if strings.Count(rec.Body.String(), "{") != 1 {
		t.Fatalf("expected a single error body, got %s", rec.Body.String())
	}

	rec = serveCodeSourceRequest(forbidden, http.MethodGet, "/api/v1/code-source-connections", nil, false)
	if rec.Code != http.StatusUnauthorized || service.lastFilters != (domain.CodeSourceConnectionFilters{}) || strings.Contains(rec.Body.String(), "connections") {
		t.Fatalf("expected unauthorized without service call, got %d %s", rec.Code, rec.Body.String())
	}
}
