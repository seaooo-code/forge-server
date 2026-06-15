package service

import (
	"context"
	"errors"
	"strings"

	"forge-server/internal/domain"
)

const (
	defaultCodeSourceRepositoryPageSize = 20
	maxCodeSourceRepositoryPageSize     = 100
	githubRepositoryFetchPageSize       = 100
)

type CodeSourceRepositoryStore interface {
	DeleteCodeSourceInstallations(ctx context.Context, provider domain.CodeSourceProvider) error
	ListCodeSourceInstallations(ctx context.Context, provider domain.CodeSourceProvider) ([]domain.CodeSourceInstallation, error)
	UpsertCodeSourceInstallation(ctx context.Context, installation domain.CodeSourceInstallation) (domain.CodeSourceInstallation, error)
	UpsertCodeSourceRepositories(ctx context.Context, repositories []domain.CodeSourceRepository) error
}

type GitHubCodeSourceClient interface {
	DeleteInstallation(ctx context.Context, installationID int64) error
	GetInstallation(ctx context.Context, installationID int64) (domain.CodeSourceInstallation, error)
	InstallationURL() (string, error)
	ListRepositories(ctx context.Context, installationID int64, page int, pageSize int) (domain.CodeSourceRepositoryPage, error)
}

type CodeSourceService struct {
	githubClient GitHubCodeSourceClient
	repository   CodeSourceRepositoryStore
}

func NewCodeSourceService(repository CodeSourceRepositoryStore, githubClient GitHubCodeSourceClient) *CodeSourceService {
	return &CodeSourceService{
		githubClient: githubClient,
		repository:   repository,
	}
}

func (s *CodeSourceService) ListCodeSources(ctx context.Context) ([]domain.CodeSourceApp, error) {
	installations, err := s.repository.ListCodeSourceInstallations(ctx, domain.CodeSourceProviderGitHub)
	if err != nil {
		return nil, err
	}

	return []domain.CodeSourceApp{newGitHubCodeSourceApp(installations)}, nil
}

func (s *CodeSourceService) GetInstallURL(_ context.Context, provider domain.CodeSourceProvider) (domain.CodeSourceInstallURL, error) {
	if !domain.IsValidCodeSourceProvider(provider) {
		return domain.CodeSourceInstallURL{}, domain.ErrCodeSourceProviderNotFound
	}
	if s.githubClient == nil {
		return domain.CodeSourceInstallURL{}, domain.ErrCodeSourceAuthNotConfigured
	}

	installURL, err := s.githubClient.InstallationURL()
	if err != nil {
		return domain.CodeSourceInstallURL{}, err
	}
	return domain.CodeSourceInstallURL{URL: installURL}, nil
}

func (s *CodeSourceService) CompleteInstallation(ctx context.Context, provider domain.CodeSourceProvider, installationID int64, setupAction string) (domain.CodeSourceApp, error) {
	if !domain.IsValidCodeSourceProvider(provider) {
		return domain.CodeSourceApp{}, domain.ErrCodeSourceProviderNotFound
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

	installation, err := s.githubClient.GetInstallation(ctx, installationID)
	if err != nil {
		return domain.CodeSourceApp{}, err
	}
	installation.Provider = domain.CodeSourceProviderGitHub

	saved, err := s.repository.UpsertCodeSourceInstallation(ctx, installation)
	if err != nil {
		return domain.CodeSourceApp{}, err
	}

	return newGitHubCodeSourceApp([]domain.CodeSourceInstallation{saved}), nil
}

func (s *CodeSourceService) DeleteInstallations(ctx context.Context, provider domain.CodeSourceProvider) error {
	if !domain.IsValidCodeSourceProvider(provider) {
		return domain.ErrCodeSourceProviderNotFound
	}
	if s.githubClient == nil {
		return domain.ErrCodeSourceAuthNotConfigured
	}

	installations, err := s.repository.ListCodeSourceInstallations(ctx, provider)
	if err != nil {
		return err
	}
	if len(installations) == 0 {
		return domain.ErrCodeSourceInstallationNotFound
	}

	for _, installation := range installations {
		if err := s.githubClient.DeleteInstallation(ctx, installation.InstallationID); err != nil && !errors.Is(err, domain.ErrCodeSourceInstallationNotFound) {
			return err
		}
	}

	return s.repository.DeleteCodeSourceInstallations(ctx, provider)
}

func (s *CodeSourceService) SearchRepositories(ctx context.Context, input domain.CodeSourceRepositorySearchInput) (domain.CodeSourceRepositorySearchResult, error) {
	normalized, err := normalizeCodeSourceRepositorySearchInput(input)
	if err != nil {
		return domain.CodeSourceRepositorySearchResult{}, err
	}
	if s.githubClient == nil {
		return domain.CodeSourceRepositorySearchResult{}, domain.ErrCodeSourceAuthNotConfigured
	}

	installations, err := s.repository.ListCodeSourceInstallations(ctx, normalized.Provider)
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
			repositoryPage, err := s.githubClient.ListRepositories(ctx, installation.InstallationID, page, githubRepositoryFetchPageSize)
			if err != nil {
				return domain.CodeSourceRepositorySearchResult{}, err
			}
			if err := s.repository.UpsertCodeSourceRepositories(ctx, repositoryPage.Repositories); err != nil {
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

func normalizeCodeSourceRepositorySearchInput(input domain.CodeSourceRepositorySearchInput) (domain.CodeSourceRepositorySearchInput, error) {
	normalized := domain.CodeSourceRepositorySearchInput{
		PageSize: input.PageSize,
		Provider: input.Provider,
		Query:    strings.TrimSpace(input.Query),
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

func newGitHubCodeSourceApp(installations []domain.CodeSourceInstallation) domain.CodeSourceApp {
	app := domain.CodeSourceApp{
		Description: "安装后可读取 GitHub Organization 下授权仓库，用于后续仓库绑定。",
		ID:          domain.CodeSourceProviderGitHub,
		Name:        "GitHub App",
		Provider:    domain.CodeSourceProviderGitHub,
		Status:      domain.CodeSourceAppStatusNotInstalled,
	}
	if len(installations) > 0 {
		installedAt := installations[0].InstalledAt
		app.InstalledAt = &installedAt
		app.Status = domain.CodeSourceAppStatusInstalled
	}
	return app
}
