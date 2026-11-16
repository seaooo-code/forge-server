package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"forge-server/internal/domain"
)

// GitHubAppClient 承载 GitHub App 特有的创建与安装流程（code_source_app_configs 只服务于这套机制）。
// 安装完成后按 installation 写入通用的 code_source_connections，后续仓库、凭证、webhook 都走 CodeSourceProvider。
type GitHubAppClient interface {
	// AppConfig 返回当前生效的 App 级配置（数据库优先，其次环境变量），configured=false 表示未配置。
	AppConfig(ctx context.Context) (domain.CodeSourceAppConfig, bool, error)
	AppManifest() (domain.CodeSourceAppManifest, error)
	CompleteAppManifest(ctx context.Context, code string) (domain.CodeSourceAppConfig, error)
	GetInstallation(ctx context.Context, config domain.CodeSourceAppConfig, installationID int64) (domain.CodeSourceInstallation, error)
	InstallationURL(config domain.CodeSourceAppConfig) (string, error)
	ValidateApp(ctx context.Context, config domain.CodeSourceAppConfig) error
}

func (s *CodeSourceService) ListCodeSources(ctx context.Context) ([]domain.CodeSourceApp, error) {
	app := domain.CodeSourceApp{
		ConfigurationStatus: domain.CodeSourceAppConfigurationStatusNotConfigured,
		Description:         "安装后可读取 GitHub Organization 下授权仓库，每个 installation 对应一个代码源连接。",
		ID:                  domain.CodeSourceProviderGitHub,
		Name:                "GitHub App",
		Provider:            domain.CodeSourceProviderGitHub,
		Status:              domain.CodeSourceAppStatusNotInstalled,
	}
	if s.githubApp != nil {
		_, configured, err := s.githubApp.AppConfig(ctx)
		if err != nil {
			return nil, err
		}
		if configured {
			app.ConfigurationStatus = domain.CodeSourceAppConfigurationStatusConfigured
		}
	}

	connections, err := s.store.ListCodeSourceConnections(ctx, domain.CodeSourceConnectionFilters{
		Provider: domain.CodeSourceProviderGitHub,
		Status:   domain.CodeSourceConnectionStatusActive,
	})
	if err != nil {
		return nil, err
	}
	app.ConnectionCount = len(connections)
	if len(connections) > 0 {
		installedAt := connections[0].CreatedAt
		app.InstalledAt = &installedAt
		app.Status = domain.CodeSourceAppStatusInstalled
	}
	return []domain.CodeSourceApp{app}, nil
}

func (s *CodeSourceService) GetAppManifest(_ context.Context) (domain.CodeSourceAppManifest, error) {
	if s.githubApp == nil {
		return domain.CodeSourceAppManifest{}, domain.ErrCodeSourceAuthNotConfigured
	}
	return s.githubApp.AppManifest()
}

func (s *CodeSourceService) GetInstallURL(ctx context.Context) (domain.CodeSourceInstallURL, error) {
	config, err := s.configuredGitHubApp(ctx)
	if err != nil {
		return domain.CodeSourceInstallURL{}, err
	}
	if err := s.githubApp.ValidateApp(ctx, config); err != nil {
		if errors.Is(err, domain.ErrCodeSourceAppNotFound) {
			if deleteErr := s.store.DeleteCodeSourceAppConfig(ctx, domain.CodeSourceProviderGitHub); deleteErr != nil && !errors.Is(deleteErr, domain.ErrCodeSourceAuthNotConfigured) {
				return domain.CodeSourceInstallURL{}, deleteErr
			}
			return domain.CodeSourceInstallURL{}, domain.ErrCodeSourceAuthNotConfigured
		}
		return domain.CodeSourceInstallURL{}, err
	}

	installURL, err := s.githubApp.InstallationURL(config)
	if err != nil {
		return domain.CodeSourceInstallURL{}, err
	}
	return domain.CodeSourceInstallURL{URL: installURL}, nil
}

func (s *CodeSourceService) CompleteAppManifest(ctx context.Context, code string) (domain.CodeSourceInstallURL, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return domain.CodeSourceInstallURL{}, domain.ErrInvalidCodeSourceInput
	}
	if s.githubApp == nil {
		return domain.CodeSourceInstallURL{}, domain.ErrCodeSourceAuthNotConfigured
	}

	config, err := s.githubApp.CompleteAppManifest(ctx, code)
	if err != nil {
		return domain.CodeSourceInstallURL{}, err
	}
	config.Provider = domain.CodeSourceProviderGitHub
	saved, err := s.store.UpsertCodeSourceAppConfig(ctx, config)
	if err != nil {
		return domain.CodeSourceInstallURL{}, err
	}

	installURL, err := s.githubApp.InstallationURL(saved)
	if err != nil {
		return domain.CodeSourceInstallURL{}, err
	}
	return domain.CodeSourceInstallURL{URL: installURL}, nil
}

// CompleteInstallation 处理 GitHub App 安装回调：每个 installation 对应一条连接，创建人记录为平台 users.id。
func (s *CodeSourceService) CompleteInstallation(ctx context.Context, userID string, installationID int64, setupAction string) (domain.CodeSourceConnection, error) {
	userID = strings.TrimSpace(userID)
	setupAction = strings.TrimSpace(setupAction)
	if userID == "" || installationID <= 0 || (setupAction != "" && setupAction != "install" && setupAction != "update") {
		return domain.CodeSourceConnection{}, domain.ErrInvalidCodeSourceInput
	}
	config, err := s.configuredGitHubApp(ctx)
	if err != nil {
		return domain.CodeSourceConnection{}, err
	}

	installation, err := s.githubApp.GetInstallation(ctx, config, installationID)
	if err != nil {
		return domain.CodeSourceConnection{}, err
	}
	now := s.now().UTC()
	externalID := strconv.FormatInt(installation.InstallationID, 10)

	existing, err := s.store.FindCodeSourceConnectionByExternalID(ctx, domain.CodeSourceProviderGitHub, externalID)
	switch {
	case err == nil:
		existing.ExternalAccount = installation.AccountLogin
		existing.Status = domain.CodeSourceConnectionStatusActive
		existing.LastVerifiedAt = &now
		return s.store.UpdateCodeSourceConnection(ctx, existing)
	case !errors.Is(err, domain.ErrCodeSourceConnectionNotFound):
		return domain.CodeSourceConnection{}, err
	}

	name := strings.TrimSpace(installation.AccountLogin)
	if name == "" {
		name = "GitHub installation " + externalID
	}
	return s.store.CreateCodeSourceConnection(ctx, domain.CodeSourceConnection{
		Provider:        domain.CodeSourceProviderGitHub,
		Name:            name,
		WebBaseURL:      installation.WebBaseURL,
		APIBaseURL:      installation.APIBaseURL,
		ExternalAccount: installation.AccountLogin,
		ExternalID:      externalID,
		Status:          domain.CodeSourceConnectionStatusActive,
		CreatedByUserID: userID,
		LastVerifiedAt:  &now,
	})
}

func (s *CodeSourceService) configuredGitHubApp(ctx context.Context) (domain.CodeSourceAppConfig, error) {
	if s.githubApp == nil {
		return domain.CodeSourceAppConfig{}, domain.ErrCodeSourceAuthNotConfigured
	}
	config, configured, err := s.githubApp.AppConfig(ctx)
	if err != nil {
		return domain.CodeSourceAppConfig{}, err
	}
	if !configured {
		return domain.CodeSourceAppConfig{}, domain.ErrCodeSourceAuthNotConfigured
	}
	return config, nil
}
