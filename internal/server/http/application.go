package httpserver

import (
	"errors"
	"net/http"
	"strings"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type applicationHandler struct {
	service ApplicationService
}

type applicationsResponse struct {
	Applications []applicationResponse      `json:"applications"`
	Summary      applicationSummaryResponse `json:"summary"`
}

type applicationDetailResponse struct {
	Application applicationResponse     `json:"application"`
	Detail      applicationDetailFields `json:"detail"`
}

type applicationResponse struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Slug               string   `json:"slug"`
	Platform           string   `json:"platform"`
	Owner              string   `json:"owner"`
	OwnerEmail         string   `json:"ownerEmail"`
	OwnerAvatarURL     string   `json:"ownerAvatarUrl"`
	OwnerDepartmentIDs []string `json:"ownerDepartmentIds"`
	ModuleCount        int      `json:"moduleCount"`
	EnvironmentCount   int      `json:"environmentCount"`
	CreatedAt          string   `json:"createdAt"`
	Status             string   `json:"status"`
}

type applicationSummaryResponse struct {
	Total       int `json:"total"`
	Running     int `json:"running"`
	Maintenance int `json:"maintenance"`
	Offline     int `json:"offline"`
}

type applicationDetailFields struct {
	ID                 string                           `json:"id"`
	AppKey             string                           `json:"appKey"`
	Slug               string                           `json:"slug"`
	Name               string                           `json:"name"`
	Description        string                           `json:"description"`
	Owner              string                           `json:"owner"`
	OwnerEmail         string                           `json:"ownerEmail"`
	OwnerAvatarURL     string                           `json:"ownerAvatarUrl"`
	OwnerDepartmentIDs []string                         `json:"ownerDepartmentIds"`
	Metrics            []applicationMetricResponse      `json:"metrics"`
	Environments       []applicationEnvironmentResponse `json:"environments"`
	Modules            []applicationModuleResponse      `json:"modules"`
	Members            []applicationMemberResponse      `json:"members"`
	Pipelines          []applicationPipelineResponse    `json:"pipelines"`
}

type applicationMetricResponse struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Value  string `json:"value"`
	Detail string `json:"detail"`
	Tone   string `json:"tone"`
}

type applicationEnvironmentResponse struct {
	ID          string `json:"id"`
	Environment string `json:"environment"`
	Traffic     string `json:"traffic"`
	UpdatedAt   string `json:"updatedAt"`
	Status      string `json:"status"`
}

type applicationModuleResponse struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Repository    string `json:"repository"`
	Owner         string `json:"owner"`
	LatestVersion string `json:"latestVersion"`
	Pipeline      string `json:"pipeline"`
	Status        string `json:"status"`
}

type applicationMemberResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
	Team string `json:"team"`
}

type applicationPipelineResponse struct {
	ID         string `json:"id"`
	RunID      string `json:"runId"`
	ModuleName string `json:"moduleName"`
	Branch     string `json:"branch"`
	Status     string `json:"status"`
	Duration   string `json:"duration"`
	Operator   string `json:"operator"`
	StartedAt  string `json:"startedAt"`
}

type applicationRequest struct {
	Slug               string                          `json:"slug"`
	Name               string                          `json:"name"`
	Platform           string                          `json:"platform"`
	Owner              string                          `json:"owner"`
	OwnerEmail         string                          `json:"ownerEmail"`
	OwnerAvatarURL     string                          `json:"ownerAvatarUrl"`
	OwnerDepartmentIDs []string                        `json:"ownerDepartmentIds"`
	Description        string                          `json:"description"`
	Status             domain.ApplicationStatus        `json:"status"`
	Environments       []applicationEnvironmentRequest `json:"environments"`
	Members            []applicationMemberRequest      `json:"members"`
}

type applicationEnvironmentRequest struct {
	Environment string `json:"environment"`
}

type applicationMemberRequest struct {
	Name string                       `json:"name"`
	Role domain.ApplicationMemberRole `json:"role"`
	Team string                       `json:"team"`
}

type applicationOptionsResponse struct {
	ApplicationStatuses []applicationOptionResponse `json:"applicationStatuses"`
	Environments        []applicationOptionResponse `json:"environments"`
	Owners              []applicationOptionResponse `json:"owners"`
	Platforms           []applicationOptionResponse `json:"platforms"`
}

type applicationOptionResponse struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

func registerApplicationRoutes(group *echo.Group, service ApplicationService) {
	handler := applicationHandler{service: service}

	group.GET("/applications", handler.list)
	group.GET("/applications/options", handler.options)
	group.GET("/applications/:slug", handler.detail)
	group.POST("/applications", handler.create)
	group.PATCH("/applications/:slug", handler.update)
	group.DELETE("/applications/:slug", handler.delete)
}

func (h applicationHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "application service unavailable"})
	}

	filters := domain.ApplicationListFilters{
		Platform: strings.TrimSpace(c.QueryParam("platform")),
		Status:   domain.ApplicationStatus(strings.TrimSpace(c.QueryParam("status"))),
		Owner:    strings.TrimSpace(c.QueryParam("owner")),
		Keyword:  strings.TrimSpace(c.QueryParam("keyword")),
	}

	applications, summary, err := h.service.ListApplications(c.Request().Context(), filters)
	if err != nil {
		return applicationErrorResponse(c, err)
	}

	response := applicationsResponse{
		Applications: newApplicationResponses(applications),
		Summary:      newApplicationSummaryResponse(summary),
	}

	return c.JSON(http.StatusOK, response)
}

func (h applicationHandler) options(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "application service unavailable"})
	}

	options, err := h.service.GetApplicationOptions(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	return c.JSON(http.StatusOK, newApplicationOptionsResponse(options))
}

func (h applicationHandler) detail(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "application service unavailable"})
	}

	detail, err := h.service.GetApplicationDetail(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return applicationErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, newApplicationDetailResponse(detail))
}

func (h applicationHandler) create(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "application service unavailable"})
	}

	input, ok := bindApplicationRequest(c)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid application request"})
	}

	detail, err := h.service.CreateApplication(c.Request().Context(), input)
	if err != nil {
		return applicationErrorResponse(c, err)
	}

	return c.JSON(http.StatusCreated, newApplicationDetailResponse(detail))
}

func (h applicationHandler) update(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "application service unavailable"})
	}

	input, ok := bindApplicationRequest(c)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid application request"})
	}

	detail, err := h.service.UpdateApplication(c.Request().Context(), c.Param("slug"), input)
	if err != nil {
		return applicationErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, newApplicationDetailResponse(detail))
}

func (h applicationHandler) delete(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "application service unavailable"})
	}

	detail, err := h.service.DeleteApplication(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return applicationErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, newApplicationDetailResponse(detail))
}

func bindApplicationRequest(c echo.Context) (domain.ApplicationInput, bool) {
	var request applicationRequest
	if err := c.Bind(&request); err != nil {
		return domain.ApplicationInput{}, false
	}

	environments := make([]domain.ApplicationEnvironmentInput, 0, len(request.Environments))
	for _, environment := range request.Environments {
		environments = append(environments, domain.ApplicationEnvironmentInput{
			Environment: environment.Environment,
		})
	}

	members := make([]domain.ApplicationMemberInput, 0, len(request.Members))
	for _, member := range request.Members {
		members = append(members, domain.ApplicationMemberInput{
			Name: member.Name,
			Role: member.Role,
			Team: member.Team,
		})
	}

	return domain.ApplicationInput{
		Slug:               request.Slug,
		Name:               request.Name,
		Platform:           request.Platform,
		Owner:              request.Owner,
		OwnerEmail:         request.OwnerEmail,
		OwnerAvatarURL:     request.OwnerAvatarURL,
		OwnerDepartmentIDs: request.OwnerDepartmentIDs,
		Description:        request.Description,
		Status:             request.Status,
		Environments:       environments,
		Members:            members,
	}, true
}

func applicationErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrApplicationNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "application not found"})
	case errors.Is(err, domain.ErrApplicationSlugConflict):
		return c.JSON(http.StatusConflict, map[string]string{"error": "application slug already exists"})
	case errors.Is(err, domain.ErrInvalidApplicationInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid application request"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func newApplicationResponses(applications []domain.Application) []applicationResponse {
	response := make([]applicationResponse, 0, len(applications))
	for _, application := range applications {
		response = append(response, newApplicationResponse(application))
	}
	return response
}

func newApplicationResponse(application domain.Application) applicationResponse {
	return applicationResponse{
		ID:                 application.ID,
		Name:               application.Name,
		Slug:               application.Slug,
		Platform:           application.Platform,
		Owner:              application.Owner,
		OwnerEmail:         application.OwnerEmail,
		OwnerAvatarURL:     application.OwnerAvatarURL,
		OwnerDepartmentIDs: application.OwnerDepartmentIDs,
		ModuleCount:        application.ModuleCount,
		EnvironmentCount:   application.EnvironmentCount,
		CreatedAt:          application.CreatedAt.Format("2006-01-02"),
		Status:             application.Status.Label(),
	}
}

func newApplicationSummaryResponse(summary domain.ApplicationSummary) applicationSummaryResponse {
	return applicationSummaryResponse{
		Total:       summary.Total,
		Running:     summary.Running,
		Maintenance: summary.Maintenance,
		Offline:     summary.Offline,
	}
}

func newApplicationDetailResponse(detail domain.ApplicationDetail) applicationDetailResponse {
	return applicationDetailResponse{
		Application: newApplicationResponse(detail.Application),
		Detail: applicationDetailFields{
			ID:                 detail.ID,
			AppKey:             detail.AppKey,
			Slug:               detail.Slug,
			Name:               detail.Name,
			Description:        detail.Description,
			Owner:              detail.Owner,
			OwnerEmail:         detail.OwnerEmail,
			OwnerAvatarURL:     detail.OwnerAvatarURL,
			OwnerDepartmentIDs: detail.OwnerDepartmentIDs,
			Metrics:            newApplicationMetricResponses(detail.Metrics),
			Environments:       newApplicationEnvironmentResponses(detail.Environments),
			Modules:            newApplicationModuleResponses(detail.Modules),
			Members:            newApplicationMemberResponses(detail.Members),
			Pipelines:          newApplicationPipelineResponses(detail.Pipelines),
		},
	}
}

func newApplicationOptionsResponse(options domain.ApplicationOptions) applicationOptionsResponse {
	return applicationOptionsResponse{
		ApplicationStatuses: newApplicationOptionResponses(options.ApplicationStatuses),
		Environments:        newApplicationOptionResponses(options.Environments),
		Owners:              newApplicationOptionResponses(options.Owners),
		Platforms:           newApplicationOptionResponses(options.Platforms),
	}
}

func newApplicationOptionResponses(options []domain.ApplicationOptionItem) []applicationOptionResponse {
	response := make([]applicationOptionResponse, 0, len(options))
	for _, option := range options {
		response = append(response, applicationOptionResponse{
			Label: option.Label,
			Value: option.Value,
		})
	}
	return response
}

func newApplicationMetricResponses(metrics []domain.ApplicationDetailMetric) []applicationMetricResponse {
	response := make([]applicationMetricResponse, 0, len(metrics))
	for _, metric := range metrics {
		response = append(response, applicationMetricResponse{
			ID:     metric.ID,
			Label:  metric.Label,
			Value:  metric.Value,
			Detail: metric.Detail,
			Tone:   metric.Tone,
		})
	}
	return response
}

func newApplicationEnvironmentResponses(environments []domain.ApplicationEnvironmentBinding) []applicationEnvironmentResponse {
	response := make([]applicationEnvironmentResponse, 0, len(environments))
	for _, environment := range environments {
		response = append(response, applicationEnvironmentResponse{
			ID:          environment.ID,
			Environment: environment.Environment,
			Traffic:     environment.Traffic,
			UpdatedAt:   formatTime(environment.UpdatedAt),
			Status:      environment.Status.Label(),
		})
	}
	return response
}

func newApplicationModuleResponses(modules []domain.ApplicationModule) []applicationModuleResponse {
	response := make([]applicationModuleResponse, 0, len(modules))
	for _, module := range modules {
		response = append(response, applicationModuleResponse{
			ID:            module.ID,
			Name:          module.Name,
			Kind:          module.Kind,
			Repository:    module.Repository,
			Owner:         module.Owner,
			LatestVersion: module.LatestVersion,
			Pipeline:      module.Pipeline,
			Status:        module.Status,
		})
	}
	return response
}

func newApplicationMemberResponses(members []domain.ApplicationMember) []applicationMemberResponse {
	response := make([]applicationMemberResponse, 0, len(members))
	for _, member := range members {
		response = append(response, applicationMemberResponse{
			ID:   member.ID,
			Name: member.Name,
			Role: string(member.Role),
			Team: member.Team,
		})
	}
	return response
}

func newApplicationPipelineResponses(pipelines []domain.ApplicationPipeline) []applicationPipelineResponse {
	response := make([]applicationPipelineResponse, 0, len(pipelines))
	for _, pipeline := range pipelines {
		response = append(response, applicationPipelineResponse{
			ID:         pipeline.ID,
			RunID:      pipeline.RunID,
			ModuleName: pipeline.ModuleName,
			Branch:     pipeline.Branch,
			Status:     pipeline.Status,
			Duration:   pipeline.Duration,
			Operator:   pipeline.Operator,
			StartedAt:  pipeline.StartedAt,
		})
	}
	return response
}
