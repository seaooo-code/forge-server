package domain

import (
	"errors"
	"time"
)

var (
	ErrCodeSourceAppNotFound          = errors.New("code source app not found")
	ErrCodeSourceAuthNotConfigured    = errors.New("code source auth not configured")
	ErrCodeSourceAuthorization        = errors.New("code source authorization failed")
	ErrCodeSourceInstallationNotFound = errors.New("code source installation not found")
	ErrCodeSourceProviderNotFound     = errors.New("code source provider not found")
	ErrInvalidCodeSourceInput         = errors.New("invalid code source request")
)

type CodeSourceProvider string

const (
	CodeSourceProviderGitHub CodeSourceProvider = "github"
)

type CodeSourceAppStatus string

const (
	CodeSourceAppStatusInstalled    CodeSourceAppStatus = "installed"
	CodeSourceAppStatusNotInstalled CodeSourceAppStatus = "notInstalled"
)

type CodeSourceAppConfigurationStatus string

const (
	CodeSourceAppConfigurationStatusConfigured    CodeSourceAppConfigurationStatus = "configured"
	CodeSourceAppConfigurationStatusNotConfigured CodeSourceAppConfigurationStatus = "notConfigured"
)

type CodeSourceApp struct {
	ConfigurationStatus CodeSourceAppConfigurationStatus
	Description         string
	ID                  CodeSourceProvider
	InstalledAt         *time.Time
	Name                string
	Provider            CodeSourceProvider
	Status              CodeSourceAppStatus
}

type CodeSourceInstallURL struct {
	URL string
}

type CodeSourceAppConfig struct {
	AppID         string
	AppSlug       string
	HTMLURL       string
	Name          string
	PrivateKey    string
	Provider      CodeSourceProvider
	WebhookSecret string
}

type CodeSourceAppManifest struct {
	ActionURL string
	Payload   string
}

type CodeSourceInstallation struct {
	AccountLogin        string
	AccountType         string
	DeletedAt           *time.Time
	InstallationID      int64
	InstalledAt         time.Time
	OwnerUnionID        string
	Provider            CodeSourceProvider
	RepositorySelection string
	UpdatedAt           time.Time
}

type CodeSourceRepository struct {
	CloneURL       string
	DefaultBranch  string
	Description    string
	FullName       string
	HTMLURL        string
	InstallationID int64
	Name           string
	Owner          string
	Private        bool
	Provider       CodeSourceProvider
	RepositoryID   int64
	SSHURL         string
	UpdatedAt      *time.Time
}

type CodeSourceRepositoryPage struct {
	HasMore      bool
	Repositories []CodeSourceRepository
}

type CodeSourceRepositorySearchInput struct {
	OwnerUnionID string
	PageSize     int
	Provider     CodeSourceProvider
	Query        string
}

type CodeSourceRepositorySearchResult struct {
	Repositories []CodeSourceRepository
}

func IsValidCodeSourceProvider(provider CodeSourceProvider) bool {
	return provider == CodeSourceProviderGitHub
}
