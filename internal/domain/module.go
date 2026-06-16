package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidModuleInput = errors.New("invalid module request")
	ErrModuleInUse        = errors.New("module is referenced")
	ErrModuleNotFound     = errors.New("module not found")
	ErrModuleIDConflict   = errors.New("module id already exists")
)

type ModuleKind string

const (
	ModuleKindCommon  ModuleKind = "Common"
	ModuleKindFeature ModuleKind = "Feature"
)

type ModuleStatus string

const (
	ModuleStatusActive ModuleStatus = "active"
	ModuleStatusFrozen ModuleStatus = "frozen"
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
	Status                   ModuleStatus
	UpdatedAt                time.Time
}

type ModuleInput struct {
	ApplicationID      string
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
	Status             ModuleStatus
}

type ModuleListFilters struct {
	Kind    ModuleKind
	Keyword string
	Owner   string
	Status  ModuleStatus
}

type ModuleSummary struct {
	Active  int
	Common  int
	Feature int
	Frozen  int
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

func IsValidModuleStatus(status ModuleStatus) bool {
	return status == ModuleStatusActive || status == ModuleStatusFrozen
}

func (s ModuleStatus) Label() string {
	switch s {
	case ModuleStatusActive:
		return "活跃"
	case ModuleStatusFrozen:
		return "冻结"
	default:
		return string(s)
	}
}
