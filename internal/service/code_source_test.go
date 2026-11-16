package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"forge-server/internal/domain"
)

func TestCodeSourceProviderRegistryDispatchesByProvider(t *testing.T) {
	gitlab := &fakeCodeSourceProvider{provider: domain.CodeSourceProviderGitLab}
	registry := NewCodeSourceProviderRegistry(gitlab, nil)

	provider, err := registry.Provider(domain.CodeSourceProviderGitLab)
	if err != nil || provider != gitlab {
		t.Fatalf("expected gitlab provider, got %v, %v", provider, err)
	}
	if _, err := registry.Provider(domain.CodeSourceProviderGitHub); !errors.Is(err, domain.ErrCodeSourceProviderNotFound) {
		t.Fatalf("expected provider not found, got %v", err)
	}
}

func TestCodeSourceServiceCreateConnectionVerifiesAndEncrypts(t *testing.T) {
	expiresAt := time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)
	provider := &fakeCodeSourceProvider{
		provider: domain.CodeSourceProviderGitLab,
		info:     domain.CodeSourceConnectionInfo{ExternalAccount: "group_bot", ExpiresAt: &expiresAt},
	}
	service, store := newTestCodeSourceService(provider)

	conn, err := service.CreateConnection(context.Background(), domain.CodeSourceConnectionCreateInput{
		Provider:        domain.CodeSourceProviderGitLab,
		Name:            " 公司 GitLab ",
		WebBaseURL:      " https://gitlab.example.com/ ",
		Credential:      " glpat-1234567890abcdef ",
		CreatedByUserID: "ou-admin",
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	if provider.lastVerified.Credential != "glpat-1234567890abcdef" {
		t.Fatalf("expected provider to verify plaintext credential, got %q", provider.lastVerified.Credential)
	}
	stored := store.created
	if stored.Credential != "" || stored.WebhookSecret != "" {
		t.Fatalf("plaintext secrets must not reach the store: %+v", stored)
	}
	if stored.CredentialEncrypted != "enc:glpat-1234567890abcdef" || !strings.HasPrefix(stored.WebhookSecretEncrypted, "enc:") || len(stored.WebhookSecretEncrypted) != len("enc:")+64 {
		t.Fatalf("expected encrypted credential and webhook secret, got %+v", stored)
	}
	if stored.CredentialPreview != "glpa****cdef" || stored.ExternalAccount != "group_bot" || stored.CredentialExpiresAt == nil || stored.LastVerifiedAt == nil {
		t.Fatalf("unexpected verification fields: %+v", stored)
	}
	if stored.Name != "公司 GitLab" || stored.WebBaseURL != "https://gitlab.example.com" || stored.APIBaseURL != "https://gitlab.example.com" ||
		stored.Status != domain.CodeSourceConnectionStatusActive || stored.CreatedByUserID != "ou-admin" {
		t.Fatalf("unexpected normalized connection: %+v", stored)
	}
	if conn.ID == "" {
		t.Fatalf("expected saved connection id")
	}
}

func TestCodeSourceServiceCreateConnectionRejectsInvalidInput(t *testing.T) {
	valid := domain.CodeSourceConnectionCreateInput{
		Provider:        domain.CodeSourceProviderGitLab,
		Name:            "GitLab",
		WebBaseURL:      "https://gitlab.example.com",
		Credential:      "token",
		CreatedByUserID: "ou-admin",
	}
	tests := map[string]struct {
		mutate  func(*domain.CodeSourceConnectionCreateInput)
		wantErr error
	}{
		"unknown provider": {mutate: func(in *domain.CodeSourceConnectionCreateInput) { in.Provider = "bitbucket" }, wantErr: domain.ErrCodeSourceProviderNotFound},
		"empty name":       {mutate: func(in *domain.CodeSourceConnectionCreateInput) { in.Name = " " }, wantErr: domain.ErrInvalidCodeSourceInput},
		"non http url":     {mutate: func(in *domain.CodeSourceConnectionCreateInput) { in.WebBaseURL = "ssh://gitlab.example.com" }, wantErr: domain.ErrInvalidCodeSourceInput},
		"url with userinfo": {mutate: func(in *domain.CodeSourceConnectionCreateInput) {
			in.WebBaseURL = "https://user:pass@gitlab.example.com"
		}, wantErr: domain.ErrInvalidCodeSourceInput},
		"missing creator": {mutate: func(in *domain.CodeSourceConnectionCreateInput) { in.CreatedByUserID = "" }, wantErr: domain.ErrInvalidCodeSourceInput},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			service, store := newTestCodeSourceService(&fakeCodeSourceProvider{provider: domain.CodeSourceProviderGitLab})
			input := valid
			tt.mutate(&input)
			if _, err := service.CreateConnection(context.Background(), input); !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
			if store.createCalls != 0 {
				t.Fatalf("expected store not to be called")
			}
		})
	}

	failing := &fakeCodeSourceProvider{provider: domain.CodeSourceProviderGitLab, verifyErr: domain.ErrCodeSourceCredentialInvalid}
	service, store := newTestCodeSourceService(failing)
	if _, err := service.CreateConnection(context.Background(), valid); !errors.Is(err, domain.ErrCodeSourceCredentialInvalid) {
		t.Fatalf("expected invalid credential, got %v", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("expected store not to be called after verify failure")
	}
}

func TestCodeSourceServiceUpdateConnectionRotatesCredential(t *testing.T) {
	provider := &fakeCodeSourceProvider{provider: domain.CodeSourceProviderGitLab}
	service, store := newTestCodeSourceService(provider)
	store.put(testGitLabConnection())

	credential := "glpat-new-token-0000"
	disabled := domain.CodeSourceConnectionStatusDisabled
	updated, err := service.UpdateConnection(context.Background(), "code-source-1", domain.CodeSourceConnectionUpdateInput{
		Credential: &credential,
		Status:     &disabled,
	})
	if err != nil {
		t.Fatalf("update connection: %v", err)
	}
	if provider.lastVerified.Credential != credential {
		t.Fatalf("expected re-verification with new credential, got %q", provider.lastVerified.Credential)
	}
	if updated.CredentialEncrypted != "enc:"+credential || updated.Credential != "" || updated.Status != domain.CodeSourceConnectionStatusDisabled {
		t.Fatalf("unexpected updated connection: %+v", updated)
	}
	if updated.WebhookSecretEncrypted != "enc:hook-secret" {
		t.Fatalf("webhook secret must be kept, got %q", updated.WebhookSecretEncrypted)
	}

	deleted := domain.CodeSourceConnectionStatusDeleted
	if _, err := service.UpdateConnection(context.Background(), "code-source-1", domain.CodeSourceConnectionUpdateInput{Status: &deleted}); !errors.Is(err, domain.ErrInvalidCodeSourceInput) {
		t.Fatalf("expected deleted status to be rejected, got %v", err)
	}
}

func TestCodeSourceServiceSearchRepositoriesCachesResults(t *testing.T) {
	provider := &fakeCodeSourceProvider{
		provider: domain.CodeSourceProviderGitLab,
		page: domain.RepositoryPage{
			Repositories: []domain.CodeSourceRepository{{ExternalRepositoryID: "42", FullName: "rn/home"}},
			HasMore:      true,
		},
	}
	service, store := newTestCodeSourceService(provider)
	store.put(testGitLabConnection())

	page, err := service.SearchRepositories(context.Background(), "code-source-1", domain.RepositorySearchInput{Query: " home "})
	if err != nil {
		t.Fatalf("search repositories: %v", err)
	}
	if provider.lastSearchConn.Credential != "glpat-token" || provider.lastSearchInput.Query != "home" ||
		provider.lastSearchInput.Page != 1 || provider.lastSearchInput.PageSize != 20 {
		t.Fatalf("unexpected provider call: conn=%+v input=%+v", provider.lastSearchConn, provider.lastSearchInput)
	}
	if page.Page != 1 || page.PageSize != 20 || !page.HasMore || page.Repositories[0].ConnectionID != "code-source-1" {
		t.Fatalf("unexpected page: %+v", page)
	}
	cached, ok := store.repositories["code-source-1/42"]
	if !ok || cached.FullName != "rn/home" || cached.LastSyncedAt.IsZero() {
		t.Fatalf("expected repository cache, got %+v", store.repositories)
	}

	if _, err := service.SearchRepositories(context.Background(), "code-source-1", domain.RepositorySearchInput{PageSize: 101}); !errors.Is(err, domain.ErrInvalidCodeSourceInput) {
		t.Fatalf("expected invalid page size, got %v", err)
	}

	disabled := testGitLabConnection()
	disabled.Status = domain.CodeSourceConnectionStatusDisabled
	store.put(disabled)
	if _, err := service.SearchRepositories(context.Background(), "code-source-1", domain.RepositorySearchInput{}); !errors.Is(err, domain.ErrCodeSourceConnectionInactive) {
		t.Fatalf("expected inactive connection, got %v", err)
	}
}

func TestCodeSourceServiceCloneCredentialsUsesDecryptedConnection(t *testing.T) {
	provider := &fakeCodeSourceProvider{
		provider:    domain.CodeSourceProviderGitLab,
		credentials: domain.GitCredentials{Username: "oauth2", Password: "glpat-token"},
	}
	service, store := newTestCodeSourceService(provider)
	store.put(testGitLabConnection())

	credentials, err := service.CloneCredentials(context.Background(), domain.ModuleRepository{ConnectionID: "code-source-1", ExternalRepositoryID: "42"})
	if err != nil {
		t.Fatalf("clone credentials: %v", err)
	}
	if credentials.Username != "oauth2" || provider.lastCloneConn.Credential != "glpat-token" || provider.lastCloneRepo.ExternalRepositoryID != "42" {
		t.Fatalf("unexpected clone credentials call: %+v %+v", credentials, provider.lastCloneConn)
	}
	if _, err := service.CloneCredentials(context.Background(), domain.ModuleRepository{ConnectionID: "missing"}); !errors.Is(err, domain.ErrCodeSourceConnectionNotFound) {
		t.Fatalf("expected connection not found, got %v", err)
	}
}

func TestCodeSourceServiceConnectionWebhook(t *testing.T) {
	service, store := newTestCodeSourceService(
		&fakeCodeSourceProvider{provider: domain.CodeSourceProviderGitLab},
		&fakeCodeSourceProvider{provider: domain.CodeSourceProviderGitHub},
	)
	store.put(testGitLabConnection())
	store.put(domain.CodeSourceConnection{ID: "code-source-gh", Provider: domain.CodeSourceProviderGitHub, ExternalID: "1001", Status: domain.CodeSourceConnectionStatusActive})

	gitlab, err := service.GetConnectionWebhook(context.Background(), "code-source-1")
	if err != nil || gitlab.Path != "/api/v1/webhooks/gitlab/code-source-1" || gitlab.Secret != "hook-secret" {
		t.Fatalf("unexpected gitlab webhook: %+v, %v", gitlab, err)
	}
	github, err := service.GetConnectionWebhook(context.Background(), "code-source-gh")
	if err != nil || github.Path != "/api/v1/webhooks/github" || github.Secret != "" {
		t.Fatalf("unexpected github webhook: %+v, %v", github, err)
	}
}

func TestCodeSourceServiceHandleWebhookWithConnectionPath(t *testing.T) {
	provider := &fakeCodeSourceProvider{
		provider: domain.CodeSourceProviderGitLab,
		event:    domain.PushEvent{ExternalRepositoryID: "42", Branch: "main", CommitSHA: "abc"},
		parseOK:  true,
	}
	service, store := newTestCodeSourceService(provider)
	handler := service.pushHandler.(*recordingPushHandler)
	conn := testGitLabConnection()
	conn.CreatedByUserID = "ou-admin"
	store.put(conn)

	result, err := service.HandleWebhook(context.Background(), domain.WebhookRequest{Provider: domain.CodeSourceProviderGitLab, ConnectionID: " code-source-1 "})
	if err != nil || !result.Handled {
		t.Fatalf("expected handled webhook, got %+v, %v", result, err)
	}
	if provider.lastWebhook.Connection == nil || provider.lastWebhook.Connection.WebhookSecret != "hook-secret" {
		t.Fatalf("expected provider to receive decrypted connection, got %+v", provider.lastWebhook.Connection)
	}
	if len(handler.events) != 1 || handler.events[0].ConnectionID != "code-source-1" || handler.events[0].Provider != domain.CodeSourceProviderGitLab ||
		handler.events[0].ConnectionCreatedByUserID != "ou-admin" {
		t.Fatalf("unexpected push events: %+v", handler.events)
	}

	if _, err := service.HandleWebhook(context.Background(), domain.WebhookRequest{Provider: domain.CodeSourceProviderGitLab, ConnectionID: "missing"}); !errors.Is(err, domain.ErrCodeSourceConnectionNotFound) {
		t.Fatalf("expected missing connection, got %v", err)
	}
}

func TestCodeSourceServiceHandleWebhookIgnoresUnmatchedPush(t *testing.T) {
	provider := &fakeCodeSourceProvider{
		provider: domain.CodeSourceProviderGitLab,
		event:    domain.PushEvent{ExternalRepositoryID: "42", Branch: "docs/readme", CommitSHA: "abc"},
		parseOK:  true,
	}
	service, store := newTestCodeSourceService(provider)
	handler := service.pushHandler.(*recordingPushHandler)
	handler.noMatch = true
	store.put(testGitLabConnection())

	result, err := service.HandleWebhook(context.Background(), domain.WebhookRequest{Provider: domain.CodeSourceProviderGitLab, ConnectionID: "code-source-1"})
	if err != nil || result.Handled || len(handler.events) != 1 {
		t.Fatalf("expected unmatched push to be reported as ignored, got %+v, %v", result, err)
	}
}

func TestCodeSourceServiceHandleWebhookRejectsProviderMismatch(t *testing.T) {
	service, store := newTestCodeSourceService(
		&fakeCodeSourceProvider{provider: domain.CodeSourceProviderGitLab},
		&fakeCodeSourceProvider{provider: domain.CodeSourceProviderGitHub},
	)
	store.put(testGitLabConnection())

	_, err := service.HandleWebhook(context.Background(), domain.WebhookRequest{Provider: domain.CodeSourceProviderGitHub, ConnectionID: "code-source-1"})
	if !errors.Is(err, domain.ErrCodeSourceConnectionNotFound) {
		t.Fatalf("expected connection not found for provider mismatch, got %v", err)
	}
}

func TestCodeSourceServiceHandleWebhookResolvesExternalConnection(t *testing.T) {
	provider := &fakeCodeSourceProvider{
		provider: domain.CodeSourceProviderGitHub,
		event:    domain.PushEvent{ExternalConnectionID: "1001", ExternalRepositoryID: "2001", Branch: "main", CommitSHA: "abc"},
		parseOK:  true,
	}
	service, store := newTestCodeSourceService(provider)
	handler := service.pushHandler.(*recordingPushHandler)
	store.put(domain.CodeSourceConnection{ID: "code-source-gh", Provider: domain.CodeSourceProviderGitHub, ExternalID: "1001", Status: domain.CodeSourceConnectionStatusActive})

	result, err := service.HandleWebhook(context.Background(), domain.WebhookRequest{Provider: domain.CodeSourceProviderGitHub})
	if err != nil || !result.Handled || result.Event.ConnectionID != "code-source-gh" {
		t.Fatalf("expected resolved connection, got %+v, %v", result, err)
	}
	if len(handler.events) != 1 {
		t.Fatalf("expected one push event, got %d", len(handler.events))
	}

	provider.event.ExternalConnectionID = "9999"
	if _, err := service.HandleWebhook(context.Background(), domain.WebhookRequest{Provider: domain.CodeSourceProviderGitHub}); !errors.Is(err, domain.ErrCodeSourceConnectionNotFound) {
		t.Fatalf("expected unknown installation to be not found, got %v", err)
	}
}

func TestCodeSourceServiceHandleWebhookSkipsIgnoredAndInactive(t *testing.T) {
	provider := &fakeCodeSourceProvider{provider: domain.CodeSourceProviderGitLab}
	service, store := newTestCodeSourceService(provider)
	handler := service.pushHandler.(*recordingPushHandler)
	store.put(testGitLabConnection())

	result, err := service.HandleWebhook(context.Background(), domain.WebhookRequest{Provider: domain.CodeSourceProviderGitLab, ConnectionID: "code-source-1"})
	if err != nil || result.Handled {
		t.Fatalf("expected ignored event, got %+v, %v", result, err)
	}

	provider.parseOK = true
	disabled := testGitLabConnection()
	disabled.Status = domain.CodeSourceConnectionStatusDisabled
	store.put(disabled)
	result, err = service.HandleWebhook(context.Background(), domain.WebhookRequest{Provider: domain.CodeSourceProviderGitLab, ConnectionID: "code-source-1"})
	if err != nil || result.Handled {
		t.Fatalf("expected disabled connection to be skipped, got %+v, %v", result, err)
	}

	provider.parseErr = domain.ErrCodeSourceWebhookUnauthorized
	if _, err := service.HandleWebhook(context.Background(), domain.WebhookRequest{Provider: domain.CodeSourceProviderGitLab, ConnectionID: "code-source-1"}); !errors.Is(err, domain.ErrCodeSourceWebhookUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
	if len(handler.events) != 0 {
		t.Fatalf("expected no push events, got %+v", handler.events)
	}
}

func TestCodeSourceServiceCompleteInstallationCreatesConnection(t *testing.T) {
	service, store := newTestCodeSourceService(&fakeCodeSourceProvider{provider: domain.CodeSourceProviderGitHub})
	githubApp := service.githubApp.(*fakeGitHubAppClient)
	githubApp.installation = domain.CodeSourceInstallation{
		AccountLogin:   "acme",
		InstallationID: 1001,
		WebBaseURL:     "https://github.com",
		APIBaseURL:     "https://api.github.com",
	}

	conn, err := service.CompleteInstallation(context.Background(), " ou-admin ", 1001, "install")
	if err != nil {
		t.Fatalf("complete installation: %v", err)
	}
	if conn.Provider != domain.CodeSourceProviderGitHub || conn.ExternalID != "1001" || conn.Name != "acme" ||
		conn.CreatedByUserID != "ou-admin" || conn.WebBaseURL != "https://github.com" || conn.APIBaseURL != "https://api.github.com" {
		t.Fatalf("unexpected connection: %+v", conn)
	}

	githubApp.installation.AccountLogin = "acme-renamed"
	again, err := service.CompleteInstallation(context.Background(), "ou-other", 1001, "update")
	if err != nil {
		t.Fatalf("complete installation update: %v", err)
	}
	if again.ID != conn.ID || again.ExternalAccount != "acme-renamed" || again.CreatedByUserID != "ou-admin" || store.createCalls != 1 {
		t.Fatalf("expected existing connection to be updated, got %+v (creates=%d)", again, store.createCalls)
	}

	if _, err := service.CompleteInstallation(context.Background(), "ou-admin", 1001, "delete"); !errors.Is(err, domain.ErrInvalidCodeSourceInput) {
		t.Fatalf("expected invalid setup action, got %v", err)
	}
}

func TestCodeSourceServiceGetInstallURLClearsDeletedGitHubAppConfig(t *testing.T) {
	service, store := newTestCodeSourceService()
	githubApp := service.githubApp.(*fakeGitHubAppClient)
	githubApp.validateErr = domain.ErrCodeSourceAppNotFound

	if _, err := service.GetInstallURL(context.Background()); !errors.Is(err, domain.ErrCodeSourceAuthNotConfigured) {
		t.Fatalf("expected auth not configured, got %v", err)
	}
	if store.deletedAppConfig != domain.CodeSourceProviderGitHub {
		t.Fatalf("expected deleted github app config to be cleared")
	}

	githubApp.validateErr = nil
	installURL, err := service.GetInstallURL(context.Background())
	if err != nil || installURL.URL != "https://github.com/apps/forge/installations/new" {
		t.Fatalf("unexpected install url: %+v, %v", installURL, err)
	}
}

func TestCodeSourceServiceListCodeSourcesCountsGitHubConnections(t *testing.T) {
	service, store := newTestCodeSourceService()
	apps, err := service.ListCodeSources(context.Background())
	if err != nil || len(apps) != 1 || apps[0].Status != domain.CodeSourceAppStatusNotInstalled ||
		apps[0].ConfigurationStatus != domain.CodeSourceAppConfigurationStatusConfigured {
		t.Fatalf("unexpected apps: %+v, %v", apps, err)
	}

	store.put(domain.CodeSourceConnection{ID: "code-source-gh", Provider: domain.CodeSourceProviderGitHub, Status: domain.CodeSourceConnectionStatusActive})
	store.put(testGitLabConnection())
	apps, err = service.ListCodeSources(context.Background())
	if err != nil || apps[0].Status != domain.CodeSourceAppStatusInstalled || apps[0].ConnectionCount != 1 {
		t.Fatalf("unexpected apps after install: %+v, %v", apps, err)
	}
}

func newTestCodeSourceService(providers ...CodeSourceProvider) (*CodeSourceService, *fakeCodeSourceStore) {
	store := newFakeCodeSourceStore()
	service := NewCodeSourceService(CodeSourceServiceDependencies{
		GitHubApp: &fakeGitHubAppClient{
			config:     domain.CodeSourceAppConfig{AppID: "1", AppSlug: "forge", Provider: domain.CodeSourceProviderGitHub},
			configured: true,
		},
		PushHandler: &recordingPushHandler{},
		Registry:    NewCodeSourceProviderRegistry(providers...),
		Secrets:     fakeSecretBox{},
		Store:       store,
	})
	return service, store
}

func testGitLabConnection() domain.CodeSourceConnection {
	return domain.CodeSourceConnection{
		ID:                     "code-source-1",
		Provider:               domain.CodeSourceProviderGitLab,
		Name:                   "GitLab",
		CredentialEncrypted:    "enc:glpat-token",
		WebhookSecretEncrypted: "enc:hook-secret",
		Status:                 domain.CodeSourceConnectionStatusActive,
	}
}

// fakeSecretBox 用可逆前缀模拟加密，便于断言落库的是密文。
type fakeSecretBox struct{}

func (fakeSecretBox) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	return "enc:" + plaintext, nil
}

func (fakeSecretBox) Decrypt(ciphertext string) (string, error) {
	plaintext, ok := strings.CutPrefix(ciphertext, "enc:")
	if !ok {
		return "", errors.New("invalid ciphertext")
	}
	return plaintext, nil
}

type recordingPushHandler struct {
	events []domain.PushEvent
	// noMatch 模拟没有模块匹配自动构建规则。
	noMatch bool
}

func (h *recordingPushHandler) HandlePushEvent(_ context.Context, event domain.PushEvent) (domain.PushEventResult, error) {
	h.events = append(h.events, event)
	if h.noMatch {
		return domain.PushEventResult{}, nil
	}
	return domain.PushEventResult{CreatedBuildIDs: []string{"build-" + event.CommitSHA}}, nil
}

type fakeCodeSourceProvider struct {
	provider    domain.CodeSourceProvider
	info        domain.CodeSourceConnectionInfo
	verifyErr   error
	page        domain.RepositoryPage
	credentials domain.GitCredentials
	event       domain.PushEvent
	parseOK     bool
	parseErr    error

	lastVerified    domain.CodeSourceConnection
	lastSearchConn  domain.CodeSourceConnection
	lastSearchInput domain.RepositorySearchInput
	lastCloneConn   domain.CodeSourceConnection
	lastCloneRepo   domain.ModuleRepository
	lastWebhook     domain.WebhookRequest
}

func (p *fakeCodeSourceProvider) Provider() domain.CodeSourceProvider {
	return p.provider
}

func (p *fakeCodeSourceProvider) VerifyConnection(_ context.Context, conn domain.CodeSourceConnection) (domain.CodeSourceConnectionInfo, error) {
	p.lastVerified = conn
	return p.info, p.verifyErr
}

func (p *fakeCodeSourceProvider) SearchRepositories(_ context.Context, conn domain.CodeSourceConnection, input domain.RepositorySearchInput) (domain.RepositoryPage, error) {
	p.lastSearchConn = conn
	p.lastSearchInput = input
	return p.page, nil
}

func (p *fakeCodeSourceProvider) CloneCredentials(_ context.Context, conn domain.CodeSourceConnection, repo domain.ModuleRepository) (domain.GitCredentials, error) {
	p.lastCloneConn = conn
	p.lastCloneRepo = repo
	return p.credentials, nil
}

func (p *fakeCodeSourceProvider) ParseWebhook(_ context.Context, req domain.WebhookRequest) (domain.PushEvent, bool, error) {
	p.lastWebhook = req
	if p.parseErr != nil {
		return domain.PushEvent{}, false, p.parseErr
	}
	return p.event, p.parseOK, nil
}

type fakeGitHubAppClient struct {
	config       domain.CodeSourceAppConfig
	configured   bool
	installation domain.CodeSourceInstallation
	validateErr  error
}

func (c *fakeGitHubAppClient) AppConfig(context.Context) (domain.CodeSourceAppConfig, bool, error) {
	return c.config, c.configured, nil
}

func (c *fakeGitHubAppClient) AppManifest() (domain.CodeSourceAppManifest, error) {
	return domain.CodeSourceAppManifest{ActionURL: "https://github.com/settings/apps/new"}, nil
}

func (c *fakeGitHubAppClient) CompleteAppManifest(context.Context, string) (domain.CodeSourceAppConfig, error) {
	return c.config, nil
}

func (c *fakeGitHubAppClient) GetInstallation(context.Context, domain.CodeSourceAppConfig, int64) (domain.CodeSourceInstallation, error) {
	return c.installation, nil
}

func (c *fakeGitHubAppClient) InstallationURL(config domain.CodeSourceAppConfig) (string, error) {
	return "https://github.com/apps/" + config.AppSlug + "/installations/new", nil
}

func (c *fakeGitHubAppClient) ValidateApp(context.Context, domain.CodeSourceAppConfig) error {
	return c.validateErr
}

type fakeCodeSourceStore struct {
	connections      map[string]domain.CodeSourceConnection
	repositories     map[string]domain.CodeSourceRepository
	created          domain.CodeSourceConnection
	createCalls      int
	deletedAppConfig domain.CodeSourceProvider
}

func newFakeCodeSourceStore() *fakeCodeSourceStore {
	return &fakeCodeSourceStore{
		connections:  map[string]domain.CodeSourceConnection{},
		repositories: map[string]domain.CodeSourceRepository{},
	}
}

func (s *fakeCodeSourceStore) put(conn domain.CodeSourceConnection) {
	s.connections[conn.ID] = conn
}

func (s *fakeCodeSourceStore) GetCodeSourceAppConfig(context.Context, domain.CodeSourceProvider) (domain.CodeSourceAppConfig, error) {
	return domain.CodeSourceAppConfig{}, domain.ErrCodeSourceAuthNotConfigured
}

func (s *fakeCodeSourceStore) UpsertCodeSourceAppConfig(_ context.Context, config domain.CodeSourceAppConfig) (domain.CodeSourceAppConfig, error) {
	return config, nil
}

func (s *fakeCodeSourceStore) DeleteCodeSourceAppConfig(_ context.Context, provider domain.CodeSourceProvider) error {
	s.deletedAppConfig = provider
	return nil
}

func (s *fakeCodeSourceStore) ListCodeSourceConnections(_ context.Context, filters domain.CodeSourceConnectionFilters) ([]domain.CodeSourceConnection, error) {
	result := []domain.CodeSourceConnection{}
	for _, conn := range s.connections {
		if (filters.Provider == "" || conn.Provider == filters.Provider) && (filters.Status == "" || conn.Status == filters.Status) {
			result = append(result, conn)
		}
	}
	return result, nil
}

func (s *fakeCodeSourceStore) GetCodeSourceConnection(_ context.Context, id string) (domain.CodeSourceConnection, error) {
	conn, ok := s.connections[id]
	if !ok || conn.Status == domain.CodeSourceConnectionStatusDeleted {
		return domain.CodeSourceConnection{}, domain.ErrCodeSourceConnectionNotFound
	}
	return conn, nil
}

func (s *fakeCodeSourceStore) FindCodeSourceConnectionByExternalID(_ context.Context, provider domain.CodeSourceProvider, externalID string) (domain.CodeSourceConnection, error) {
	for _, conn := range s.connections {
		if conn.Provider == provider && conn.ExternalID == externalID && conn.Status != domain.CodeSourceConnectionStatusDeleted {
			return conn, nil
		}
	}
	return domain.CodeSourceConnection{}, domain.ErrCodeSourceConnectionNotFound
}

func (s *fakeCodeSourceStore) CreateCodeSourceConnection(_ context.Context, conn domain.CodeSourceConnection) (domain.CodeSourceConnection, error) {
	s.createCalls++
	s.created = conn
	conn.ID = "code-source-new"
	s.connections[conn.ID] = conn
	return conn, nil
}

func (s *fakeCodeSourceStore) UpdateCodeSourceConnection(_ context.Context, conn domain.CodeSourceConnection) (domain.CodeSourceConnection, error) {
	if _, ok := s.connections[conn.ID]; !ok {
		return domain.CodeSourceConnection{}, domain.ErrCodeSourceConnectionNotFound
	}
	s.connections[conn.ID] = conn
	return conn, nil
}

func (s *fakeCodeSourceStore) DeleteCodeSourceConnection(_ context.Context, id string) error {
	delete(s.connections, id)
	return nil
}

func (s *fakeCodeSourceStore) UpsertCodeSourceRepositories(_ context.Context, repositories []domain.CodeSourceRepository) error {
	for _, repository := range repositories {
		s.repositories[repository.ConnectionID+"/"+repository.ExternalRepositoryID] = repository
	}
	return nil
}

func (s *fakeCodeSourceStore) GetCodeSourceRepository(_ context.Context, connectionID string, externalRepositoryID string) (domain.CodeSourceRepository, error) {
	repository, ok := s.repositories[connectionID+"/"+externalRepositoryID]
	if !ok {
		return domain.CodeSourceRepository{}, domain.ErrCodeSourceRepositoryNotFound
	}
	return repository, nil
}
