package httpserver

import (
	"errors"
	"mime"
	"net/http"
	"strconv"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type artifactHandler struct {
	service ArtifactService
}

type artifactManifestResponse struct {
	Application    artifactApplicationResponse      `json:"application"`
	Environment    artifactEnvironmentResponse      `json:"environment"`
	Platform       string                           `json:"platform"`
	RuntimeVersion string                           `json:"runtimeVersion"`
	Identifier     string                           `json:"identifier"`
	Modules        []artifactModuleManifestResponse `json:"modules"`
	MissingModules []artifactMissingModuleResponse  `json:"missingModules"`
}

type artifactApplicationResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type artifactEnvironmentResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type artifactModuleManifestResponse struct {
	ID             string                         `json:"id"`
	Name           string                         `json:"name"`
	Kind           string                         `json:"kind"`
	ReleaseID      string                         `json:"releaseId"`
	ReleaseType    string                         `json:"releaseType"`
	GrayStrategy   string                         `json:"grayStrategy"`
	GrayPercentage int                            `json:"grayPercentage"`
	Identifier     string                         `json:"identifier"`
	BuildRecordID  string                         `json:"buildRecordId"`
	Branch         string                         `json:"branch"`
	CommitSHA      string                         `json:"commitSha"`
	ArtifactKey    string                         `json:"artifactKey"`
	ManifestKey    string                         `json:"manifestKey"`
	Files          []artifactManifestFileResponse `json:"files"`
}

type artifactManifestFileResponse struct {
	Path        string `json:"path"`
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
	SHA256      string `json:"sha256"`
	// Digests 键为算法名（sha256、md5），由 FORGE_ARTIFACT_DIGESTS 决定。
	Digests map[string]string `json:"digests"`
}

type artifactMissingModuleResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

func registerArtifactRoutes(group *echo.Group, service ArtifactService, tokenService ArtifactAccessTokenService, authService FeishuAuthService) {
	handler := artifactHandler{service: service}

	group.GET("/artifacts/applications/:applicationSlug/modules/:moduleId/environments/:environment/builds/:buildId/file", handler.downloadFile, requireArtifactAccess(authService, tokenService))
	group.GET("/artifacts/applications/:applicationSlug/environments/:environment/manifest", handler.manifest, requireArtifactToken(tokenService))
}

func (h artifactHandler) downloadFile(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "artifact service unavailable"})
	}

	object, err := h.service.DownloadArtifactFile(c.Request().Context(), domain.ArtifactDownloadInput{
		ApplicationSlug: c.Param("applicationSlug"),
		ModuleID:        c.Param("moduleId"),
		Environment:     c.Param("environment"),
		BuildRecordID:   c.Param("buildId"),
		Platform:        c.QueryParam("platform"),
		Path:            c.QueryParam("path"),
	})
	if err != nil {
		return artifactErrorResponse(c, err)
	}
	defer object.Body.Close()

	contentType := object.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if object.ContentLength > 0 {
		c.Response().Header().Set(echo.HeaderContentLength, strconv.FormatInt(object.ContentLength, 10))
	}
	if object.ETag != "" {
		c.Response().Header().Set("ETag", object.ETag)
	}
	if object.Name != "" {
		c.Response().Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
			"filename": object.Name,
		}))
	}
	return c.Stream(http.StatusOK, contentType, object.Body)
}

func (h artifactHandler) manifest(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "artifact service unavailable"})
	}

	manifest, err := h.service.GetApplicationManifest(c.Request().Context(), domain.ArtifactManifestInput{
		ApplicationSlug: c.Param("applicationSlug"),
		Environment:     c.Param("environment"),
		Platform:        c.QueryParam("platform"),
		RuntimeVersion:  c.QueryParam("runtimeVersion"),
		Identifier:      c.QueryParam("identifier"),
		UserID:          c.QueryParam("userId"),
	})
	if err != nil {
		return artifactErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newArtifactManifestResponse(manifest))
}

func artifactErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidArtifactInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid artifact request"})
	case errors.Is(err, domain.ErrArtifactNotFound),
		errors.Is(err, domain.ErrApplicationNotFound),
		errors.Is(err, domain.ErrEnvironmentNotFound),
		errors.Is(err, domain.ErrBuildRecordNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "artifact not found"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func newArtifactManifestResponse(manifest domain.ArtifactApplicationManifest) artifactManifestResponse {
	return artifactManifestResponse{
		Application: artifactApplicationResponse{
			ID:   manifest.Application.ID,
			Name: manifest.Application.Name,
			Slug: manifest.Application.Slug,
		},
		Environment: artifactEnvironmentResponse{
			ID:   manifest.Environment.ID,
			Name: manifest.Environment.Name,
		},
		Platform:       manifest.Platform,
		RuntimeVersion: manifest.RuntimeVersion,
		Identifier:     manifest.Identifier,
		Modules:        newArtifactModuleManifestResponses(manifest.Modules),
		MissingModules: newArtifactMissingModuleResponses(manifest.MissingModules),
	}
}

func newArtifactModuleManifestResponses(modules []domain.ArtifactModuleManifest) []artifactModuleManifestResponse {
	responses := make([]artifactModuleManifestResponse, 0, len(modules))
	for _, module := range modules {
		responses = append(responses, artifactModuleManifestResponse{
			ID:             module.ID,
			Name:           module.Name,
			Kind:           string(module.Kind),
			ReleaseID:      module.ReleaseID,
			ReleaseType:    string(module.ReleaseType),
			GrayStrategy:   string(module.GrayStrategy),
			GrayPercentage: module.GrayPercentage,
			Identifier:     module.Identifier,
			BuildRecordID:  module.BuildRecordID,
			Branch:         module.Branch,
			CommitSHA:      module.CommitSHA,
			ArtifactKey:    module.ArtifactKey,
			ManifestKey:    module.ManifestKey,
			Files:          newArtifactManifestFileResponses(module.Files),
		})
	}
	return responses
}

func newArtifactManifestFileResponses(files []domain.ArtifactManifestResolvedFile) []artifactManifestFileResponse {
	responses := make([]artifactManifestFileResponse, 0, len(files))
	for _, file := range files {
		digests := file.Digests
		if digests == nil {
			digests = map[string]string{}
		}
		responses = append(responses, artifactManifestFileResponse{
			Path:        file.Path,
			URL:         file.URL,
			ContentType: file.ContentType,
			SizeBytes:   file.SizeBytes,
			SHA256:      file.SHA256,
			Digests:     digests,
		})
	}
	return responses
}

func newArtifactMissingModuleResponses(modules []domain.ArtifactMissingModule) []artifactMissingModuleResponse {
	responses := make([]artifactMissingModuleResponse, 0, len(modules))
	for _, module := range modules {
		responses = append(responses, artifactMissingModuleResponse{
			ID:     module.ID,
			Name:   module.Name,
			Kind:   string(module.Kind),
			Reason: module.Reason,
		})
	}
	return responses
}
