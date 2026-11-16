package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type BuildAdapterService interface {
	CreateBuildAdapter(ctx context.Context, input domain.BuildAdapterInput) (domain.BuildAdapter, error)
	DisableBuildAdapter(ctx context.Context, id string, userID string) (domain.BuildAdapter, error)
	EnableBuildAdapter(ctx context.Context, id string, userID string) (domain.BuildAdapter, error)
	GetBuildAdapter(ctx context.Context, id string) (domain.BuildAdapter, error)
	ListBuildAdapters(ctx context.Context, filters domain.BuildAdapterFilters) ([]domain.BuildAdapter, error)
	PreviewBuildAdapter(ctx context.Context, id string, input domain.BuildAdapterPreviewInput) (domain.BuildAdapterRenderResult, error)
	PreviewBuildAdapterDraft(ctx context.Context, draft domain.BuildAdapterInput, input domain.BuildAdapterPreviewInput) (domain.BuildAdapterRenderResult, error)
	UpdateBuildAdapter(ctx context.Context, id string, patch domain.BuildAdapterPatch) (domain.BuildAdapter, error)
}

type buildAdapterHandler struct {
	authService AuthorizationService
	service     BuildAdapterService
}

type buildAdapterParameterPayload struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Default     string `json:"default"`
}

type buildAdapterCreateRequest struct {
	Name               string                         `json:"name"`
	Description        string                         `json:"description"`
	DockerfileTemplate string                         `json:"dockerfileTemplate"`
	Files              map[string]string              `json:"files"`
	Parameters         []buildAdapterParameterPayload `json:"parameters"`
}

// buildAdapterUpdateRequest 的字段省略时保持不变；files、parameters 传入时整体替换。
type buildAdapterUpdateRequest struct {
	Name               *string                         `json:"name"`
	Description        *string                         `json:"description"`
	DockerfileTemplate *string                         `json:"dockerfileTemplate"`
	Files              *map[string]string              `json:"files"`
	Parameters         *[]buildAdapterParameterPayload `json:"parameters"`
	ExpectedVersion    int                             `json:"expectedVersion"`
}

type buildAdapterRenderPreviewRequest struct {
	ModuleID string            `json:"moduleId"`
	Params   map[string]string `json:"params"`
}

// buildAdapterDraftRenderPreviewRequest 预览尚未保存的草稿，用于编辑页实时预览。
type buildAdapterDraftRenderPreviewRequest struct {
	DockerfileTemplate string                         `json:"dockerfileTemplate"`
	Files              map[string]string              `json:"files"`
	Parameters         []buildAdapterParameterPayload `json:"parameters"`
	ModuleID           string                         `json:"moduleId"`
	Params             map[string]string              `json:"params"`
}

type buildAdapterResponse struct {
	ID                 string                         `json:"id"`
	Name               string                         `json:"name"`
	Description        string                         `json:"description"`
	DockerfileTemplate string                         `json:"dockerfileTemplate"`
	Files              map[string]string              `json:"files"`
	Parameters         []buildAdapterParameterPayload `json:"parameters"`
	Version            int                            `json:"version"`
	Status             string                         `json:"status"`
	ModuleCount        int                            `json:"moduleCount"`
	CreatedByUserID    string                         `json:"createdByUserId"`
	CreatedBy          *userSummaryResponse           `json:"createdBy"`
	UpdatedByUserID    string                         `json:"updatedByUserId"`
	UpdatedBy          *userSummaryResponse           `json:"updatedBy"`
	CreatedAt          string                         `json:"createdAt"`
	UpdatedAt          string                         `json:"updatedAt"`
}

type buildAdaptersResponse struct {
	BuildAdapters []buildAdapterResponse `json:"buildAdapters"`
}

type buildAdapterDetailResponse struct {
	BuildAdapter buildAdapterResponse `json:"buildAdapter"`
}

type buildAdapterRenderErrorResponse struct {
	Message  string `json:"message"`
	Template string `json:"template"`
	Line     int    `json:"line"`
}

type buildAdapterRenderPreviewResponse struct {
	OK               bool                             `json:"ok"`
	Dockerfile       string                           `json:"dockerfile"`
	DockerfileSHA256 string                           `json:"dockerfileSha256"`
	Files            map[string]string                `json:"files"`
	Error            *buildAdapterRenderErrorResponse `json:"error"`
}

// buildAdapterErrorBody 是保存适配器失败时的响应：模板错误额外给出模板名与行号。
type buildAdapterErrorBody struct {
	Error    string `json:"error"`
	Template string `json:"template,omitempty"`
	Line     int    `json:"line,omitempty"`
}

// registerBuildAdapterRoutes 注册构建适配器接口：管理需要 build_adapter:manage（PlatformAdmin/SuperAdmin），
// 读取与预览需要 build_adapter:use（任一应用里能管理模块的人）。适配器不提供删除接口，只能停用。
func registerBuildAdapterRoutes(group *echo.Group, service BuildAdapterService, authService AuthorizationService) {
	handler := buildAdapterHandler{authService: authService, service: service}

	group.GET("/build-adapters", handler.list)
	group.POST("/build-adapters", handler.create)
	group.POST("/build-adapters/render-preview", handler.renderDraftPreview)
	group.GET("/build-adapters/:id", handler.detail)
	group.PATCH("/build-adapters/:id", handler.update)
	group.POST("/build-adapters/:id/disable", handler.disable)
	group.POST("/build-adapters/:id/enable", handler.enable)
	group.POST("/build-adapters/:id/render-preview", handler.renderPreview)
}

func (h buildAdapterHandler) list(c echo.Context) error {
	if h.service == nil {
		return buildAdapterUnavailable(c)
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionBuildAdapterUse, domain.AuthorizationResource{}); err != nil {
		return err
	}
	adapters, err := h.service.ListBuildAdapters(c.Request().Context(), domain.BuildAdapterFilters{
		Status: domain.BuildAdapterStatus(strings.TrimSpace(c.QueryParam("status"))),
	})
	if err != nil {
		return buildAdapterErrorResponse(c, err)
	}
	response := buildAdaptersResponse{BuildAdapters: make([]buildAdapterResponse, 0, len(adapters))}
	for _, adapter := range adapters {
		response.BuildAdapters = append(response.BuildAdapters, newBuildAdapterResponse(adapter))
	}
	return c.JSON(http.StatusOK, response)
}

func (h buildAdapterHandler) detail(c echo.Context) error {
	if h.service == nil {
		return buildAdapterUnavailable(c)
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionBuildAdapterUse, domain.AuthorizationResource{}); err != nil {
		return err
	}
	adapter, err := h.service.GetBuildAdapter(c.Request().Context(), c.Param("id"))
	if err != nil {
		return buildAdapterErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, buildAdapterDetailResponse{BuildAdapter: newBuildAdapterResponse(adapter)})
}

func (h buildAdapterHandler) create(c echo.Context) error {
	if h.service == nil {
		return buildAdapterUnavailable(c)
	}
	var request buildAdapterCreateRequest
	if err := c.Bind(&request); err != nil {
		return buildAdapterErrorResponse(c, domain.ErrInvalidBuildAdapterInput)
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionBuildAdapterManage, domain.AuthorizationResource{}); err != nil {
		return err
	}
	session, _ := consoleSession(c)

	adapter, err := h.service.CreateBuildAdapter(c.Request().Context(), domain.BuildAdapterInput{
		Name:               request.Name,
		Description:        request.Description,
		DockerfileTemplate: request.DockerfileTemplate,
		Files:              request.Files,
		Parameters:         buildAdapterParametersFromPayload(request.Parameters),
		UserID:             domain.ConsoleUserIDFromFeishu(session.User),
	})
	if err != nil {
		return buildAdapterErrorResponse(c, err)
	}
	return c.JSON(http.StatusCreated, buildAdapterDetailResponse{BuildAdapter: newBuildAdapterResponse(adapter)})
}

func (h buildAdapterHandler) update(c echo.Context) error {
	if h.service == nil {
		return buildAdapterUnavailable(c)
	}
	var request buildAdapterUpdateRequest
	if err := c.Bind(&request); err != nil {
		return buildAdapterErrorResponse(c, domain.ErrInvalidBuildAdapterInput)
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionBuildAdapterManage, domain.AuthorizationResource{}); err != nil {
		return err
	}
	session, _ := consoleSession(c)

	patch := domain.BuildAdapterPatch{
		Name:               request.Name,
		Description:        request.Description,
		DockerfileTemplate: request.DockerfileTemplate,
		Files:              request.Files,
		ExpectedVersion:    request.ExpectedVersion,
		UserID:             domain.ConsoleUserIDFromFeishu(session.User),
	}
	if request.Parameters != nil {
		parameters := buildAdapterParametersFromPayload(*request.Parameters)
		patch.Parameters = &parameters
	}
	adapter, err := h.service.UpdateBuildAdapter(c.Request().Context(), c.Param("id"), patch)
	if err != nil {
		return buildAdapterErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, buildAdapterDetailResponse{BuildAdapter: newBuildAdapterResponse(adapter)})
}

func (h buildAdapterHandler) disable(c echo.Context) error {
	return h.setStatus(c, h.service.DisableBuildAdapter)
}

func (h buildAdapterHandler) enable(c echo.Context) error {
	return h.setStatus(c, h.service.EnableBuildAdapter)
}

func (h buildAdapterHandler) setStatus(c echo.Context, apply func(ctx context.Context, id string, userID string) (domain.BuildAdapter, error)) error {
	if h.service == nil {
		return buildAdapterUnavailable(c)
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionBuildAdapterManage, domain.AuthorizationResource{}); err != nil {
		return err
	}
	session, _ := consoleSession(c)
	adapter, err := apply(c.Request().Context(), c.Param("id"), domain.ConsoleUserIDFromFeishu(session.User))
	if err != nil {
		return buildAdapterErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, buildAdapterDetailResponse{BuildAdapter: newBuildAdapterResponse(adapter)})
}

// renderPreview 渲染失败（模板或参数错误）时仍返回 200，ok=false 并给出错误；适配器或模块不存在返回 404。
func (h buildAdapterHandler) renderPreview(c echo.Context) error {
	if h.service == nil {
		return buildAdapterUnavailable(c)
	}
	var request buildAdapterRenderPreviewRequest
	if err := c.Bind(&request); err != nil {
		return buildAdapterErrorResponse(c, domain.ErrInvalidBuildAdapterInput)
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionBuildAdapterUse, domain.AuthorizationResource{}); err != nil {
		return err
	}

	result, err := h.service.PreviewBuildAdapter(c.Request().Context(), c.Param("id"), domain.BuildAdapterPreviewInput{
		ModuleID: request.ModuleID,
		Params:   request.Params,
	})
	return writeBuildAdapterPreview(c, result, err)
}

// renderDraftPreview 预览草稿需要管理权限；模板、文件、参数定义或参数值不合法时返回 200 且 ok=false。
func (h buildAdapterHandler) renderDraftPreview(c echo.Context) error {
	if h.service == nil {
		return buildAdapterUnavailable(c)
	}
	var request buildAdapterDraftRenderPreviewRequest
	if err := c.Bind(&request); err != nil {
		return buildAdapterErrorResponse(c, domain.ErrInvalidBuildAdapterInput)
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionBuildAdapterManage, domain.AuthorizationResource{}); err != nil {
		return err
	}

	result, err := h.service.PreviewBuildAdapterDraft(c.Request().Context(), domain.BuildAdapterInput{
		DockerfileTemplate: request.DockerfileTemplate,
		Files:              request.Files,
		Parameters:         buildAdapterParametersFromPayload(request.Parameters),
	}, domain.BuildAdapterPreviewInput{
		ModuleID: request.ModuleID,
		Params:   request.Params,
	})
	return writeBuildAdapterPreview(c, result, err)
}

func writeBuildAdapterPreview(c echo.Context, result domain.BuildAdapterRenderResult, err error) error {
	var renderErr *domain.BuildAdapterRenderError
	switch {
	case err == nil:
		files := result.Files
		if files == nil {
			files = map[string]string{}
		}
		return c.JSON(http.StatusOK, buildAdapterRenderPreviewResponse{
			OK:               true,
			Dockerfile:       result.Dockerfile,
			DockerfileSHA256: result.DockerfileSHA256,
			Files:            files,
		})
	case errors.As(err, &renderErr):
		return c.JSON(http.StatusOK, buildAdapterRenderPreviewResponse{
			Files: map[string]string{},
			Error: &buildAdapterRenderErrorResponse{Message: renderErr.Message, Template: renderErr.Template, Line: renderErr.Line},
		})
	case errors.Is(err, domain.ErrInvalidBuildAdapterInput):
		return c.JSON(http.StatusOK, buildAdapterRenderPreviewResponse{
			Files: map[string]string{},
			Error: &buildAdapterRenderErrorResponse{Message: err.Error()},
		})
	case errors.Is(err, domain.ErrModuleNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "module not found"})
	default:
		return buildAdapterErrorResponse(c, err)
	}
}

func buildAdapterParametersFromPayload(payload []buildAdapterParameterPayload) []domain.BuildAdapterParameter {
	parameters := make([]domain.BuildAdapterParameter, 0, len(payload))
	for _, item := range payload {
		parameters = append(parameters, domain.BuildAdapterParameter{
			Key:         item.Key,
			Label:       item.Label,
			Description: item.Description,
			Required:    item.Required,
			Default:     item.Default,
		})
	}
	return parameters
}

func newBuildAdapterResponse(adapter domain.BuildAdapter) buildAdapterResponse {
	files := adapter.Files
	if files == nil {
		files = map[string]string{}
	}
	parameters := make([]buildAdapterParameterPayload, 0, len(adapter.Parameters))
	for _, parameter := range adapter.Parameters {
		parameters = append(parameters, buildAdapterParameterPayload(parameter))
	}
	response := buildAdapterResponse{
		ID:                 adapter.ID,
		Name:               adapter.Name,
		Description:        adapter.Description,
		DockerfileTemplate: adapter.DockerfileTemplate,
		Files:              files,
		Parameters:         parameters,
		Version:            adapter.Version,
		Status:             string(adapter.Status),
		ModuleCount:        adapter.ModuleCount,
		CreatedByUserID:    adapter.CreatedByUserID,
		UpdatedByUserID:    adapter.UpdatedByUserID,
		CreatedAt:          formatTime(adapter.CreatedAt),
		UpdatedAt:          formatTime(adapter.UpdatedAt),
	}
	if adapter.CreatedBy != nil {
		response.CreatedBy = &userSummaryResponse{AvatarURL: adapter.CreatedBy.AvatarURL, ID: adapter.CreatedBy.ID, Name: adapter.CreatedBy.Name}
	}
	if adapter.UpdatedBy != nil {
		response.UpdatedBy = &userSummaryResponse{AvatarURL: adapter.UpdatedBy.AvatarURL, ID: adapter.UpdatedBy.ID, Name: adapter.UpdatedBy.Name}
	}
	return response
}

func buildAdapterUnavailable(c echo.Context) error {
	return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "build adapter service unavailable"})
}

func buildAdapterErrorResponse(c echo.Context, err error) error {
	var renderErr *domain.BuildAdapterRenderError
	switch {
	case errors.As(err, &renderErr):
		return c.JSON(http.StatusBadRequest, buildAdapterErrorBody{Error: renderErr.Error(), Template: renderErr.Template, Line: renderErr.Line})
	case errors.Is(err, domain.ErrInvalidBuildAdapterInput):
		return c.JSON(http.StatusBadRequest, buildAdapterErrorBody{Error: err.Error()})
	case errors.Is(err, domain.ErrBuildAdapterNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "build adapter not found"})
	case errors.Is(err, domain.ErrBuildAdapterVersionConflict):
		return c.JSON(http.StatusConflict, map[string]string{"error": "build adapter was modified concurrently"})
	case errors.Is(err, domain.ErrForbidden):
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}
