package domain

import (
	"errors"
	"io"
)

var (
	ErrArtifactNotFound     = errors.New("artifact not found")
	ErrInvalidArtifactInput = errors.New("invalid artifact request")
)

type ArtifactDownloadInput struct {
	ApplicationSlug string
	ModuleID        string
	Environment     string
	BuildRecordID   string
	Path            string
}

type ArtifactManifestInput struct {
	ApplicationSlug string
	Environment     string
	Identifier      string
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
	ID            string
	Name          string
	Kind          ModuleKind
	ReleaseID     string
	ReleaseType   ReleaseType
	Identifier    string
	BuildRecordID string
}

type ArtifactApplicationManifest struct {
	Application    ArtifactApplication
	Environment    ArtifactEnvironment
	Identifier     string
	Modules        []ArtifactModuleManifest
	MissingModules []ArtifactMissingModule
}

type ArtifactModuleManifest struct {
	ID            string
	Name          string
	Kind          ModuleKind
	ReleaseID     string
	ReleaseType   ReleaseType
	Identifier    string
	BuildRecordID string
	ArtifactKey   string
	ManifestKey   string
	Files         []ArtifactManifestResolvedFile
}

type ArtifactManifestResolvedFile struct {
	Path        string
	URL         string
	ContentType string
	SizeBytes   int64
	SHA256      string
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
