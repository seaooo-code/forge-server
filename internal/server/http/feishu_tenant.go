package httpserver

import (
	"errors"
	"net/http"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

// 飞书登录租户管理。只有 SuperAdmin 能读写：租户凭证决定谁能登录平台。接口永远不返回 App Secret。
type feishuTenantHandler struct {
	authService AuthorizationService
	service     FeishuTenantService
}

type feishuTenantRequest struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AppID     string `json:"appId"`
	AppSecret string `json:"appSecret"`
	Status    string `json:"status"`
	SortOrder int    `json:"sortOrder"`
}

type feishuTenantResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AppID     string `json:"appId"`
	Status    string `json:"status"`
	SortOrder int    `json:"sortOrder"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type feishuTenantsResponse struct {
	Tenants []feishuTenantResponse `json:"tenants"`
}

type feishuTenantDetailResponse struct {
	Tenant feishuTenantResponse `json:"tenant"`
}

func registerFeishuTenantRoutes(group *echo.Group, service FeishuTenantService, authService AuthorizationService) {
	handler := feishuTenantHandler{authService: authService, service: service}

	group.GET("/feishu-tenants", handler.list)
	group.POST("/feishu-tenants", handler.create)
	group.PATCH("/feishu-tenants/:id", handler.update)
}

func (h feishuTenantHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "feishu tenant service unavailable"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionFeishuTenantManage, domain.AuthorizationResource{}); err != nil {
		return err
	}
	tenants, err := h.service.ListFeishuTenants(c.Request().Context())
	if err != nil {
		return feishuTenantErrorResponse(c, err)
	}
	response := feishuTenantsResponse{Tenants: make([]feishuTenantResponse, 0, len(tenants))}
	for _, tenant := range tenants {
		response.Tenants = append(response.Tenants, newFeishuTenantResponse(tenant))
	}
	return c.JSON(http.StatusOK, response)
}

func (h feishuTenantHandler) create(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "feishu tenant service unavailable"})
	}
	input, ok := bindFeishuTenantRequest(c)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid feishu tenant request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionFeishuTenantManage, domain.AuthorizationResource{}); err != nil {
		return err
	}
	tenant, err := h.service.CreateFeishuTenant(c.Request().Context(), input)
	if err != nil {
		return feishuTenantErrorResponse(c, err)
	}
	return c.JSON(http.StatusCreated, feishuTenantDetailResponse{Tenant: newFeishuTenantResponse(tenant)})
}

func (h feishuTenantHandler) update(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "feishu tenant service unavailable"})
	}
	input, ok := bindFeishuTenantRequest(c)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid feishu tenant request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionFeishuTenantManage, domain.AuthorizationResource{}); err != nil {
		return err
	}
	tenant, err := h.service.UpdateFeishuTenant(c.Request().Context(), c.Param("id"), input)
	if err != nil {
		return feishuTenantErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, feishuTenantDetailResponse{Tenant: newFeishuTenantResponse(tenant)})
}

func bindFeishuTenantRequest(c echo.Context) (domain.FeishuTenantInput, bool) {
	var request feishuTenantRequest
	if err := c.Bind(&request); err != nil {
		return domain.FeishuTenantInput{}, false
	}
	return domain.FeishuTenantInput{
		ID:        request.ID,
		Name:      request.Name,
		AppID:     request.AppID,
		AppSecret: request.AppSecret,
		Status:    domain.FeishuTenantStatus(request.Status),
		SortOrder: request.SortOrder,
	}, true
}

func feishuTenantErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrFeishuTenantNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "feishu tenant not found"})
	case errors.Is(err, domain.ErrInvalidFeishuTenantInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid feishu tenant request"})
	case errors.Is(err, domain.ErrFeishuTenantIDConflict):
		return c.JSON(http.StatusConflict, map[string]string{"error": "feishu tenant id already exists"})
	case errors.Is(err, domain.ErrFeishuTenantAppIDConflict):
		return c.JSON(http.StatusConflict, map[string]string{"error": "feishu tenant app id already exists"})
	case errors.Is(err, domain.ErrFeishuTenantLastActive):
		return c.JSON(http.StatusConflict, map[string]string{"error": "at least one feishu tenant must stay active"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func newFeishuTenantResponse(tenant domain.FeishuTenant) feishuTenantResponse {
	return feishuTenantResponse{
		ID:        tenant.ID,
		Name:      tenant.Name,
		AppID:     tenant.AppID,
		Status:    string(tenant.Status),
		SortOrder: tenant.SortOrder,
		CreatedAt: formatTime(tenant.CreatedAt),
		UpdatedAt: formatTime(tenant.UpdatedAt),
	}
}
