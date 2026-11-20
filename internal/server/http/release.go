package httpserver

import (
	"errors"
	"net/http"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type releaseHandler struct {
	authService AuthorizationService
	service     ReleaseService
}

type releaseCreateRequest struct {
	BuildRecordID  string `json:"buildRecordId"`
	ReleaseType    string `json:"releaseType"`
	GrayStrategy   string `json:"grayStrategy"`
	GrayPercentage int    `json:"grayPercentage"`
	Description    string `json:"description"`
}

type releaseGrayPercentageUpdateRequest struct {
	GrayPercentage int `json:"grayPercentage"`
}

type releaseDetailResponse struct {
	Release releaseRecordResponse `json:"release"`
	Route   releaseRouteResponse  `json:"route"`
}

type releasesResponse struct {
	Pagination paginationResponse      `json:"pagination"`
	Releases   []releaseRecordResponse `json:"releases"`
	Summary    releaseSummaryResponse  `json:"summary"`
}

type releaseSummaryResponse struct {
	Active   int `json:"active"`
	Inactive int `json:"inactive"`
	Total    int `json:"total"`
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
	ModuleID        string `json:"moduleId"`
	ModuleName      string `json:"moduleName"`
	ReleaseType     string `json:"releaseType"`
	RuntimeVersion  string `json:"runtimeVersion"`
	GrayStrategy    string `json:"grayStrategy"`
	GrayPercentage  int    `json:"grayPercentage"`
	ExpiresAt       string `json:"expiresAt"`
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
	ModuleID        string `json:"moduleId"`
	ModuleName      string `json:"moduleName"`
	ReleaseID       string `json:"releaseId"`
	RouteType       string `json:"routeType"`
	RuntimeVersion  string `json:"runtimeVersion"`
	GrayStrategy    string `json:"grayStrategy"`
	GrayPercentage  int    `json:"grayPercentage"`
	Status          string `json:"status"`
	UpdatedAt       string `json:"updatedAt"`
}

func registerReleaseRoutes(group *echo.Group, service ReleaseService, authService AuthorizationService) {
	handler := releaseHandler{authService: authService, service: service}

	group.GET("/releases", handler.list)
	group.POST("/releases", handler.create)
	group.PATCH("/releases/:id/gray-percentage", handler.updateGrayPercentage)
	group.GET("/releases/resolve", handler.resolve)
}

func (h releaseHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release service unavailable"})
	}

	pagination, ok := requestPagination(c)
	if !ok {
		return releaseErrorResponse(c, domain.ErrInvalidReleaseInput)
	}

	records, summary, err := h.service.ListReleases(c.Request().Context(), domain.ReleaseRecordFilters{
		ApplicationID: c.QueryParam("applicationId"),
		Environment:   c.QueryParam("environment"),
		ModuleID:      c.QueryParam("moduleId"),
		Pagination:    pagination,
		ReleaseType:   domain.ReleaseType(c.QueryParam("releaseType")),
		Status:        domain.ReleaseStatus(c.QueryParam("status")),
	})
	if err != nil {
		return releaseErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, releasesResponse{
		Pagination: newPaginationResponse(pagination, summary.Total),
		Releases:   newReleaseRecordResponses(records),
		Summary: releaseSummaryResponse{
			Active: summary.Active, Inactive: summary.Inactive, Total: summary.Total,
		},
	})
}

func (h releaseHandler) create(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release service unavailable"})
	}

	var request releaseCreateRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid release request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionReleaseCreate, domain.AuthorizationResource{BuildRecordID: request.BuildRecordID}); err != nil {
		return err
	}

	session, ok := consoleSession(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	createdByUserID := domain.ConsoleUserIDFromFeishu(session.User)
	if createdByUserID == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	record, route, err := h.service.CreateRelease(c.Request().Context(), domain.ReleaseCreateInput{
		BuildRecordID:   request.BuildRecordID,
		ReleaseType:     domain.ReleaseType(request.ReleaseType),
		GrayStrategy:    domain.ReleaseGrayStrategy(request.GrayStrategy),
		GrayPercentage:  request.GrayPercentage,
		Description:     request.Description,
		CreatedByUserID: createdByUserID,
	})
	if err != nil {
		return releaseErrorResponse(c, err)
	}

	return c.JSON(http.StatusCreated, releaseDetailResponse{
		Release: newReleaseRecordResponse(record),
		Route:   newReleaseRouteResponse(route),
	})
}

func (h releaseHandler) updateGrayPercentage(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release service unavailable"})
	}

	var request releaseGrayPercentageUpdateRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid release request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionReleaseCreate, domain.AuthorizationResource{ReleaseID: c.Param("id")}); err != nil {
		return err
	}

	session, ok := consoleSession(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	updatedByUserID := domain.ConsoleUserIDFromFeishu(session.User)
	if updatedByUserID == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	record, route, err := h.service.UpdateReleaseGrayPercentage(c.Request().Context(), domain.ReleaseGrayPercentageUpdateInput{
		ID:              c.Param("id"),
		GrayPercentage:  request.GrayPercentage,
		UpdatedByUserID: updatedByUserID,
	})
	if err != nil {
		return releaseErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, releaseDetailResponse{
		Release: newReleaseRecordResponse(record),
		Route:   newReleaseRouteResponse(route),
	})
}

func (h releaseHandler) resolve(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release service unavailable"})
	}

	route, err := h.service.ResolveReleaseRoute(c.Request().Context(), domain.ReleaseResolveInput{
		ModuleID:       c.QueryParam("moduleId"),
		Environment:    c.QueryParam("environment"),
		RuntimeVersion: c.QueryParam("runtimeVersion"),
		UserID:         c.QueryParam("userId"),
	})
	if err != nil {
		return releaseErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, releaseResolveResponse{Route: newReleaseRouteResponse(route)})
}

func releaseErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrReleaseAlreadyActive):
		return c.JSON(http.StatusConflict, map[string]string{"error": "release already active"})
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
	case errors.Is(err, domain.ErrForbidden):
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
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
		ModuleID:        record.ModuleID,
		ModuleName:      record.ModuleName,
		ReleaseType:     string(record.ReleaseType),
		RuntimeVersion:  record.RuntimeVersion,
		GrayStrategy:    string(record.GrayStrategy),
		GrayPercentage:  record.GrayPercentage,
		ExpiresAt:       formatOptionalTime(record.ExpiresAt),
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
		ModuleID:        route.ModuleID,
		ModuleName:      route.ModuleName,
		ReleaseID:       route.ReleaseID,
		RouteType:       string(route.RouteType),
		RuntimeVersion:  route.RuntimeVersion,
		GrayStrategy:    string(route.GrayStrategy),
		GrayPercentage:  route.GrayPercentage,
		Status:          string(route.Status),
		UpdatedAt:       formatTime(route.UpdatedAt),
	}
}
