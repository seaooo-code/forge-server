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

const (
	DefaultModuleDockerfilePath      = "Dockerfile"
	DefaultModuleBuildContext        = "."
	DefaultModuleAutoPreviewTTLHours = 168
)

// ModuleBuildConfig 是模块的构建与自动触发配置。
type ModuleBuildConfig struct {
	DockerfilePath       string
	BuildContext         string
	AutoBuildBranches    []string
	AutoBuildEnvironment string
	AutoPreviewRelease   bool
	AutoPreviewTTLHours  int
}

type Module struct {
	ApplicationID      string
	ApplicationName    string
	ApplicationSlug    string
	BuildConfig        ModuleBuildConfig
	CodeSourceProvider CodeSourceProvider
	ConnectionName     string
	CreatedAt          time.Time
	ID                 string
	Kind               ModuleKind
	CommonModuleID     string
	CommonModuleName   string
	Name               string
	Owner              string
	OwnerAvatarURL     string
	OwnerDepartmentIDs []string
	OwnerEmail         string
	OwnerUserID        string
	Repository         ModuleRepository
	UpdatedAt          time.Time
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
	Repository         ModuleRepository
	BuildConfig        ModuleBuildConfig
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
