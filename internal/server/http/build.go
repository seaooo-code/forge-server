package httpserver

import (
	"errors"
	"net/http"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type buildHandler struct {
	authService AuthorizationService
	service     BuildService
}

type buildCreateRequest struct {
	ModuleID    string `json:"moduleId"`
	Environment string `json:"environment"`
	Branch      string `json:"branch"`
	TriggerID   string `json:"triggerId"`
	TriggerType string `json:"triggerType"`
}

type buildRecordDetailResponse struct {
	Build buildRecordResponse `json:"build"`
}

type buildRecordsResponse struct {
	Builds  []buildRecordResponse      `json:"builds"`
	Summary buildRecordSummaryResponse `json:"summary"`
}

type buildRecordResponse struct {
	ApplicationName          string                `json:"applicationName"`
	ArtifactTree             *artifactTreeResponse `json:"artifactTree,omitempty"`
	ArtifactKey              string                `json:"artifactKey"`
	ArtifactURL              string                `json:"artifactUrl"`
	CreatedAt                string                `json:"createdAt"`
	Branch                   string                `json:"branch"`
	DefaultBranch            string                `json:"defaultBranch"`
	Environment              string                `json:"environment"`
	EnvironmentName          string                `json:"environmentName"`
	ErrorMessage             string                `json:"errorMessage"`
	FinishedAt               string                `json:"finishedAt"`
	ID                       string                `json:"id"`
	ModuleID                 string                `json:"moduleId"`
	ModuleName               string                `json:"moduleName"`
	ManifestKey              string                `json:"manifestKey"`
	FileCount                int                   `json:"fileCount"`
	TotalSizeBytes           int64                 `json:"totalSizeBytes"`
	QueuedAt                 string                `json:"queuedAt"`
	RepositoryFullName       string                `json:"repositoryFullName"`
	RepositoryID             string                `json:"repositoryId"`
	RepositoryInstallationID string                `json:"repositoryInstallationId"`
	RepositoryProvider       string                `json:"repositoryProvider"`
	StartedAt                string                `json:"startedAt"`
	Status                   string                `json:"status"`
	StorageConfigurationID   string                `json:"storageConfigurationId"`
	StorageConfigurationName string                `json:"storageConfigurationName"`
	TriggerID                string                `json:"triggerId"`
	TriggerType              string                `json:"triggerType"`
	UpdatedAt                string                `json:"updatedAt"`
	WorkerID                 string                `json:"workerId"`
}

type artifactTreeResponse struct {
	FileCount      int                        `json:"fileCount"`
	TotalSizeBytes int64                      `json:"totalSizeBytes"`
	Nodes          []artifactTreeNodeResponse `json:"nodes"`
}

type artifactTreeNodeResponse struct {
	Children    []artifactTreeNodeResponse `json:"children,omitempty"`
	ContentType string                     `json:"contentType,omitempty"`
	Name        string                     `json:"name"`
	ObjectKey   string                     `json:"objectKey,omitempty"`
	Path        string                     `json:"path"`
	SHA256      string                     `json:"sha256,omitempty"`
	SizeBytes   int64                      `json:"sizeBytes"`
	Type        string                     `json:"type"`
}

type buildRecordSummaryResponse struct {
	Failed    int `json:"failed"`
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Succeeded int `json:"succeeded"`
	Total     int `json:"total"`
}

func registerBuildRoutes(group *echo.Group, service BuildService, authService AuthorizationService) {
	handler := buildHandler{authService: authService, service: service}

	group.GET("/builds", handler.list)
	group.POST("/builds", handler.create)
	group.GET("/builds/:id", handler.get)
}

func (h buildHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "build service unavailable"})
	}

	records, summary, err := h.service.ListBuildRecords(c.Request().Context(), domain.BuildRecordFilters{
		Environment: c.QueryParam("environment"),
		ModuleID:    c.QueryParam("moduleId"),
		Status:      domain.BuildStatus(c.QueryParam("status")),
		TriggerType: domain.BuildTriggerType(c.QueryParam("triggerType")),
	})
	if err != nil {
		return buildErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, buildRecordsResponse{
		Builds:  newBuildRecordResponses(records),
		Summary: newBuildRecordSummaryResponse(summary),
	})
}

func (h buildHandler) get(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "build service unavailable"})
	}

	record, err := h.service.GetBuildRecord(c.Request().Context(), c.Param("id"))
	if err != nil {
		return buildErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, buildRecordDetailResponse{Build: newBuildRecordDetailResponse(record)})
}

func (h buildHandler) create(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "build service unavailable"})
	}

	var request buildCreateRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid build request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionBuildCreate, domain.AuthorizationResource{ModuleID: request.ModuleID}); err != nil {
		return err
	}

	record, err := h.service.CreateBuildRecord(c.Request().Context(), domain.BuildRecordInput{
		ModuleID:    request.ModuleID,
		Environment: request.Environment,
		Branch:      request.Branch,
		TriggerID:   request.TriggerID,
		TriggerType: domain.BuildTriggerType(request.TriggerType),
	})
	if err != nil {
		return buildErrorResponse(c, err)
	}

	return c.JSON(http.StatusCreated, buildRecordDetailResponse{Build: newBuildRecordResponse(record)})
}

func buildErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidBuildInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid build request"})
	case errors.Is(err, domain.ErrModuleNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "module not found"})
	case errors.Is(err, domain.ErrEnvironmentNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "environment not found"})
	case errors.Is(err, domain.ErrStorageConfigurationNotFound):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "storage configuration not found"})
	case errors.Is(err, domain.ErrBuildRecordNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "build record not found"})
	case errors.Is(err, domain.ErrForbidden):
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func newBuildRecordResponses(records []domain.BuildRecord) []buildRecordResponse {
	responses := make([]buildRecordResponse, 0, len(records))
	for _, record := range records {
		responses = append(responses, newBuildRecordResponse(record))
	}
	return responses
}

func newBuildRecordResponse(record domain.BuildRecord) buildRecordResponse {
	return newBuildRecordResponseWithTree(record, false)
}

func newBuildRecordDetailResponse(record domain.BuildRecord) buildRecordResponse {
	return newBuildRecordResponseWithTree(record, true)
}

func newBuildRecordResponseWithTree(record domain.BuildRecord, includeArtifactTree bool) buildRecordResponse {
	response := buildRecordResponse{
		ApplicationName:          record.ApplicationName,
		ArtifactKey:              record.ArtifactKey,
		ArtifactURL:              record.ArtifactURL,
		CreatedAt:                formatTime(record.CreatedAt),
		Branch:                   record.Branch,
		DefaultBranch:            record.DefaultBranch,
		Environment:              record.Environment,
		EnvironmentName:          record.EnvironmentName,
		ErrorMessage:             record.ErrorMessage,
		FinishedAt:               formatOptionalTime(record.FinishedAt),
		ID:                       record.ID,
		ModuleID:                 record.ModuleID,
		ModuleName:               record.ModuleName,
		ManifestKey:              record.ManifestKey,
		FileCount:                record.FileCount,
		TotalSizeBytes:           record.TotalSizeBytes,
		QueuedAt:                 formatTime(record.QueuedAt),
		RepositoryFullName:       record.RepositoryFullName,
		RepositoryID:             int64String(record.RepositoryID),
		RepositoryInstallationID: int64String(record.RepositoryInstallationID),
		RepositoryProvider:       string(record.RepositoryProvider),
		StartedAt:                formatOptionalTime(record.StartedAt),
		Status:                   string(record.Status),
		StorageConfigurationID:   record.StorageConfigurationID,
		StorageConfigurationName: record.StorageConfigurationName,
		TriggerID:                record.TriggerID,
		TriggerType:              string(record.TriggerType),
		UpdatedAt:                formatTime(record.UpdatedAt),
		WorkerID:                 record.WorkerID,
	}
	if includeArtifactTree {
		tree := newArtifactTreeResponse(record.ArtifactTree)
		response.ArtifactTree = &tree
	}
	return response
}

func newArtifactTreeResponse(tree domain.ArtifactTree) artifactTreeResponse {
	nodes := make([]artifactTreeNodeResponse, 0, len(tree.Nodes))
	for _, node := range tree.Nodes {
		nodes = append(nodes, newArtifactTreeNodeResponse(node))
	}
	return artifactTreeResponse{
		FileCount:      tree.FileCount,
		TotalSizeBytes: tree.TotalSizeBytes,
		Nodes:          nodes,
	}
}

func newArtifactTreeNodeResponse(node domain.ArtifactTreeNode) artifactTreeNodeResponse {
	children := make([]artifactTreeNodeResponse, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, newArtifactTreeNodeResponse(child))
	}
	return artifactTreeNodeResponse{
		Children:    children,
		ContentType: node.ContentType,
		Name:        node.Name,
		ObjectKey:   node.ObjectKey,
		Path:        node.Path,
		SHA256:      node.SHA256,
		SizeBytes:   node.SizeBytes,
		Type:        node.Type,
	}
}

func newBuildRecordSummaryResponse(summary domain.BuildRecordSummary) buildRecordSummaryResponse {
	return buildRecordSummaryResponse{
		Failed:    summary.Failed,
		Pending:   summary.Pending,
		Running:   summary.Running,
		Succeeded: summary.Succeeded,
		Total:     summary.Total,
	}
}
