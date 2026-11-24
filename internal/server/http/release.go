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
	Channel        string `json:"channel"`
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
	Channel         string `json:"channel"`
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
	Channel         string `json:"channel"`
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
	group.POST("/releases/:id/deactivate", handler.deactivate)
	group.POST("/releases/:id/rollback", handler.rollback)
	group.GET("/releases/:id/events", handler.events)
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

	var channel *string
	if c.QueryParams().Has("channel") {
		value := c.QueryParam("channel")
		channel = &value
	}
	records, summary, err := h.service.ListReleases(c.Request().Context(), domain.ReleaseRecordFilters{
		ApplicationID: c.QueryParam("applicationId"),
		Channel:       channel,
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
		Channel:         request.Channel,
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

type releaseDeactivateRequest struct {
	Reason string `json:"reason"`
}

type releaseRollbackRequest struct {
	Description string `json:"description"`
}

type releaseEventsResponse struct {
	Events []releaseEventResponse `json:"events"`
}

type releaseEventResponse struct {
	ID        int64          `json:"id"`
	Kind      string         `json:"kind"`
	Detail    map[string]any `json:"detail"`
	Actor     *userRefJSON   `json:"actor"`
	CreatedAt string         `json:"createdAt"`
}

type userRefJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// releaseActor 校验当前用户对发布有回滚权限（停用和回滚共用 release:rollback），返回操作人。
func (h releaseHandler) releaseActor(c echo.Context) (string, error) {
	if err := authorizeCurrentUser(c, h.authService, domain.ActionReleaseRollback, domain.AuthorizationResource{ReleaseID: c.Param("id")}); err != nil {
		return "", err
	}
	session, ok := consoleSession(c)
	if !ok {
		return "", c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	actor := domain.ConsoleUserIDFromFeishu(session.User)
	if actor == "" {
		return "", c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	return actor, nil
}

func (h releaseHandler) deactivate(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release service unavailable"})
	}
	var request releaseDeactivateRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid release request"})
	}
	actor, err := h.releaseActor(c)
	if err != nil {
		return err
	}
	record, route, err := h.service.DeactivateRelease(c.Request().Context(), domain.ReleaseDeactivateInput{ID: c.Param("id"), Reason: request.Reason, ActorUserID: actor})
	if err != nil {
		return releaseErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, releaseDetailResponse{Release: newReleaseRecordResponse(record), Route: newReleaseRouteResponse(route)})
}

func (h releaseHandler) rollback(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release service unavailable"})
	}
	var request releaseRollbackRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid release request"})
	}
	actor, err := h.releaseActor(c)
	if err != nil {
		return err
	}
	record, route, err := h.service.RollbackRelease(c.Request().Context(), domain.ReleaseRollbackInput{ID: c.Param("id"), Description: request.Description, ActorUserID: actor})
	if err != nil {
		return releaseErrorResponse(c, err)
	}
	return c.JSON(http.StatusCreated, releaseDetailResponse{Release: newReleaseRecordResponse(record), Route: newReleaseRouteResponse(route)})
}

func (h releaseHandler) events(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release service unavailable"})
	}
	events, err := h.service.ListReleaseEvents(c.Request().Context(), c.Param("id"))
	if err != nil {
		return releaseErrorResponse(c, err)
	}
	response := releaseEventsResponse{Events: make([]releaseEventResponse, 0, len(events))}
	for _, event := range events {
		item := releaseEventResponse{ID: event.ID, Kind: string(event.Kind), Detail: event.Detail, CreatedAt: formatTime(event.CreatedAt)}
		if item.Detail == nil {
			item.Detail = map[string]any{}
		}
		if event.ActorUserID != "" {
			item.Actor = &userRefJSON{ID: event.ActorUserID, Name: event.ActorName}
		}
		response.Events = append(response.Events, item)
	}
	return c.JSON(http.StatusOK, response)
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
		Channel:        c.QueryParam("channel"),
	})
	if err != nil {
		return releaseErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, releaseResolveResponse{Route: newReleaseRouteResponse(route)})
}

type releaseCapabilityConflictErrorResponse struct {
	Error     string                              `json:"error"`
	Conflicts []releaseCapabilityConflictResponse `json:"conflicts"`
}

type releaseCapabilityConflictResponse struct {
	ModuleID      string                        `json:"moduleId"`
	ModuleName    string                        `json:"moduleName"`
	ReleaseID     string                        `json:"releaseId"`
	ReleaseType   string                        `json:"releaseType"`
	BuildRecordID string                        `json:"buildRecordId"`
	BuildNumber   int64                         `json:"buildNumber"`
	Reason        string                        `json:"reason"`
	Shortfalls    []capabilityShortfallResponse `json:"shortfalls"`
}

type capabilityShortfallResponse struct {
	Name     string `json:"name"`
	Required int64  `json:"required"`
	Provided *int64 `json:"provided"`
}

func releaseErrorResponse(c echo.Context, err error) error {
	var capabilityConflict *domain.ReleaseCapabilityConflictError
	if errors.As(err, &capabilityConflict) {
		conflicts := make([]releaseCapabilityConflictResponse, 0, len(capabilityConflict.Conflicts))
		for _, conflict := range capabilityConflict.Conflicts {
			shortfalls := make([]capabilityShortfallResponse, 0, len(conflict.Shortfalls))
			for _, shortfall := range conflict.Shortfalls {
				shortfalls = append(shortfalls, capabilityShortfallResponse{Name: shortfall.Name, Required: shortfall.Required, Provided: shortfall.Provided})
			}
			conflicts = append(conflicts, releaseCapabilityConflictResponse{
				ModuleID:      conflict.ModuleID,
				ModuleName:    conflict.ModuleName,
				ReleaseID:     conflict.ReleaseID,
				ReleaseType:   string(conflict.ReleaseType),
				BuildRecordID: conflict.BuildRecordID,
				BuildNumber:   conflict.BuildNumber,
				Reason:        conflict.Reason,
				Shortfalls:    shortfalls,
			})
		}
		return c.JSON(http.StatusConflict, releaseCapabilityConflictErrorResponse{Error: "release capability conflict", Conflicts: conflicts})
	}
	switch {
	case errors.Is(err, domain.ErrReleaseAlreadyActive):
		return c.JSON(http.StatusConflict, map[string]string{"error": "release already active"})
	case errors.Is(err, domain.ErrReleaseNotActive):
		return c.JSON(http.StatusConflict, map[string]string{"error": "release not active"})
	case errors.Is(err, domain.ErrReleaseRollbackUnavailable):
		return c.JSON(http.StatusConflict, map[string]string{"error": "release rollback unavailable"})
	case errors.Is(err, domain.ErrInvalidReleaseInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid release request"})
	case errors.Is(err, domain.ErrBuildRecordNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "build record not found"})
	case errors.Is(err, domain.ErrModuleNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "module not found"})
	case errors.Is(err, domain.ErrEnvironmentNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "environment not found"})
	case errors.Is(err, domain.ErrReleaseChannelNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "release channel not found"})
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
		Channel:         record.Channel,
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
		Channel:         route.Channel,
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
