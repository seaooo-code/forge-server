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
	DefaultModuleDockerfilePath = "Dockerfile"
	DefaultModuleBuildContext   = "."
	DefaultModuleCIArtifactPath = "dist"
)

// ModuleBuildConfig 是模块的构建与自动触发配置。
type ModuleBuildConfig struct {
	// BuildMode 为空时按 dockerfile 处理；DockerfilePath 只在 dockerfile 模式下生效。
	BuildMode          BuildMode
	BuildAdapterID     string
	BuildAdapterName   string
	BuildAdapterParams map[string]string
	DockerfilePath     string
	BuildContext       string
	// CIJob、CIArtifactPath 只在 gitlab_ci 模式下生效：CIJob 是产出构建产物的 job 名，
	// CIArtifactPath 是该 job 的 artifacts 里产物根目录的相对路径，默认 dist。
	CIJob                string
	CIArtifactPath       string
	AutoBuildBranches    []string
	AutoBuildEnvironment string
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
