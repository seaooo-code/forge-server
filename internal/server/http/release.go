package httpserver

import (
	"errors"
	"net/http"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type releaseHandler struct {
	service ReleaseService
}

type releaseCreateRequest struct {
	BuildRecordID   string `json:"buildRecordId"`
	ReleaseType     string `json:"releaseType"`
	Identifier      string `json:"identifier"`
	Description     string `json:"description"`
	CreatedByUserID string `json:"createdByUserId"`
}

type releaseDetailResponse struct {
	Release releaseRecordResponse `json:"release"`
	Route   releaseRouteResponse  `json:"route"`
}

type releasesResponse struct {
	Releases []releaseRecordResponse `json:"releases"`
}

type releaseResolveResponse struct {
	Route releaseRouteResponse `json:"route"`
}

type releaseRecordResponse struct {
	ApplicationName string `json:"applicationName"`
	ArtifactKey     string `json:"artifactKey"`
	ArtifactURL     string `json:"artifactUrl"`
	BuildRecordID   string `json:"buildRecordId"`
	CreatedAt       string `json:"createdAt"`
	CreatedBy       string `json:"createdBy"`
	CreatedByUserID string `json:"createdByUserId"`
	Description     string `json:"description"`
	Environment     string `json:"environment"`
	EnvironmentName string `json:"environmentName"`
	ID              string `json:"id"`
	Identifier      string `json:"identifier"`
	ModuleID        string `json:"moduleId"`
	ModuleName      string `json:"moduleName"`
	ReleaseType     string `json:"releaseType"`
	Status          string `json:"status"`
	UpdatedAt       string `json:"updatedAt"`
}

type releaseRouteResponse struct {
	ApplicationName string `json:"applicationName"`
	ArtifactKey     string `json:"artifactKey"`
	ArtifactURL     string `json:"artifactUrl"`
	BuildRecordID   string `json:"buildRecordId"`
	CreatedAt       string `json:"createdAt"`
	CreatedBy       string `json:"createdBy"`
	CreatedByUserID string `json:"createdByUserId"`
	Environment     string `json:"environment"`
	EnvironmentName string `json:"environmentName"`
	ID              string `json:"id"`
	Identifier      string `json:"identifier"`
	ModuleID        string `json:"moduleId"`
	ModuleName      string `json:"moduleName"`
	ReleaseID       string `json:"releaseId"`
	RouteType       string `json:"routeType"`
	Status          string `json:"status"`
	UpdatedAt       string `json:"updatedAt"`
}

func registerReleaseRoutes(group *echo.Group, service ReleaseService) {
	handler := releaseHandler{service: service}

	group.GET("/releases", handler.list)
	group.POST("/releases", handler.create)
	group.GET("/releases/resolve", handler.resolve)
}

func (h releaseHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release service unavailable"})
	}

	records, err := h.service.ListReleases(
		c.Request().Context(),
		c.QueryParam("moduleId"),
		c.QueryParam("environment"),
	)
	if err != nil {
		return releaseErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, releasesResponse{Releases: newReleaseRecordResponses(records)})
}

func (h releaseHandler) create(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release service unavailable"})
	}

	var request releaseCreateRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid release request"})
	}

	record, route, err := h.service.CreateRelease(c.Request().Context(), domain.ReleaseCreateInput{
		BuildRecordID:   request.BuildRecordID,
		ReleaseType:     domain.ReleaseType(request.ReleaseType),
		Identifier:      request.Identifier,
		Description:     request.Description,
		CreatedByUserID: request.CreatedByUserID,
	})
	if err != nil {
		return releaseErrorResponse(c, err)
	}

	return c.JSON(http.StatusCreated, releaseDetailResponse{
		Release: newReleaseRecordResponse(record),
		Route:   newReleaseRouteResponse(route),
	})
}

func (h releaseHandler) resolve(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release service unavailable"})
	}

	route, err := h.service.ResolveReleaseRoute(c.Request().Context(), domain.ReleaseResolveInput{
		ModuleID:    c.QueryParam("moduleId"),
		Environment: c.QueryParam("environment"),
		Identifier:  c.QueryParam("identifier"),
	})
	if err != nil {
		return releaseErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, releaseResolveResponse{Route: newReleaseRouteResponse(route)})
}

func releaseErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidReleaseInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid release request"})
	case errors.Is(err, domain.ErrBuildRecordNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "build record not found"})
	case errors.Is(err, domain.ErrModuleNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "module not found"})
	case errors.Is(err, domain.ErrEnvironmentNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "environment not found"})
	case errors.Is(err, domain.ErrReleaseNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "release not found"})
	case errors.Is(err, domain.ErrReleaseRouteNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "release route not found"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func newReleaseRecordResponses(records []domain.ReleaseRecord) []releaseRecordResponse {
	responses := make([]releaseRecordResponse, 0, len(records))
	for _, record := range records {
		responses = append(responses, newReleaseRecordResponse(record))
	}
	return responses
}

func newReleaseRecordResponse(record domain.ReleaseRecord) releaseRecordResponse {
	return releaseRecordResponse{
		ApplicationName: record.ApplicationName,
		ArtifactKey:     record.ArtifactKey,
		ArtifactURL:     record.ArtifactURL,
		BuildRecordID:   record.BuildRecordID,
		CreatedAt:       formatTime(record.CreatedAt),
		CreatedBy:       record.CreatedBy,
		CreatedByUserID: record.CreatedByUserID,
		Description:     record.Description,
		Environment:     record.Environment,
		EnvironmentName: record.EnvironmentName,
		ID:              record.ID,
		Identifier:      record.Identifier,
		ModuleID:        record.ModuleID,
		ModuleName:      record.ModuleName,
		ReleaseType:     string(record.ReleaseType),
		Status:          string(record.Status),
		UpdatedAt:       formatTime(record.UpdatedAt),
	}
}

func newReleaseRouteResponse(route domain.ReleaseRoute) releaseRouteResponse {
	return releaseRouteResponse{
		ApplicationName: route.ApplicationName,
		ArtifactKey:     route.ArtifactKey,
		ArtifactURL:     route.ArtifactURL,
		BuildRecordID:   route.BuildRecordID,
		CreatedAt:       formatTime(route.CreatedAt),
		CreatedBy:       route.CreatedBy,
		CreatedByUserID: route.CreatedByUserID,
		Environment:     route.Environment,
		EnvironmentName: route.EnvironmentName,
		ID:              route.ID,
		Identifier:      route.Identifier,
		ModuleID:        route.ModuleID,
		ModuleName:      route.ModuleName,
		ReleaseID:       route.ReleaseID,
		RouteType:       string(route.RouteType),
		Status:          string(route.Status),
		UpdatedAt:       formatTime(route.UpdatedAt),
	}
}
