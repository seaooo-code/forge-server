package httpserver

import (
	"errors"
	"net/http"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

// 租户管理。只有 SuperAdmin 能读写：租户的飞书应用决定谁能登录平台。接口永远不返回飞书 App Secret。
type tenantHandler struct {
	authService AuthorizationService
	service     TenantService
}

type tenantRequest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	FeishuAppID     string `json:"feishuAppId"`
	FeishuAppSecret string `json:"feishuAppSecret"`
	Status          string `json:"status"`
	SortOrder       int    `json:"sortOrder"`
}

type tenantResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	FeishuAppID string `json:"feishuAppId"`
	Status      string `json:"status"`
	SortOrder   int    `json:"sortOrder"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type tenantsResponse struct {
	Tenants []tenantResponse `json:"tenants"`
}

type tenantDetailResponse struct {
	Tenant tenantResponse `json:"tenant"`
}

func registerTenantRoutes(group *echo.Group, service TenantService, authService AuthorizationService) {
	handler := tenantHandler{authService: authService, service: service}

	group.GET("/tenants", handler.list)
	group.POST("/tenants", handler.create)
	group.PATCH("/tenants/:id", handler.update)
}

func (h tenantHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "tenant service unavailable"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionTenantManage, domain.AuthorizationResource{}); err != nil {
		return err
	}
	tenants, err := h.service.ListTenants(c.Request().Context())
	if err != nil {
		return tenantErrorResponse(c, err)
	}
	response := tenantsResponse{Tenants: make([]tenantResponse, 0, len(tenants))}
	for _, tenant := range tenants {
		response.Tenants = append(response.Tenants, newTenantResponse(tenant))
	}
	return c.JSON(http.StatusOK, response)
}

func (h tenantHandler) create(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "tenant service unavailable"})
	}
	input, ok := bindTenantRequest(c)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid tenant request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionTenantManage, domain.AuthorizationResource{}); err != nil {
		return err
	}
	tenant, err := h.service.CreateTenant(c.Request().Context(), input)
	if err != nil {
		return tenantErrorResponse(c, err)
	}
	return c.JSON(http.StatusCreated, tenantDetailResponse{Tenant: newTenantResponse(tenant)})
}

func (h tenantHandler) update(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "tenant service unavailable"})
	}
	input, ok := bindTenantRequest(c)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid tenant request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionTenantManage, domain.AuthorizationResource{}); err != nil {
		return err
	}
	tenant, err := h.service.UpdateTenant(c.Request().Context(), c.Param("id"), input)
	if err != nil {
		return tenantErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, tenantDetailResponse{Tenant: newTenantResponse(tenant)})
}

func bindTenantRequest(c echo.Context) (domain.TenantInput, bool) {
	var request tenantRequest
	if err := c.Bind(&request); err != nil {
		return domain.TenantInput{}, false
	}
	return domain.TenantInput{
		ID:              request.ID,
		Name:            request.Name,
		FeishuAppID:     request.FeishuAppID,
		FeishuAppSecret: request.FeishuAppSecret,
		Status:          domain.TenantStatus(request.Status),
		SortOrder:       request.SortOrder,
	}, true
}

func tenantErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrTenantNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "tenant not found"})
	case errors.Is(err, domain.ErrInvalidTenantInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid tenant request"})
	case errors.Is(err, domain.ErrTenantIDConflict):
		return c.JSON(http.StatusConflict, map[string]string{"error": "tenant id already exists"})
	case errors.Is(err, domain.ErrFeishuAppIDConflict):
		return c.JSON(http.StatusConflict, map[string]string{"error": "feishu app id already used by another tenant"})
	case errors.Is(err, domain.ErrTenantLastActive):
		return c.JSON(http.StatusConflict, map[string]string{"error": "at least one tenant must stay active"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func newTenantResponse(tenant domain.Tenant) tenantResponse {
	return tenantResponse{
		ID:          tenant.ID,
		Name:        tenant.Name,
		FeishuAppID: tenant.FeishuAppID,
		Status:      string(tenant.Status),
		SortOrder:   tenant.SortOrder,
		CreatedAt:   formatTime(tenant.CreatedAt),
		UpdatedAt:   formatTime(tenant.UpdatedAt),
	}
}
