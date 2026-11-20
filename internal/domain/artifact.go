package domain

import (
	"errors"
	"io"
)

var (
	ErrArtifactNotFound     = errors.New("artifact not found")
	ErrInvalidArtifactInput = errors.New("invalid artifact request")
)

// 拉包清单中模块缺失的原因。
const (
	ArtifactMissingReasonReleaseNotFound     = "release not found"
	ArtifactMissingReasonBuildNotFound       = "build not found"
	ArtifactMissingReasonArtifactUnavailable = "build artifact not available"
	// 该运行时版本下模块没有任何生效路由。
	ArtifactMissingReasonNoReleaseForRuntime = "no_release_for_runtime"
	// 命中的构建不包含请求的平台。
	ArtifactMissingReasonPlatformUnavailable = "platform_unavailable"
)

type ArtifactDownloadInput struct {
	ApplicationSlug string
	ModuleID        string
	Environment     string
	BuildRecordID   string
	// Platform 非空时 Path 是去掉平台前缀后的路径，按 {Platform}/{Path} 查找文件。
	Platform string
	Path     string
}

type ArtifactManifestInput struct {
	ApplicationSlug string
	Environment     string
	Platform        string
	RuntimeVersion  string
	// UserID 是客户端上报的用户标识：用于匹配提测单成员和比例灰度分桶，为空时只按全量下发。
	UserID string
}

// ArtifactSource 说明清单里的模块是按哪条规则命中的。
type ArtifactSource string

const (
	ArtifactSourceTestPlan   ArtifactSource = "testPlan"
	ArtifactSourcePercentage ArtifactSource = "percentage"
	ArtifactSourceFull       ArtifactSource = "full"
)

type ArtifactApplication struct {
	ID   string
	Name string
	Slug string
}

type ArtifactEnvironment struct {
	ID   string
	Name string
}

type ArtifactManifestSource struct {
	Application ArtifactApplication
	Environment ArtifactEnvironment
	Modules     []ArtifactModuleReleaseSource
}

type ArtifactModuleReleaseSource struct {
	ID             string
	Name           string
	Kind           ModuleKind
	ReleaseID      string
	ReleaseType    ReleaseType
	GrayStrategy   ReleaseGrayStrategy
	GrayPercentage int
	BuildRecordID  string
	// TestPlanID 非空表示模块命中了提测单，此时发布相关字段为空，BuildRecordID 是提测单条目的构建。
	TestPlanID   string
	TestPlanName string
	// TestPlanMatches 是命中的提测单数量；正常情况下冲突规则保证最多 1 张，大于 1 说明并发写入留下了例外。
	TestPlanMatches int
	// HasRuntimeRoute 表示该运行时版本下模块存在生效路由（不论是否命中当前请求）。
	HasRuntimeRoute bool
}

type ArtifactApplicationManifest struct {
	Application    ArtifactApplication
	Environment    ArtifactEnvironment
	Platform       string
	RuntimeVersion string
	UserID         string
	Modules        []ArtifactModuleManifest
	MissingModules []ArtifactMissingModule
}

type ArtifactModuleManifest struct {
	ID             string
	Name           string
	Kind           ModuleKind
	ReleaseID      string
	ReleaseType    ReleaseType
	GrayStrategy   ReleaseGrayStrategy
	GrayPercentage int
	Source         ArtifactSource
	TestPlanID     string
	TestPlanName   string
	BuildRecordID  string
	Branch         string
	CommitSHA      string
	ArtifactKey    string
	ManifestKey    string
	Files          []ArtifactManifestResolvedFile
}

type ArtifactManifestResolvedFile struct {
	Path        string
	URL         string
	ContentType string
	SizeBytes   int64
	SHA256      string
	// Digests 来自清单文件项的 digests；旧清单没有该字段时用 sha256 补齐。
	Digests map[string]string
}

type ArtifactMissingModule struct {
	ID     string
	Name   string
	Kind   ModuleKind
	Reason string
}

type ArtifactObject struct {
	Body          io.ReadCloser
	ContentType   string
	ContentLength int64
	ETag          string
	Name          string
	ObjectKey     string
	Path          string
}
