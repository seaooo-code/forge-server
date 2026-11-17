package domain

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"
)

var (
	ErrBuildAdapterNotFound        = errors.New("build adapter not found")
	ErrInvalidBuildAdapterInput    = errors.New("invalid build adapter request")
	ErrBuildAdapterDisabled        = errors.New("build adapter is disabled")
	ErrBuildAdapterVersionConflict = errors.New("build adapter was modified concurrently")
)

// BuildMode 是模块的构建方式：dockerfile 使用仓库自带 Dockerfile，adapter 使用平台维护的构建适配器。
type BuildMode string

const (
	BuildModeDockerfile BuildMode = "dockerfile"
	BuildModeAdapter    BuildMode = "adapter"
)

type BuildAdapterStatus string

const (
	BuildAdapterStatusActive   BuildAdapterStatus = "active"
	BuildAdapterStatusDisabled BuildAdapterStatus = "disabled"
)

// 构建适配器的大小限制，同时约束保存和渲染，避免超大模板拖慢 API 或 worker。
const (
	MaxBuildAdapterTemplateBytes     = 64 * 1024
	MaxBuildAdapterFileBytes         = 64 * 1024
	MaxBuildAdapterFiles             = 20
	MaxBuildAdapterFilePathLength    = 255
	MaxBuildAdapterParameters        = 50
	MaxBuildAdapterParamValueBytes   = 4 * 1024
	MaxBuildAdapterRenderedBytes     = 1024 * 1024
	MaxBuildAdapterNameLength        = 100
	MaxBuildAdapterDescriptionLength = 2000
	// BuildAdapterFilesDir 是辅助文件在构建上下文中的目录；仓库里已有该目录时构建失败。
	BuildAdapterFilesDir = ".forge"
)

var buildAdapterParameterKeyPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

// BuildAdapterParameter 是适配器声明的可配置项。参数值都是字符串，参数不允许存放密钥，密钥走模块构建变量。
type BuildAdapterParameter struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Default     string `json:"default"`
}

type BuildAdapter struct {
	ID                 string
	Name               string
	Description        string
	DockerfileTemplate string
	// Files 的键是相对 .forge/ 的路径，值是模板内容。
	Files           map[string]string
	Parameters      []BuildAdapterParameter
	Version         int
	Status          BuildAdapterStatus
	ModuleCount     int
	CreatedByUserID string
	CreatedBy       *UserSummary
	UpdatedByUserID string
	UpdatedBy       *UserSummary
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type BuildAdapterInput struct {
	Name               string
	Description        string
	DockerfileTemplate string
	Files              map[string]string
	Parameters         []BuildAdapterParameter
	UserID             string
}

// BuildAdapterPatch 是 PATCH 请求，nil 字段保持不变。
type BuildAdapterPatch struct {
	Name               *string
	Description        *string
	DockerfileTemplate *string
	Files              *map[string]string
	Parameters         *[]BuildAdapterParameter
	// ExpectedVersion 非 0 时，必须等于当前版本，否则返回 ErrBuildAdapterVersionConflict，
	// 用于拦截"打开编辑页之后被他人保存"的覆盖。
	ExpectedVersion int
	UserID          string
}

type BuildAdapterFilters struct {
	Status BuildAdapterStatus
}

// BuildAdapterRenderContext 是模板上下文。构建变量和密钥不进入上下文，仍通过 --build-arg / --secret 注入。
// 字段只使用字符串和整数，模板里没有可调用的方法。
type BuildAdapterRenderContext struct {
	Module      BuildAdapterRenderModule
	Environment string
	Branch      string
	CommitSHA   string
	Build       BuildAdapterRenderBuild
	Params      map[string]string
}

type BuildAdapterRenderModule struct {
	ID   string
	Name string
}

type BuildAdapterRenderBuild struct {
	ID     string
	Number int64
	// Time 是构建开始时间，UTC RFC 3339。
	Time string
}

type BuildAdapterRenderResult struct {
	Dockerfile       string
	DockerfileSHA256 string
	Files            map[string]string
}

// BuildAdapterRenderError 是模板解析或执行失败。Template 是 "Dockerfile" 或辅助文件路径，Line 从 1 开始，未知时为 0。
type BuildAdapterRenderError struct {
	Template string
	Line     int
	Message  string
}

func (e *BuildAdapterRenderError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s 第 %d 行：%s", e.Template, e.Line, e.Message)
	}
	return fmt.Sprintf("%s：%s", e.Template, e.Message)
}

// BuildAdapterPreviewInput 是渲染预览请求：ModuleID 为空时使用示例上下文；Params 为 nil 时使用模块已保存的参数。
type BuildAdapterPreviewInput struct {
	ModuleID string
	Params   map[string]string
}

// BuildAdapterUsage 是 worker 写回构建记录的适配器版本与渲染结果摘要。
type BuildAdapterUsage struct {
	AdapterID                string
	AdapterVersion           int
	RenderedDockerfileSHA256 string
}

func IsValidBuildMode(mode BuildMode) bool {
	return mode == BuildModeDockerfile || mode == BuildModeAdapter
}

func IsValidBuildAdapterStatus(status BuildAdapterStatus) bool {
	return status == BuildAdapterStatusActive || status == BuildAdapterStatusDisabled
}

func IsValidBuildAdapterParameterKey(key string) bool {
	return buildAdapterParameterKeyPattern.MatchString(key)
}

// IsReservedBuildAdapterFilePath 判断辅助文件路径是否落在平台保留的目录里（.forge/deps/ 用于依赖产物注入）。
func IsReservedBuildAdapterFilePath(value string) bool {
	return value == BuildDependenciesDir || strings.HasPrefix(value, BuildDependenciesDir+"/")
}

// IsValidBuildAdapterFilePath 要求辅助文件路径是规范化的相对路径，不能含 ..、反斜杠或控制字符。
func IsValidBuildAdapterFilePath(value string) bool {
	if value == "" || len(value) > MaxBuildAdapterFilePathLength || strings.Contains(value, "\\") || path.IsAbs(value) {
		return false
	}
	if path.Clean(value) != value || value == "." {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return false
		}
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}
