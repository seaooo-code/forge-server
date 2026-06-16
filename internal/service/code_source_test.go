package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"forge-server/internal/domain"
)

func TestCodeSourceServiceListCodeSourcesReflectsGitHubInstallation(t *testing.T) {
	installedAt := time.Date(2026, 6, 10, 11, 24, 0, 0, time.UTC)
	service := NewCodeSourceService(&fakeCodeSourceRepositoryStore{
		installations: []domain.CodeSourceInstallation{
			{
				InstallationID: 1001,
				InstalledAt:    installedAt,
				Provider:       domain.CodeSourceProviderGitHub,
			},
		},
		config: domain.CodeSourceAppConfig{
			AppID:      "1",
			AppSlug:    "forge-console",
			PrivateKey: "key",
			Provider:   domain.CodeSourceProviderGitHub,
		},
	}, nil, domain.CodeSourceAppConfig{})

	apps, err := service.ListCodeSources(context.Background(), "on-user-1")
	if err != nil {
		t.Fatalf("list code sources: %v", err)
	}

	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}
	if apps[0].Provider != domain.CodeSourceProviderGitHub || apps[0].Status != domain.CodeSourceAppStatusInstalled {
		t.Fatalf("unexpected github app: %+v", apps[0])
	}
	if apps[0].ConfigurationStatus != domain.CodeSourceAppConfigurationStatusConfigured {
		t.Fatalf("expected configured app, got %+v", apps[0])
	}
	if apps[0].InstalledAt == nil || !apps[0].InstalledAt.Equal(installedAt) {
		t.Fatalf("expected installed at %s, got %#v", installedAt, apps[0].InstalledAt)
	}
}

func TestCodeSourceServiceListCodeSourcesReportsUnconfiguredGitHubApp(t *testing.T) {
	service := NewCodeSourceService(&fakeCodeSourceRepositoryStore{}, nil, domain.CodeSourceAppConfig{})

	apps, err := service.ListCodeSources(context.Background(), "on-user-1")
	if err != nil {
		t.Fatalf("list code sources: %v", err)
	}

	if len(apps) != 1 || apps[0].ConfigurationStatus != domain.CodeSourceAppConfigurationStatusNotConfigured {
		t.Fatalf("expected unconfigured github app, got %+v", apps)
	}
}

func TestCodeSourceServiceGetAppManifest(t *testing.T) {
	service := NewCodeSourceService(&fakeCodeSourceRepositoryStore{}, &fakeGitHubCodeSourceClient{
		manifest: domain.CodeSourceAppManifest{
			ActionURL: "https://github.com/settings/apps/new",
			Payload:   `{"name":"Forge Console"}`,
		},
	}, domain.CodeSourceAppConfig{})

	manifest, err := service.GetAppManifest(context.Background(), domain.CodeSourceProviderGitHub)
	if err != nil {
		t.Fatalf("get manifest: %v", err)
	}
	if manifest.ActionURL != "https://github.com/settings/apps/new" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
}

func TestCodeSourceServiceGetInstallURL(t *testing.T) {
	client := &fakeGitHubCodeSourceClient{
		installURL: "https://github.com/apps/forge/installations/new",
	}
	service := NewCodeSourceService(&fakeCodeSourceRepositoryStore{
		config: domain.CodeSourceAppConfig{
			AppID:      "1",
			AppSlug:    "forge-console",
			PrivateKey: "key",
			Provider:   domain.CodeSourceProviderGitHub,
		},
	}, client, domain.CodeSourceAppConfig{})

	installURL, err := service.GetInstallURL(context.Background(), domain.CodeSourceProviderGitHub, "on-user-1")
	if err != nil {
		t.Fatalf("get install url: %v", err)
	}
	if installURL.URL != "https://github.com/apps/forge/installations/new" {
		t.Fatalf("unexpected install url %q", installURL.URL)
	}
	if client.validateAppCalls != 1 {
		t.Fatalf("expected app validation, got %d calls", client.validateAppCalls)
	}
}

func TestCodeSourceServiceGetInstallURLClearsDeletedGitHubAppConfig(t *testing.T) {
	repository := &fakeCodeSourceRepositoryStore{
		config: domain.CodeSourceAppConfig{
			AppID:      "1",
			AppSlug:    "deleted-forge-console",
			PrivateKey: "key",
			Provider:   domain.CodeSourceProviderGitHub,
		},
	}
	service := NewCodeSourceService(repository, &fakeGitHubCodeSourceClient{
		validateAppErr: domain.ErrCodeSourceAppNotFound,
	}, domain.CodeSourceAppConfig{})

	_, err := service.GetInstallURL(context.Background(), domain.CodeSourceProviderGitHub, "on-user-1")
	if !errors.Is(err, domain.ErrCodeSourceAuthNotConfigured) {
		t.Fatalf("expected auth not configured, got %v", err)
	}
	if repository.deleteConfigCalls != 1 {
		t.Fatalf("expected stale config to be deleted, got %d calls", repository.deleteConfigCalls)
	}
}

func TestCodeSourceServiceCompleteManifestStoresConfigAndReturnsInstallURL(t *testing.T) {
	repository := &fakeCodeSourceRepositoryStore{}
	service := NewCodeSourceService(repository, &fakeGitHubCodeSourceClient{
		convertedConfig: domain.CodeSourceAppConfig{
			AppID:      "100",
			AppSlug:    "forge-console",
			PrivateKey: "pem",
		},
		installURL: "https://github.com/apps/forge-console/installations/new",
	}, domain.CodeSourceAppConfig{})

	installURL, err := service.CompleteAppManifest(context.Background(), domain.CodeSourceProviderGitHub, "manifest-code")
	if err != nil {
		t.Fatalf("complete manifest: %v", err)
	}

	if repository.upsertedConfig.AppID != "100" || repository.upsertedConfig.Provider != domain.CodeSourceProviderGitHub {
		t.Fatalf("unexpected upserted config: %+v", repository.upsertedConfig)
	}
	if installURL.URL != "https://github.com/apps/forge-console/installations/new" {
		t.Fatalf("unexpected install url %q", installURL.URL)
	}
}

func TestCodeSourceServiceCompleteInstallationUpsertsInstallation(t *testing.T) {
	installedAt := time.Date(2026, 6, 10, 11, 24, 0, 0, time.UTC)
	repository := &fakeCodeSourceRepositoryStore{
		config: domain.CodeSourceAppConfig{
			AppID:      "1",
			AppSlug:    "forge-console",
			PrivateKey: "key",
			Provider:   domain.CodeSourceProviderGitHub,
		},
	}
	service := NewCodeSourceService(repository, &fakeGitHubCodeSourceClient{
		installation: domain.CodeSourceInstallation{
			AccountLogin:        "mi-car",
			AccountType:         "Organization",
			InstallationID:      1001,
			InstalledAt:         installedAt,
			RepositorySelection: "selected",
		},
	}, domain.CodeSourceAppConfig{})

	app, err := service.CompleteInstallation(context.Background(), domain.CodeSourceProviderGitHub, "on-user-1", 1001, "install")
	if err != nil {
		t.Fatalf("complete installation: %v", err)
	}

	if app.Status != domain.CodeSourceAppStatusInstalled {
		t.Fatalf("expected installed app, got %+v", app)
	}
	if repository.upsertInstallationCalls != 1 {
		t.Fatalf("expected installation upsert, got %d calls", repository.upsertInstallationCalls)
	}
	if repository.upsertedInstallation.AccountLogin != "mi-car" || repository.upsertedInstallation.Provider != domain.CodeSourceProviderGitHub {
		t.Fatalf("unexpected upserted installation: %+v", repository.upsertedInstallation)
	}
	if repository.upsertedInstallation.OwnerUnionID != "on-user-1" {
		t.Fatalf("expected owner union id to be persisted, got %+v", repository.upsertedInstallation)
	}
}

func TestCodeSourceServiceRejectsInvalidSetupAction(t *testing.T) {
	repository := &fakeCodeSourceRepositoryStore{}
	client := &fakeGitHubCodeSourceClient{}
	service := NewCodeSourceService(repository, client, domain.CodeSourceAppConfig{})

	_, err := service.CompleteInstallation(context.Background(), domain.CodeSourceProviderGitHub, "on-user-1", 1001, "remove")
	if !errors.Is(err, domain.ErrInvalidCodeSourceInput) {
		t.Fatalf("expected invalid code source input, got %v", err)
	}
	if repository.upsertInstallationCalls != 0 || client.getInstallationCalls != 0 {
		t.Fatalf("expected no external calls, got upsert=%d get=%d", repository.upsertInstallationCalls, client.getInstallationCalls)
	}
}

func TestCodeSourceServiceDeleteInstallationsRevokesGitHubInstallations(t *testing.T) {
	repository := &fakeCodeSourceRepositoryStore{
		config: domain.CodeSourceAppConfig{
			AppID:      "1",
			AppSlug:    "forge-console",
			PrivateKey: "key",
			Provider:   domain.CodeSourceProviderGitHub,
		},
		installations: []domain.CodeSourceInstallation{
			{InstallationID: 1001, Provider: domain.CodeSourceProviderGitHub},
			{InstallationID: 1002, Provider: domain.CodeSourceProviderGitHub},
		},
	}
	client := &fakeGitHubCodeSourceClient{}
	service := NewCodeSourceService(repository, client, domain.CodeSourceAppConfig{})

	if err := service.DeleteInstallations(context.Background(), domain.CodeSourceProviderGitHub, "on-user-1"); err != nil {
		t.Fatalf("delete installations: %v", err)
	}

	if len(client.deletedInstallationIDs) != 2 {
		t.Fatalf("expected 2 revoked installations, got %#v", client.deletedInstallationIDs)
	}
	if repository.deleteInstallationCalls != 1 {
		t.Fatalf("expected local delete, got %d calls", repository.deleteInstallationCalls)
	}
	if repository.deletedOwnerUnionID != "on-user-1" {
		t.Fatalf("expected current user local delete, got %q", repository.deletedOwnerUnionID)
	}
}

func TestCodeSourceServiceDeleteInstallationsKeepsSharedGitHubInstallation(t *testing.T) {
	repository := &fakeCodeSourceRepositoryStore{
		config: domain.CodeSourceAppConfig{
			AppID:      "1",
			AppSlug:    "forge-console",
			PrivateKey: "key",
			Provider:   domain.CodeSourceProviderGitHub,
		},
		hasOtherInstallationOwner: true,
		installations: []domain.CodeSourceInstallation{
			{InstallationID: 1001, Provider: domain.CodeSourceProviderGitHub},
		},
	}
	client := &fakeGitHubCodeSourceClient{}
	service := NewCodeSourceService(repository, client, domain.CodeSourceAppConfig{})

	if err := service.DeleteInstallations(context.Background(), domain.CodeSourceProviderGitHub, "on-user-1"); err != nil {
		t.Fatalf("delete installations: %v", err)
	}

	if len(client.deletedInstallationIDs) != 0 {
		t.Fatalf("expected shared installation to be kept, got %#v", client.deletedInstallationIDs)
	}
	if repository.deleteInstallationCalls != 1 {
		t.Fatalf("expected current user local delete, got %d calls", repository.deleteInstallationCalls)
	}
}

func TestCodeSourceServiceDeleteInstallationsClearsLocalConfigWithoutActiveInstallations(t *testing.T) {
	repository := &fakeCodeSourceRepositoryStore{
		deleteInstallationErr: domain.ErrCodeSourceInstallationNotFound,
		config: domain.CodeSourceAppConfig{
			AppID:      "1",
			AppSlug:    "forge-console",
			PrivateKey: "key",
			Provider:   domain.CodeSourceProviderGitHub,
		},
	}
	client := &fakeGitHubCodeSourceClient{}
	service := NewCodeSourceService(repository, client, domain.CodeSourceAppConfig{})

	if err := service.DeleteInstallations(context.Background(), domain.CodeSourceProviderGitHub, "on-user-1"); !errors.Is(err, domain.ErrCodeSourceInstallationNotFound) {
		t.Fatalf("expected installation not found, got %v", err)
	}

	if len(client.deletedInstallationIDs) != 0 {
		t.Fatalf("expected no github delete calls, got %#v", client.deletedInstallationIDs)
	}
	if repository.deleteInstallationCalls != 1 {
		t.Fatalf("expected local delete, got %d calls", repository.deleteInstallationCalls)
	}
}

func TestCodeSourceServiceSearchRepositoriesFetchesAndFiltersGitHubRepositories(t *testing.T) {
	updatedAt := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	repository := &fakeCodeSourceRepositoryStore{
		config: domain.CodeSourceAppConfig{
			AppID:      "1",
			AppSlug:    "forge-console",
			PrivateKey: "key",
			Provider:   domain.CodeSourceProviderGitHub,
		},
		installations: []domain.CodeSourceInstallation{
			{InstallationID: 1001, Provider: domain.CodeSourceProviderGitHub},
		},
	}
	service := NewCodeSourceService(repository, &fakeGitHubCodeSourceClient{
		repositoryPages: []domain.CodeSourceRepositoryPage{
			{
				Repositories: []domain.CodeSourceRepository{
					{
						FullName:       "mi-car/home",
						InstallationID: 1001,
						Name:           "home",
						Owner:          "mi-car",
						Provider:       domain.CodeSourceProviderGitHub,
						RepositoryID:   2001,
						UpdatedAt:      &updatedAt,
					},
					{
						FullName:     "mi-car/payment",
						Name:         "payment",
						Owner:        "mi-car",
						Provider:     domain.CodeSourceProviderGitHub,
						RepositoryID: 2002,
					},
				},
			},
		},
	}, domain.CodeSourceAppConfig{})

	result, err := service.SearchRepositories(context.Background(), domain.CodeSourceRepositorySearchInput{
		OwnerUnionID: "on-user-1",
		PageSize:     10,
		Provider:     domain.CodeSourceProviderGitHub,
		Query:        "home",
	})
	if err != nil {
		t.Fatalf("search repositories: %v", err)
	}

	if len(result.Repositories) != 1 || result.Repositories[0].FullName != "mi-car/home" {
		t.Fatalf("unexpected repositories: %+v", result.Repositories)
	}
	if len(repository.upsertedRepositories) != 2 {
		t.Fatalf("expected fetched repositories to be cached, got %d", len(repository.upsertedRepositories))
	}
	if repository.upsertedRepositoriesOwnerUnionID != "on-user-1" {
		t.Fatalf("expected current user repository cache, got %q", repository.upsertedRepositoriesOwnerUnionID)
	}
}

type fakeCodeSourceRepositoryStore struct {
	deleteConfigCalls                int
	deleteConfigErr                  error
	deleteInstallationCalls          int
	deleteInstallationErr            error
	deletedOwnerUnionID              string
	config                           domain.CodeSourceAppConfig
	configErr                        error
	hasOtherInstallationOwner        bool
	hasOtherInstallationOwnerErr     error
	installations                    []domain.CodeSourceInstallation
	listOwnerUnionID                 string
	listErr                          error
	upsertConfigErr                  error
	upsertInstallationCalls          int
	upsertInstallationErr            error
	upsertRepositoriesErr            error
	upsertedConfig                   domain.CodeSourceAppConfig
	upsertedInstallation             domain.CodeSourceInstallation
	upsertedRepositories             []domain.CodeSourceRepository
	upsertedRepositoriesOwnerUnionID string
}

func (r *fakeCodeSourceRepositoryStore) DeleteCodeSourceAppConfig(_ context.Context, _ domain.CodeSourceProvider) error {
	r.deleteConfigCalls++
	return r.deleteConfigErr
}

func (r *fakeCodeSourceRepositoryStore) DeleteCodeSourceInstallations(_ context.Context, _ domain.CodeSourceProvider, ownerUnionID string) error {
	r.deleteInstallationCalls++
	r.deletedOwnerUnionID = ownerUnionID
	return r.deleteInstallationErr
}

func (r *fakeCodeSourceRepositoryStore) GetCodeSourceAppConfig(_ context.Context, _ domain.CodeSourceProvider) (domain.CodeSourceAppConfig, error) {
	if r.configErr != nil {
		return domain.CodeSourceAppConfig{}, r.configErr
	}
	if r.config.AppID == "" && r.config.AppSlug == "" && r.config.PrivateKey == "" {
		return domain.CodeSourceAppConfig{}, domain.ErrCodeSourceAuthNotConfigured
	}
	return r.config, nil
}

func (r *fakeCodeSourceRepositoryStore) HasOtherCodeSourceInstallationOwner(_ context.Context, _ domain.CodeSourceProvider, _ string, _ int64) (bool, error) {
	if r.hasOtherInstallationOwnerErr != nil {
		return false, r.hasOtherInstallationOwnerErr
	}
	return r.hasOtherInstallationOwner, nil
}

func (r *fakeCodeSourceRepositoryStore) ListCodeSourceInstallations(_ context.Context, _ domain.CodeSourceProvider, ownerUnionID string) ([]domain.CodeSourceInstallation, error) {
	r.listOwnerUnionID = ownerUnionID
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.installations, nil
}

func (r *fakeCodeSourceRepositoryStore) UpsertCodeSourceAppConfig(_ context.Context, config domain.CodeSourceAppConfig) (domain.CodeSourceAppConfig, error) {
	r.upsertedConfig = config
	if r.upsertConfigErr != nil {
		return domain.CodeSourceAppConfig{}, r.upsertConfigErr
	}
	return config, nil
}

func (r *fakeCodeSourceRepositoryStore) UpsertCodeSourceInstallation(_ context.Context, installation domain.CodeSourceInstallation) (domain.CodeSourceInstallation, error) {
	r.upsertInstallationCalls++
	r.upsertedInstallation = installation
	if r.upsertInstallationErr != nil {
		return domain.CodeSourceInstallation{}, r.upsertInstallationErr
	}
	return installation, nil
}

func (r *fakeCodeSourceRepositoryStore) UpsertCodeSourceRepositories(_ context.Context, ownerUnionID string, repositories []domain.CodeSourceRepository) error {
	r.upsertedRepositoriesOwnerUnionID = ownerUnionID
	r.upsertedRepositories = append(r.upsertedRepositories, repositories...)
	return r.upsertRepositoriesErr
}

type fakeGitHubCodeSourceClient struct {
	convertedConfig        domain.CodeSourceAppConfig
	deletedInstallationIDs []int64
	deleteErr              error
	getInstallationCalls   int
	getInstallationErr     error
	installation           domain.CodeSourceInstallation
	installURL             string
	installURLErr          error
	manifest               domain.CodeSourceAppManifest
	manifestErr            error
	repositoryPages        []domain.CodeSourceRepositoryPage
	repositoryPageErr      error
	validateAppCalls       int
	validateAppErr         error
}

func (c *fakeGitHubCodeSourceClient) AppManifest() (domain.CodeSourceAppManifest, error) {
	if c.manifestErr != nil {
		return domain.CodeSourceAppManifest{}, c.manifestErr
	}
	return c.manifest, nil
}

func (c *fakeGitHubCodeSourceClient) CompleteAppManifest(_ context.Context, _ string) (domain.CodeSourceAppConfig, error) {
	return c.convertedConfig, nil
}

func (c *fakeGitHubCodeSourceClient) DeleteInstallation(_ context.Context, _ domain.CodeSourceAppConfig, installationID int64) error {
	c.deletedInstallationIDs = append(c.deletedInstallationIDs, installationID)
	return c.deleteErr
}

func (c *fakeGitHubCodeSourceClient) GetInstallation(_ context.Context, _ domain.CodeSourceAppConfig, _ int64) (domain.CodeSourceInstallation, error) {
	c.getInstallationCalls++
	if c.getInstallationErr != nil {
		return domain.CodeSourceInstallation{}, c.getInstallationErr
	}
	return c.installation, nil
}

func (c *fakeGitHubCodeSourceClient) InstallationURL(_ domain.CodeSourceAppConfig) (string, error) {
	if c.installURLErr != nil {
		return "", c.installURLErr
	}
	return c.installURL, nil
}

func (c *fakeGitHubCodeSourceClient) ListRepositories(_ context.Context, _ domain.CodeSourceAppConfig, _ int64, page int, _ int) (domain.CodeSourceRepositoryPage, error) {
	if c.repositoryPageErr != nil {
		return domain.CodeSourceRepositoryPage{}, c.repositoryPageErr
	}
	if page < 1 || page > len(c.repositoryPages) {
		return domain.CodeSourceRepositoryPage{}, nil
	}
	return c.repositoryPages[page-1], nil
}

func (c *fakeGitHubCodeSourceClient) ValidateApp(_ context.Context, _ domain.CodeSourceAppConfig) error {
	c.validateAppCalls++
	return c.validateAppErr
}
