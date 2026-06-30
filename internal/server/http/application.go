package httpserver

import (
	"errors"
	"net/http"
	"strings"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type applicationHandler struct {
	authService AuthorizationService
	service     ApplicationService
}

type applicationsResponse struct {
	Applications []applicationResponse      `json:"applications"`
	Summary      applicationSummaryResponse `json:"summary"`
}

type applicationDetailResponse struct {
	Application applicationResponse     `json:"application"`
	Detail      applicationDetailFields `json:"detail"`
}

type applicationDeleteCheckResponse struct {
	Deletable    bool                                  `json:"deletable"`
	Dependencies []applicationDeleteDependencyResponse `json:"dependencies"`
	Message      string                                `json:"message"`
}

type applicationDeleteDependencyResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type applicationResponse struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Slug               string   `json:"slug"`
	Platform           string   `json:"platform"`
	OwnerUserID        string   `json:"ownerUserId"`
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
	ID                 string                               `json:"id"`
	AppKey             string                               `json:"appKey"`
	Slug               string                               `json:"slug"`
	Name               string                               `json:"name"`
	Description        string                               `json:"description"`
	OwnerUserID        string                               `json:"ownerUserId"`
	Owner              string                               `json:"owner"`
	OwnerEmail         string                               `json:"ownerEmail"`
	OwnerAvatarURL     string                               `json:"ownerAvatarUrl"`
	OwnerDepartmentIDs []string                             `json:"ownerDepartmentIds"`
	Metrics            []applicationMetricResponse          `json:"metrics"`
	Environments       []applicationEnvironmentResponse     `json:"environments"`
	Modules            []applicationModuleResponse          `json:"modules"`
	Members            []applicationMemberResponse          `json:"members"`
	Capabilities       domain.ApplicationCapabilities       `json:"capabilities"`
	ClientIntegration  applicationClientIntegrationResponse `json:"clientIntegration"`
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
	CommonModuleID   string `json:"commonModuleId"`
	CommonModuleName string `json:"commonModuleName"`
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	Name             string `json:"name"`
	Owner            string `json:"owner"`
	Repository       string `json:"repository"`
}

type applicationMemberResponse struct {
	AvatarURL string `json:"avatarUrl"`
	Email     string `json:"email"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Team      string `json:"team"`
	UserID    string `json:"userId"`
}

type applicationMembersResponse struct {
	Members []applicationMemberResponse `json:"members"`
}

type applicationClientIntegrationResponse struct {
	Endpoint applicationClientIntegrationEndpoint `json:"endpoint"`
}

type applicationClientIntegrationEndpoint struct {
	BaseURL        string                              `json:"baseUrl"`
	Path           string                              `json:"path"`
	Method         string                              `json:"method"`
	Description    string                              `json:"description"`
	ExampleURL     string                              `json:"exampleUrl"`
	RequestFields  []applicationClientIntegrationField `json:"requestFields"`
	ResponseFields []applicationClientIntegrationField `json:"responseFields"`
}

type applicationClientIntegrationField struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Type        string `json:"type"`
	Example     string `json:"example"`
}

type applicationRequest struct {
	Slug               string                          `json:"slug"`
	Name               string                          `json:"name"`
	Platform           string                          `json:"platform"`
	OwnerUserID        string                          `json:"ownerUserId"`
	Owner              string                          `json:"owner"`
	OwnerEmail         string                          `json:"ownerEmail"`
	OwnerAvatarURL     string                          `json:"ownerAvatarUrl"`
	OwnerDepartmentIDs []string                        `json:"ownerDepartmentIds"`
	OwnerFeishuUnionID string                          `json:"ownerFeishuUnionId"`
	OwnerFeishuOpenID  string                          `json:"ownerFeishuOpenId"`
	OwnerFeishuUserID  string                          `json:"ownerFeishuUserId"`
	Description        string                          `json:"description"`
	Status             domain.ApplicationStatus        `json:"status"`
	Environments       []applicationEnvironmentRequest `json:"environments"`
	Members            []applicationMemberRequest      `json:"members"`
}

type applicationStatusRequest struct {
	Status domain.ApplicationStatus `json:"status"`
}

type applicationEnvironmentRequest struct {
	Environment string `json:"environment"`
}

type applicationMemberRequest struct {
	Role   domain.ApplicationMemberRole `json:"role"`
	UserID string                       `json:"userId"`
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

func registerApplicationRoutes(group *echo.Group, service ApplicationService, authService AuthorizationService) {
	handler := applicationHandler{authService: authService, service: service}

	group.GET("/applications", handler.list)
	group.GET("/applications/options", handler.options)
	group.GET("/applications/:slug/delete-check", handler.deleteCheck)
	group.GET("/applications/:slug/members", handler.members)
	group.GET("/applications/:slug", handler.detail)
	group.POST("/applications", handler.create)
	group.POST("/applications/:slug/members", handler.upsertMember)
	group.PATCH("/applications/:slug/members/:userId", handler.upsertMember)
	group.DELETE("/applications/:slug/members/:userId", handler.deleteMember)
	group.PATCH("/applications/:slug", handler.update)
	group.PATCH("/applications/:slug/status", handler.updateStatus)
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
	detail, err = h.detailWithCapabilities(c, detail)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, newApplicationDetailResponse(c, detail))
}

func (h applicationHandler) create(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "application service unavailable"})
	}

	input, ok := bindApplicationRequest(c)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid application request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionApplicationCreate, domain.AuthorizationResource{}); err != nil {
		return err
	}

	detail, err := h.service.CreateApplication(c.Request().Context(), input)
	if err != nil {
		return applicationErrorResponse(c, err)
	}
	detail, err = h.detailWithCapabilities(c, detail)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, newApplicationDetailResponse(c, detail))
}

func (h applicationHandler) update(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "application service unavailable"})
	}

	input, ok := bindApplicationRequest(c)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid application request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionApplicationUpdate, domain.AuthorizationResource{ApplicationSlug: c.Param("slug")}); err != nil {
		return err
	}

	detail, err := h.service.UpdateApplication(c.Request().Context(), c.Param("slug"), input)
	if err != nil {
		return applicationErrorResponse(c, err)
	}
	detail, err = h.detailWithCapabilities(c, detail)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, newApplicationDetailResponse(c, detail))
}

func (h applicationHandler) updateStatus(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "application service unavailable"})
	}

	var request applicationStatusRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid application request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionApplicationStatus, domain.AuthorizationResource{ApplicationSlug: c.Param("slug")}); err != nil {
		return err
	}

	detail, err := h.service.UpdateApplicationStatus(c.Request().Context(), c.Param("slug"), request.Status)
	if err != nil {
		return applicationErrorResponse(c, err)
	}
	detail, err = h.detailWithCapabilities(c, detail)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, newApplicationDetailResponse(c, detail))
}

func (h applicationHandler) deleteCheck(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "application service unavailable"})
	}

	check, err := h.service.CheckApplicationDelete(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return applicationErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, newApplicationDeleteCheckResponse(check))
}

func (h applicationHandler) delete(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "application service unavailable"})
	}

	if err := authorizeCurrentUser(c, h.authService, domain.ActionApplicationDelete, domain.AuthorizationResource{ApplicationSlug: c.Param("slug")}); err != nil {
		return err
	}
	if err := h.service.DeleteApplication(c.Request().Context(), c.Param("slug")); err != nil {
		return applicationErrorResponse(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

func (h applicationHandler) members(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "application service unavailable"})
	}

	members, err := h.service.ListApplicationMembers(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return applicationErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, applicationMembersResponse{Members: newApplicationMemberResponses(members)})
}

func (h applicationHandler) upsertMember(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "application service unavailable"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionApplicationMemberManage, domain.AuthorizationResource{ApplicationSlug: c.Param("slug")}); err != nil {
		return err
	}

	var request applicationMemberRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid application request"})
	}
	userID := strings.TrimSpace(c.Param("userId"))
	if userID == "" {
		userID = request.UserID
	}
	member, err := h.service.UpsertApplicationMember(c.Request().Context(), c.Param("slug"), domain.ApplicationMemberInput{
		Role:   request.Role,
		UserID: userID,
	})
	if err != nil {
		return applicationErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, applicationMemberResponseFromDomain(member))
}

func (h applicationHandler) deleteMember(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "application service unavailable"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionApplicationMemberManage, domain.AuthorizationResource{ApplicationSlug: c.Param("slug")}); err != nil {
		return err
	}
	if err := h.service.DeleteApplicationMember(c.Request().Context(), c.Param("slug"), c.Param("userId")); err != nil {
		return applicationErrorResponse(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h applicationHandler) detailWithCapabilities(c echo.Context, detail domain.ApplicationDetail) (domain.ApplicationDetail, error) {
	if h.authService == nil {
		return detail, nil
	}
	session, ok := consoleSession(c)
	if !ok {
		return detail, nil
	}
	userID := domain.ConsoleUserIDFromFeishu(session.User)
	if userID == "" {
		return detail, nil
	}
	capabilities, err := h.authService.ApplicationCapabilities(c.Request().Context(), userID, detail.ID)
	if err != nil {
		return domain.ApplicationDetail{}, c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	detail.Capabilities = capabilities
	return detail, nil
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
			Role:   member.Role,
			UserID: member.UserID,
		})
	}

	return domain.ApplicationInput{
		Slug:               request.Slug,
		Name:               request.Name,
		Platform:           request.Platform,
		OwnerUserID:        request.OwnerUserID,
		Owner:              request.Owner,
		OwnerEmail:         request.OwnerEmail,
		OwnerAvatarURL:     request.OwnerAvatarURL,
		OwnerDepartmentIDs: request.OwnerDepartmentIDs,
		OwnerFeishuUnionID: request.OwnerFeishuUnionID,
		OwnerFeishuOpenID:  request.OwnerFeishuOpenID,
		OwnerFeishuUserID:  request.OwnerFeishuUserID,
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
	case errors.Is(err, domain.ErrApplicationInUse):
		return c.JSON(http.StatusConflict, map[string]string{"error": "application is referenced by modules"})
	case errors.Is(err, domain.ErrApplicationLastOwner):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "application last owner"})
	case errors.Is(err, domain.ErrInvalidApplicationInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid application request"})
	case errors.Is(err, domain.ErrForbidden):
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
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
		OwnerUserID:        application.OwnerUserID,
		Owner:              application.Owner,
		OwnerEmail:         application.OwnerEmail,
		OwnerAvatarURL:     application.OwnerAvatarURL,
		OwnerDepartmentIDs: application.OwnerDepartmentIDs,
		ModuleCount:        application.ModuleCount,
		EnvironmentCount:   application.EnvironmentCount,
		CreatedAt:          application.CreatedAt.Format("2006-01-02"),
		Status:             string(application.Status),
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

func newApplicationDetailResponse(c echo.Context, detail domain.ApplicationDetail) applicationDetailResponse {
	return applicationDetailResponse{
		Application: newApplicationResponse(detail.Application),
		Detail: applicationDetailFields{
			ID:                 detail.ID,
			AppKey:             detail.AppKey,
			Slug:               detail.Slug,
			Name:               detail.Name,
			Description:        detail.Description,
			OwnerUserID:        detail.OwnerUserID,
			Owner:              detail.Owner,
			OwnerEmail:         detail.OwnerEmail,
			OwnerAvatarURL:     detail.OwnerAvatarURL,
			OwnerDepartmentIDs: detail.OwnerDepartmentIDs,
			Metrics:            newApplicationMetricResponses(detail.Metrics),
			Environments:       newApplicationEnvironmentResponses(detail.Environments),
			Modules:            newApplicationModuleResponses(detail.Modules),
			Members:            newApplicationMemberResponses(detail.Members),
			Capabilities:       detail.Capabilities,
			ClientIntegration:  newApplicationClientIntegrationResponse(detail.ClientIntegration, applicationRequestBaseURL(c)),
		},
	}
}

func newApplicationDeleteCheckResponse(check domain.ApplicationDeleteCheck) applicationDeleteCheckResponse {
	dependencies := make([]applicationDeleteDependencyResponse, 0, len(check.Dependencies))
	for _, dependency := range check.Dependencies {
		dependencies = append(dependencies, applicationDeleteDependencyResponse{
			ID:          dependency.ID,
			Name:        dependency.Name,
			Type:        dependency.Type,
			Description: dependency.Description,
		})
	}
	return applicationDeleteCheckResponse{
		Deletable:    check.Deletable,
		Dependencies: dependencies,
		Message:      check.Message,
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
			Status:      string(environment.Status),
		})
	}
	return response
}

func newApplicationModuleResponses(modules []domain.ApplicationModule) []applicationModuleResponse {
	response := make([]applicationModuleResponse, 0, len(modules))
	for _, module := range modules {
		response = append(response, applicationModuleResponse{
			CommonModuleID:   module.CommonModuleID,
			CommonModuleName: module.CommonModuleName,
			ID:               module.ID,
			Kind:             module.Kind,
			Name:             module.Name,
			Owner:            module.Owner,
			Repository:       module.Repository,
		})
	}
	return response
}

func newApplicationMemberResponses(members []domain.ApplicationMember) []applicationMemberResponse {
	response := make([]applicationMemberResponse, 0, len(members))
	for _, member := range members {
		response = append(response, applicationMemberResponseFromDomain(member))
	}
	return response
}

func applicationMemberResponseFromDomain(member domain.ApplicationMember) applicationMemberResponse {
	return applicationMemberResponse{
		AvatarURL: member.AvatarURL,
		Email:     member.Email,
		ID:        member.ID,
		Name:      member.Name,
		Role:      string(member.Role),
		Team:      member.Team,
		UserID:    member.UserID,
	}
}

func applicationRequestBaseURL(c echo.Context) string {
	request := c.Request()
	host := strings.TrimSpace(request.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(request.Host)
	}
	if host == "" {
		return ""
	}

	scheme := strings.TrimSpace(request.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		scheme = c.Scheme()
	}
	if scheme == "" {
		scheme = "http"
	}

	return scheme + "://" + host
}

func newApplicationClientIntegrationResponse(integration domain.ClientIntegrationInfo, baseURL string) applicationClientIntegrationResponse {
	return applicationClientIntegrationResponse{
		Endpoint: applicationClientIntegrationEndpoint{
			BaseURL:        baseURL,
			Path:           integration.Endpoint.Path,
			Method:         integration.Endpoint.Method,
			Description:    integration.Endpoint.Description,
			ExampleURL:     integration.Endpoint.ExampleURL,
			RequestFields:  newApplicationClientIntegrationFieldResponses(integration.Endpoint.RequestFields),
			ResponseFields: newApplicationClientIntegrationFieldResponses(integration.Endpoint.ResponseFields),
		},
	}
}

func newApplicationClientIntegrationFieldResponses(fields []domain.ClientIntegrationField) []applicationClientIntegrationField {
	response := make([]applicationClientIntegrationField, 0, len(fields))
	for _, field := range fields {
		response = append(response, applicationClientIntegrationField{
			Name:        field.Name,
			Description: field.Description,
			Required:    field.Required,
			Type:        field.Type,
			Example:     field.Example,
		})
	}
	return response
}
