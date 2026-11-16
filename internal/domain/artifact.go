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
	Identifier      string
	UserID          string
}

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
	Identifier     string
	BuildRecordID  string
	// HasRuntimeRoute 表示该运行时版本下模块存在生效路由（不论是否命中当前请求）。
	HasRuntimeRoute bool
}

type ArtifactApplicationManifest struct {
	Application    ArtifactApplication
	Environment    ArtifactEnvironment
	Platform       string
	RuntimeVersion string
	Identifier     string
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
	Identifier     string
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
