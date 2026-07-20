package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidModuleInput       = errors.New("invalid module request")
	ErrModuleInUse              = errors.New("module is referenced")
	ErrModuleNotFound           = errors.New("module not found")
	ErrModuleIDConflict         = errors.New("module id already exists")
	ErrModuleRepositoryConflict = errors.New("module repository already exists")
	ErrModuleCommonConflict     = errors.New("application already has common module")
	ErrModuleCommonMissing      = errors.New("feature module requires common module")
	ErrModuleCommonApplication  = errors.New("common module must belong to same application")
)

type ModuleKind string

const (
	ModuleKindCommon  ModuleKind = "Common"
	ModuleKindFeature ModuleKind = "Feature"
)

type ModuleRepositoryBinding struct {
	Provider       CodeSourceProvider
	RepositoryID   int64
	InstallationID int64
	FullName       string
	HTMLURL        string
	CloneURL       string
	SSHURL         string
	DefaultBranch  string
}

type Module struct {
	ApplicationID            string
	ApplicationName          string
	ApplicationSlug          string
	CreatedAt                time.Time
	DefaultBranch            string
	ID                       string
	Kind                     ModuleKind
	CommonModuleID           string
	CommonModuleName         string
	Name                     string
	Owner                    string
	OwnerAvatarURL           string
	OwnerDepartmentIDs       []string
	OwnerEmail               string
	OwnerUserID              string
	RepositoryCloneURL       string
	RepositoryFullName       string
	RepositoryHTMLURL        string
	RepositoryID             int64
	RepositoryInstallationID int64
	RepositoryProvider       CodeSourceProvider
	RepositorySSHURL         string
	UpdatedAt                time.Time
}

type ModuleInput struct {
	ApplicationID      string
	CommonModuleID     string
	ID                 string
	Kind               ModuleKind
	Name               string
	Owner              string
	OwnerAvatarURL     string
	OwnerDepartmentIDs []string
	OwnerEmail         string
	OwnerFeishuOpenID  string
	OwnerFeishuUnionID string
	OwnerFeishuUserID  string
	OwnerUserID        string
	Repository         ModuleRepositoryBinding
}

type ModuleListFilters struct {
	ApplicationID string
	Kind          ModuleKind
	Keyword       string
	Owner         string
	Pagination    Pagination
}

type ModuleSummary struct {
	Common  int
	Feature int
	Total   int
}

func IsValidModuleID(id string) bool {
	if id == "" || strings.TrimSpace(id) != id {
		return false
	}
	return !strings.ContainsAny(id, " \t\r\n")
}

func IsValidModuleKind(kind ModuleKind) bool {
	return kind == ModuleKindCommon || kind == ModuleKindFeature
}
