package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"forge-server/internal/domain"
)

const (
	defaultCodeSourceRepositoryPageSize = 20
	maxCodeSourceRepositoryPageSize     = 100
	githubRepositoryFetchPageSize       = 100
)

type CodeSourceRepositoryStore interface {
	DeleteCodeSourceAppConfig(ctx context.Context, provider domain.CodeSourceProvider) error
	DeleteCodeSourceInstallations(ctx context.Context, provider domain.CodeSourceProvider, ownerUnionID string) error
	GetCodeSourceAppConfig(ctx context.Context, provider domain.CodeSourceProvider) (domain.CodeSourceAppConfig, error)
	HasOtherCodeSourceInstallationOwner(ctx context.Context, provider domain.CodeSourceProvider, ownerUnionID string, installationID int64) (bool, error)
	ListCodeSourceInstallations(ctx context.Context, provider domain.CodeSourceProvider, ownerUnionID string) ([]domain.CodeSourceInstallation, error)
	UpsertCodeSourceAppConfig(ctx context.Context, config domain.CodeSourceAppConfig) (domain.CodeSourceAppConfig, error)
	UpsertCodeSourceInstallation(ctx context.Context, installation domain.CodeSourceInstallation) (domain.CodeSourceInstallation, error)
	UpsertCodeSourceRepositories(ctx context.Context, ownerUnionID string, repositories []domain.CodeSourceRepository) error
}

type GitHubCodeSourceClient interface {
	AppManifest() (domain.CodeSourceAppManifest, error)
	CompleteAppManifest(ctx context.Context, code string) (domain.CodeSourceAppConfig, error)
	DeleteInstallation(ctx context.Context, config domain.CodeSourceAppConfig, installationID int64) error
	GetInstallation(ctx context.Context, config domain.CodeSourceAppConfig, installationID int64) (domain.CodeSourceInstallation, error)
	InstallationURL(config domain.CodeSourceAppConfig) (string, error)
	ListRepositories(ctx context.Context, config domain.CodeSourceAppConfig, installationID int64, page int, pageSize int) (domain.CodeSourceRepositoryPage, error)
	ValidateApp(ctx context.Context, config domain.CodeSourceAppConfig) error
}

type CodeSourceService struct {
	fallbackGitHubConfig domain.CodeSourceAppConfig
	githubClient         GitHubCodeSourceClient
	repository           CodeSourceRepositoryStore
}

func NewCodeSourceService(repository CodeSourceRepositoryStore, githubClient GitHubCodeSourceClient, fallbackGitHubConfig domain.CodeSourceAppConfig) *CodeSourceService {
	return &CodeSourceService{
		fallbackGitHubConfig: fallbackGitHubConfig,
		githubClient:         githubClient,
		repository:           repository,
	}
}

func (s *CodeSourceService) ListCodeSources(ctx context.Context, ownerUnionID string) ([]domain.CodeSourceApp, error) {
	ownerUnionID = strings.TrimSpace(ownerUnionID)
	if ownerUnionID == "" {
		return nil, domain.ErrInvalidCodeSourceInput
	}

	installations, err := s.repository.ListCodeSourceInstallations(ctx, domain.CodeSourceProviderGitHub, ownerUnionID)
	if err != nil {
		return nil, err
	}

	_, configured, err := s.githubAppConfig(ctx)
	if err != nil {
		return nil, err
	}

	return []domain.CodeSourceApp{newGitHubCodeSourceApp(installations, configured)}, nil
}

func (s *CodeSourceService) GetAppManifest(_ context.Context, provider domain.CodeSourceProvider) (domain.CodeSourceAppManifest, error) {
	if !domain.IsValidCodeSourceProvider(provider) {
		return domain.CodeSourceAppManifest{}, domain.ErrCodeSourceProviderNotFound
	}
	if s.githubClient == nil {
		return domain.CodeSourceAppManifest{}, domain.ErrCodeSourceAuthNotConfigured
	}

	return s.githubClient.AppManifest()
}

func (s *CodeSourceService) GetInstallURL(ctx context.Context, provider domain.CodeSourceProvider, ownerUnionID string) (domain.CodeSourceInstallURL, error) {
	if !domain.IsValidCodeSourceProvider(provider) {
		return domain.CodeSourceInstallURL{}, domain.ErrCodeSourceProviderNotFound
	}
	ownerUnionID = strings.TrimSpace(ownerUnionID)
	if ownerUnionID == "" {
		return domain.CodeSourceInstallURL{}, domain.ErrInvalidCodeSourceInput
	}
	if s.githubClient == nil {
		return domain.CodeSourceInstallURL{}, domain.ErrCodeSourceAuthNotConfigured
	}
	config, configured, err := s.githubAppConfig(ctx)
	if err != nil {
		return domain.CodeSourceInstallURL{}, err
	}
	if !configured {
		return domain.CodeSourceInstallURL{}, domain.ErrCodeSourceAuthNotConfigured
	}
	if err := s.githubClient.ValidateApp(ctx, config); err != nil {
		if errors.Is(err, domain.ErrCodeSourceAppNotFound) {
			if deleteErr := s.repository.DeleteCodeSourceAppConfig(ctx, provider); deleteErr != nil && !errors.Is(deleteErr, domain.ErrCodeSourceAuthNotConfigured) {
				return domain.CodeSourceInstallURL{}, deleteErr
			}
			return domain.CodeSourceInstallURL{}, domain.ErrCodeSourceAuthNotConfigured
		}
		return domain.CodeSourceInstallURL{}, err
	}

	installURL, err := s.githubClient.InstallationURL(config)
	if err != nil {
		return domain.CodeSourceInstallURL{}, err
	}
	return domain.CodeSourceInstallURL{URL: installURL}, nil
}

func (s *CodeSourceService) CompleteAppManifest(ctx context.Context, provider domain.CodeSourceProvider, code string) (domain.CodeSourceInstallURL, error) {
	if !domain.IsValidCodeSourceProvider(provider) {
		return domain.CodeSourceInstallURL{}, domain.ErrCodeSourceProviderNotFound
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return domain.CodeSourceInstallURL{}, domain.ErrInvalidCodeSourceInput
	}
	if s.githubClient == nil {
		return domain.CodeSourceInstallURL{}, domain.ErrCodeSourceAuthNotConfigured
	}

	config, err := s.githubClient.CompleteAppManifest(ctx, code)
	if err != nil {
		return domain.CodeSourceInstallURL{}, err
	}
	config.Provider = provider
	saved, err := s.repository.UpsertCodeSourceAppConfig(ctx, config)
	if err != nil {
		return domain.CodeSourceInstallURL{}, err
	}

	installURL, err := s.githubClient.InstallationURL(saved)
	if err != nil {
		return domain.CodeSourceInstallURL{}, err
	}
	return domain.CodeSourceInstallURL{URL: installURL}, nil
}

func (s *CodeSourceService) CompleteInstallation(ctx context.Context, provider domain.CodeSourceProvider, ownerUnionID string, installationID int64, setupAction string) (domain.CodeSourceApp, error) {
	if !domain.IsValidCodeSourceProvider(provider) {
		return domain.CodeSourceApp{}, domain.ErrCodeSourceProviderNotFound
	}
	ownerUnionID = strings.TrimSpace(ownerUnionID)
	if ownerUnionID == "" {
		return domain.CodeSourceApp{}, domain.ErrInvalidCodeSourceInput
	}
	if installationID <= 0 {
		return domain.CodeSourceApp{}, domain.ErrInvalidCodeSourceInput
	}
	setupAction = strings.TrimSpace(setupAction)
	if setupAction != "" && setupAction != "install" && setupAction != "update" {
		return domain.CodeSourceApp{}, domain.ErrInvalidCodeSourceInput
	}
	if s.githubClient == nil {
		return domain.CodeSourceApp{}, domain.ErrCodeSourceAuthNotConfigured
	}
	config, configured, err := s.githubAppConfig(ctx)
	if err != nil {
		return domain.CodeSourceApp{}, err
	}
	if !configured {
		return domain.CodeSourceApp{}, domain.ErrCodeSourceAuthNotConfigured
	}

	installation, err := s.githubClient.GetInstallation(ctx, config, installationID)
	if err != nil {
		return domain.CodeSourceApp{}, err
	}
	installation.Provider = domain.CodeSourceProviderGitHub
	installation.OwnerUnionID = ownerUnionID
	if installation.InstalledAt.IsZero() {
		installation.InstalledAt = time.Now().UTC()
	}

	saved, err := s.repository.UpsertCodeSourceInstallation(ctx, installation)
	if err != nil {
		return domain.CodeSourceApp{}, err
	}

	return newGitHubCodeSourceApp([]domain.CodeSourceInstallation{saved}, true), nil
}

func (s *CodeSourceService) DeleteInstallations(ctx context.Context, provider domain.CodeSourceProvider, ownerUnionID string) error {
	if !domain.IsValidCodeSourceProvider(provider) {
		return domain.ErrCodeSourceProviderNotFound
	}
	ownerUnionID = strings.TrimSpace(ownerUnionID)
	if ownerUnionID == "" {
		return domain.ErrInvalidCodeSourceInput
	}
	if s.githubClient == nil {
		return domain.ErrCodeSourceAuthNotConfigured
	}
	config, configured, err := s.githubAppConfig(ctx)
	if err != nil {
		return err
	}
	if !configured {
		return domain.ErrCodeSourceAuthNotConfigured
	}

	installations, err := s.repository.ListCodeSourceInstallations(ctx, provider, ownerUnionID)
	if err != nil {
		return err
	}

	for _, installation := range installations {
		hasOtherOwner, err := s.repository.HasOtherCodeSourceInstallationOwner(ctx, provider, ownerUnionID, installation.InstallationID)
		if err != nil {
			return err
		}
		if hasOtherOwner {
			continue
		}
		if err := s.githubClient.DeleteInstallation(ctx, config, installation.InstallationID); err != nil && !errors.Is(err, domain.ErrCodeSourceInstallationNotFound) {
			return err
		}
	}

	return s.repository.DeleteCodeSourceInstallations(ctx, provider, ownerUnionID)
}

func (s *CodeSourceService) SearchRepositories(ctx context.Context, input domain.CodeSourceRepositorySearchInput) (domain.CodeSourceRepositorySearchResult, error) {
	normalized, err := normalizeCodeSourceRepositorySearchInput(input)
	if err != nil {
		return domain.CodeSourceRepositorySearchResult{}, err
	}
	if s.githubClient == nil {
		return domain.CodeSourceRepositorySearchResult{}, domain.ErrCodeSourceAuthNotConfigured
	}
	config, configured, err := s.githubAppConfig(ctx)
	if err != nil {
		return domain.CodeSourceRepositorySearchResult{}, err
	}
	if !configured {
		return domain.CodeSourceRepositorySearchResult{}, domain.ErrCodeSourceAuthNotConfigured
	}

	installations, err := s.repository.ListCodeSourceInstallations(ctx, normalized.Provider, normalized.OwnerUnionID)
	if err != nil {
		return domain.CodeSourceRepositorySearchResult{}, err
	}
	if len(installations) == 0 {
		return domain.CodeSourceRepositorySearchResult{}, domain.ErrCodeSourceInstallationNotFound
	}

	query := strings.ToLower(normalized.Query)
	seen := map[int64]struct{}{}
	results := make([]domain.CodeSourceRepository, 0, normalized.PageSize)
	for _, installation := range installations {
		for page := 1; ; page++ {
			repositoryPage, err := s.githubClient.ListRepositories(ctx, config, installation.InstallationID, page, githubRepositoryFetchPageSize)
			if err != nil {
				return domain.CodeSourceRepositorySearchResult{}, err
			}
			if err := s.repository.UpsertCodeSourceRepositories(ctx, normalized.OwnerUnionID, repositoryPage.Repositories); err != nil {
				return domain.CodeSourceRepositorySearchResult{}, err
			}

			for _, repository := range repositoryPage.Repositories {
				if _, ok := seen[repository.RepositoryID]; ok {
					continue
				}
				if !matchesCodeSourceRepositoryQuery(repository, query) {
					continue
				}
				seen[repository.RepositoryID] = struct{}{}
				results = append(results, repository)
				if len(results) >= normalized.PageSize {
					return domain.CodeSourceRepositorySearchResult{Repositories: results}, nil
				}
			}

			if !repositoryPage.HasMore {
				break
			}
		}
	}

	return domain.CodeSourceRepositorySearchResult{Repositories: results}, nil
}

func (s *CodeSourceService) githubAppConfig(ctx context.Context) (domain.CodeSourceAppConfig, bool, error) {
	config, err := s.repository.GetCodeSourceAppConfig(ctx, domain.CodeSourceProviderGitHub)
	if err == nil {
		return config, true, nil
	}
	if !errors.Is(err, domain.ErrCodeSourceAuthNotConfigured) {
		return domain.CodeSourceAppConfig{}, false, err
	}
	if isUsableGitHubAppConfig(s.fallbackGitHubConfig) {
		return s.fallbackGitHubConfig, true, nil
	}
	return domain.CodeSourceAppConfig{}, false, nil
}

func normalizeCodeSourceRepositorySearchInput(input domain.CodeSourceRepositorySearchInput) (domain.CodeSourceRepositorySearchInput, error) {
	normalized := domain.CodeSourceRepositorySearchInput{
		OwnerUnionID: strings.TrimSpace(input.OwnerUnionID),
		PageSize:     input.PageSize,
		Provider:     input.Provider,
		Query:        strings.TrimSpace(input.Query),
	}
	if normalized.OwnerUnionID == "" {
		return domain.CodeSourceRepositorySearchInput{}, domain.ErrInvalidCodeSourceInput
	}
	if !domain.IsValidCodeSourceProvider(normalized.Provider) {
		return domain.CodeSourceRepositorySearchInput{}, domain.ErrCodeSourceProviderNotFound
	}
	if normalized.PageSize == 0 {
		normalized.PageSize = defaultCodeSourceRepositoryPageSize
	}
	if normalized.PageSize < 1 || normalized.PageSize > maxCodeSourceRepositoryPageSize {
		return domain.CodeSourceRepositorySearchInput{}, domain.ErrInvalidCodeSourceInput
	}
	return normalized, nil
}

func matchesCodeSourceRepositoryQuery(repository domain.CodeSourceRepository, query string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(repository.FullName), query) ||
		strings.Contains(strings.ToLower(repository.Name), query) ||
		strings.Contains(strings.ToLower(repository.Owner), query) ||
		strings.Contains(strings.ToLower(repository.Description), query)
}

func isUsableGitHubAppConfig(config domain.CodeSourceAppConfig) bool {
	return strings.TrimSpace(config.AppID) != "" &&
		strings.TrimSpace(config.AppSlug) != "" &&
		strings.TrimSpace(config.PrivateKey) != ""
}

func newGitHubCodeSourceApp(installations []domain.CodeSourceInstallation, configured bool) domain.CodeSourceApp {
	configurationStatus := domain.CodeSourceAppConfigurationStatusNotConfigured
	if configured {
		configurationStatus = domain.CodeSourceAppConfigurationStatusConfigured
	}
	app := domain.CodeSourceApp{
		ConfigurationStatus: configurationStatus,
		Description:         "安装后可读取 GitHub Organization 下授权仓库，用于后续仓库绑定。",
		ID:                  domain.CodeSourceProviderGitHub,
		Name:                "GitHub App",
		Provider:            domain.CodeSourceProviderGitHub,
		Status:              domain.CodeSourceAppStatusNotInstalled,
	}
	if len(installations) > 0 {
		installedAt := installations[0].InstalledAt
		app.InstalledAt = &installedAt
		app.Status = domain.CodeSourceAppStatusInstalled
	}
	return app
}
