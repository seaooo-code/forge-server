package httpserver

import (
	"errors"
	"net/http"
	"strings"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type moduleHandler struct {
	authService AuthorizationService
	service     ModuleService
}

type modulesResponse struct {
	Modules    []moduleResponse      `json:"modules"`
	Pagination paginationResponse    `json:"pagination"`
	Summary    moduleSummaryResponse `json:"summary"`
}

type moduleDetailResponse struct {
	Module moduleResponse `json:"module"`
}

type moduleResponse struct {
	ApplicationID            string                    `json:"applicationId"`
	ApplicationName          string                    `json:"applicationName"`
	ApplicationSlug          string                    `json:"applicationSlug"`
	BuildConfig              moduleBuildConfigResponse `json:"buildConfig"`
	CodeSourceConnectionID   string                    `json:"codeSourceConnectionId"`
	CodeSourceConnectionName string                    `json:"codeSourceConnectionName"`
	CreatedAt                string                    `json:"createdAt"`
	DefaultBranch            string                    `json:"defaultBranch"`
	ID                       string                    `json:"id"`
	Kind                     string                    `json:"kind"`
	CommonModuleID           string                    `json:"commonModuleId"`
	CommonModuleName         string                    `json:"commonModuleName"`
	Name                     string                    `json:"name"`
	Owner                    string                    `json:"owner"`
	OwnerAvatarURL           string                    `json:"ownerAvatarUrl"`
	OwnerEmail               string                    `json:"ownerEmail"`
	OwnerUserID              string                    `json:"ownerUserId"`
	Repository               string                    `json:"repository"`
	RepositoryCloneURL       string                    `json:"repositoryCloneUrl"`
	RepositoryFullName       string                    `json:"repositoryFullName"`
	RepositoryID             string                    `json:"repositoryId"`
	RepositoryProvider       string                    `json:"repositoryProvider"`
	RepositoryWebURL         string                    `json:"repositoryWebUrl"`
	UpdatedAt                string                    `json:"updatedAt"`
}

type moduleBuildConfigResponse struct {
	AutoBuildBranches    []string `json:"autoBuildBranches"`
	AutoBuildEnvironment string   `json:"autoBuildEnvironment"`
	AutoPreviewRelease   bool     `json:"autoPreviewRelease"`
	AutoPreviewTTLHours  int      `json:"autoPreviewTtlHours"`
	BuildContext         string   `json:"buildContext"`
	DockerfilePath       string   `json:"dockerfilePath"`
}

type moduleSummaryResponse struct {
	Common  int `json:"common"`
	Feature int `json:"feature"`
	Total   int `json:"total"`
}

type moduleRequest struct {
	ApplicationID      string                   `json:"applicationId"`
	CommonModuleID     string                   `json:"commonModuleId"`
	ID                 string                   `json:"id"`
	Kind               domain.ModuleKind        `json:"kind"`
	Name               string                   `json:"name"`
	Owner              string                   `json:"owner"`
	OwnerAvatarURL     string                   `json:"ownerAvatarUrl"`
	OwnerDepartmentIDs []string                 `json:"ownerDepartmentIds"`
	OwnerEmail         string                   `json:"ownerEmail"`
	OwnerFeishuOpenID  string                   `json:"ownerFeishuOpenId"`
	OwnerFeishuUnionID string                   `json:"ownerFeishuUnionId"`
	OwnerFeishuUserID  string                   `json:"ownerFeishuUserId"`
	OwnerUserID        string                   `json:"ownerUserId"`
	Repository         moduleRepositoryRequest  `json:"repository"`
	BuildConfig        moduleBuildConfigRequest `json:"buildConfig"`
}

// moduleRepositoryRequest 只需要连接和外部仓库 id，仓库名称、地址等以服务端仓库缓存为准。
type moduleRepositoryRequest struct {
	ConnectionID         string `json:"connectionId"`
	ExternalRepositoryID string `json:"externalRepositoryId"`
}

type moduleBuildConfigRequest struct {
	AutoBuildBranches    []string `json:"autoBuildBranches"`
	AutoBuildEnvironment string   `json:"autoBuildEnvironment"`
	AutoPreviewRelease   bool     `json:"autoPreviewRelease"`
	AutoPreviewTTLHours  int      `json:"autoPreviewTtlHours"`
	BuildContext         string   `json:"buildContext"`
	DockerfilePath       string   `json:"dockerfilePath"`
}

func registerModuleRoutes(group *echo.Group, service ModuleService, authService AuthorizationService) {
	handler := moduleHandler{authService: authService, service: service}

	group.GET("/modules", handler.list)
	group.GET("/modules/:id", handler.detail)
	group.POST("/modules", handler.create)
	group.PATCH("/modules/:id", handler.update)
	group.DELETE("/modules/:id", handler.delete)
}

func (h moduleHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "module service unavailable"})
	}

	pagination, ok := requestPagination(c)
	if !ok {
		return moduleErrorResponse(c, domain.ErrInvalidModuleInput)
	}

	modules, summary, err := h.service.ListModules(c.Request().Context(), domain.ModuleListFilters{
		ApplicationID: strings.TrimSpace(c.QueryParam("applicationId")),
		Kind:          domain.ModuleKind(strings.TrimSpace(c.QueryParam("kind"))),
		Keyword:       strings.TrimSpace(c.QueryParam("keyword")),
		Owner:         strings.TrimSpace(c.QueryParam("owner")),
		Pagination:    pagination,
	})
	if err != nil {
		return moduleErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, modulesResponse{
		Modules:    newModuleResponses(modules),
		Pagination: newPaginationResponse(pagination, summary.Total),
		Summary:    newModuleSummaryResponse(summary),
	})
}

func (h moduleHandler) detail(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "module service unavailable"})
	}

	module, err := h.service.GetModule(c.Request().Context(), c.Param("id"))
	if err != nil {
		return moduleErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, moduleDetailResponse{Module: newModuleResponse(module)})
}

func (h moduleHandler) create(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "module service unavailable"})
	}

	input, ok := bindModuleRequest(c, true)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid module request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionModuleCreate, domain.AuthorizationResource{ApplicationID: input.ApplicationID}); err != nil {
		return err
	}

	module, err := h.service.CreateModule(c.Request().Context(), input)
	if err != nil {
		return moduleErrorResponse(c, err)
	}

	return c.JSON(http.StatusCreated, moduleDetailResponse{Module: newModuleResponse(module)})
}

func (h moduleHandler) update(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "module service unavailable"})
	}

	input, ok := bindModuleRequest(c, false)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid module request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionModuleUpdate, domain.AuthorizationResource{ModuleID: c.Param("id")}); err != nil {
		return err
	}

	module, err := h.service.UpdateModule(c.Request().Context(), c.Param("id"), input)
	if err != nil {
		return moduleErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, moduleDetailResponse{Module: newModuleResponse(module)})
}

func (h moduleHandler) delete(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "module service unavailable"})
	}

	if err := authorizeCurrentUser(c, h.authService, domain.ActionModuleDelete, domain.AuthorizationResource{ModuleID: c.Param("id")}); err != nil {
		return err
	}
	if err := h.service.DeleteModule(c.Request().Context(), c.Param("id")); err != nil {
		return moduleErrorResponse(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

func bindModuleRequest(c echo.Context, includeID bool) (domain.ModuleInput, bool) {
	var request moduleRequest
	if err := c.Bind(&request); err != nil {
		return domain.ModuleInput{}, false
	}

	input := domain.ModuleInput{
		ApplicationID:      request.ApplicationID,
		CommonModuleID:     request.CommonModuleID,
		Kind:               request.Kind,
		Name:               request.Name,
		Owner:              request.Owner,
		OwnerAvatarURL:     request.OwnerAvatarURL,
		OwnerDepartmentIDs: request.OwnerDepartmentIDs,
		OwnerEmail:         request.OwnerEmail,
		OwnerFeishuOpenID:  request.OwnerFeishuOpenID,
		OwnerFeishuUnionID: request.OwnerFeishuUnionID,
		OwnerFeishuUserID:  request.OwnerFeishuUserID,
		OwnerUserID:        request.OwnerUserID,
		Repository: domain.ModuleRepository{
			ConnectionID:         request.Repository.ConnectionID,
			ExternalRepositoryID: request.Repository.ExternalRepositoryID,
		},
		BuildConfig: domain.ModuleBuildConfig{
			AutoBuildBranches:    request.BuildConfig.AutoBuildBranches,
			AutoBuildEnvironment: request.BuildConfig.AutoBuildEnvironment,
			AutoPreviewRelease:   request.BuildConfig.AutoPreviewRelease,
			AutoPreviewTTLHours:  request.BuildConfig.AutoPreviewTTLHours,
			BuildContext:         request.BuildConfig.BuildContext,
			DockerfilePath:       request.BuildConfig.DockerfilePath,
		},
	}
	if includeID {
		input.ID = request.ID
	}

	return input, true
}

func moduleErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrModuleNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "module not found"})
	case errors.Is(err, domain.ErrModuleIDConflict):
		return c.JSON(http.StatusConflict, map[string]string{"error": "module id already exists"})
	case errors.Is(err, domain.ErrModuleRepositoryConflict):
		return c.JSON(http.StatusConflict, map[string]string{"error": "module repository already exists"})
	case errors.Is(err, domain.ErrModuleCommonConflict):
		return c.JSON(http.StatusConflict, map[string]string{"error": "application already has common module"})
	case errors.Is(err, domain.ErrModuleInUse):
		return c.JSON(http.StatusConflict, map[string]string{"error": "module is referenced"})
	case errors.Is(err, domain.ErrModuleCommonMissing):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "feature module requires common module"})
	case errors.Is(err, domain.ErrModuleCommonApplication):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "common module must belong to same application"})
	case errors.Is(err, domain.ErrInvalidModuleInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid module request"})
	case errors.Is(err, domain.ErrCodeSourceConnectionInactive):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "code source connection is not active"})
	case errors.Is(err, domain.ErrForbidden):
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func newModuleResponses(modules []domain.Module) []moduleResponse {
	response := make([]moduleResponse, 0, len(modules))
	for _, module := range modules {
		response = append(response, newModuleResponse(module))
	}
	return response
}

func newModuleResponse(module domain.Module) moduleResponse {
	return moduleResponse{
		ApplicationID:   module.ApplicationID,
		ApplicationName: module.ApplicationName,
		ApplicationSlug: module.ApplicationSlug,
		BuildConfig: moduleBuildConfigResponse{
			AutoBuildBranches:    nonNilStrings(module.BuildConfig.AutoBuildBranches),
			AutoBuildEnvironment: module.BuildConfig.AutoBuildEnvironment,
			AutoPreviewRelease:   module.BuildConfig.AutoPreviewRelease,
			AutoPreviewTTLHours:  module.BuildConfig.AutoPreviewTTLHours,
			BuildContext:         module.BuildConfig.BuildContext,
			DockerfilePath:       module.BuildConfig.DockerfilePath,
		},
		CodeSourceConnectionID:   module.Repository.ConnectionID,
		CodeSourceConnectionName: module.ConnectionName,
		CreatedAt:                formatTime(module.CreatedAt),
		DefaultBranch:            module.Repository.DefaultBranch,
		ID:                       module.ID,
		Kind:                     string(module.Kind),
		CommonModuleID:           module.CommonModuleID,
		CommonModuleName:         module.CommonModuleName,
		Name:                     module.Name,
		Owner:                    module.Owner,
		OwnerAvatarURL:           module.OwnerAvatarURL,
		OwnerEmail:               module.OwnerEmail,
		OwnerUserID:              module.OwnerUserID,
		Repository:               module.Repository.FullName,
		RepositoryCloneURL:       module.Repository.CloneURL,
		RepositoryFullName:       module.Repository.FullName,
		RepositoryID:             module.Repository.ExternalRepositoryID,
		RepositoryProvider:       string(module.CodeSourceProvider),
		RepositoryWebURL:         module.Repository.WebURL,
		UpdatedAt:                formatTime(module.UpdatedAt),
	}
}

func newModuleSummaryResponse(summary domain.ModuleSummary) moduleSummaryResponse {
	return moduleSummaryResponse{
		Common:  summary.Common,
		Feature: summary.Feature,
		Total:   summary.Total,
	}
}
