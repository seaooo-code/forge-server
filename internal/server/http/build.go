package httpserver

import (
	"errors"
	"net/http"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type buildHandler struct {
	service BuildService
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
	ApplicationName          string `json:"applicationName"`
	ArtifactKey              string `json:"artifactKey"`
	ArtifactURL              string `json:"artifactUrl"`
	CreatedAt                string `json:"createdAt"`
	Branch                   string `json:"branch"`
	DefaultBranch            string `json:"defaultBranch"`
	Environment              string `json:"environment"`
	EnvironmentName          string `json:"environmentName"`
	ErrorMessage             string `json:"errorMessage"`
	FinishedAt               string `json:"finishedAt"`
	ID                       string `json:"id"`
	ModuleID                 string `json:"moduleId"`
	ModuleName               string `json:"moduleName"`
	QueuedAt                 string `json:"queuedAt"`
	RepositoryFullName       string `json:"repositoryFullName"`
	RepositoryID             string `json:"repositoryId"`
	RepositoryInstallationID string `json:"repositoryInstallationId"`
	RepositoryProvider       string `json:"repositoryProvider"`
	StartedAt                string `json:"startedAt"`
	Status                   string `json:"status"`
	StorageConfigurationID   string `json:"storageConfigurationId"`
	StorageConfigurationName string `json:"storageConfigurationName"`
	TriggerID                string `json:"triggerId"`
	TriggerType              string `json:"triggerType"`
	UpdatedAt                string `json:"updatedAt"`
	WorkerID                 string `json:"workerId"`
}

type buildRecordSummaryResponse struct {
	Failed    int `json:"failed"`
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Succeeded int `json:"succeeded"`
	Total     int `json:"total"`
}

func registerBuildRoutes(group *echo.Group, service BuildService) {
	handler := buildHandler{service: service}

	group.GET("/builds", handler.list)
	group.POST("/builds", handler.create)
	group.GET("/builds/:id", handler.get)
}

func (h buildHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "build service unavailable"})
	}

	records, summary, err := h.service.ListBuildRecords(c.Request().Context())
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

	return c.JSON(http.StatusOK, buildRecordDetailResponse{Build: newBuildRecordResponse(record)})
}

func (h buildHandler) create(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "build service unavailable"})
	}

	var request buildCreateRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid build request"})
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
	return buildRecordResponse{
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
