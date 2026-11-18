package httpserver

import (
	"errors"
	"net/http"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
)

// 平台首次初始化：租户表为空时，持有启动日志里初始化令牌的人可以创建第一个飞书租户。
// 接口不需要登录；创建成功后永久关闭（返回 409）。
type setupHandler struct {
	tenants FeishuTenantService
}

type setupStatusResponse struct {
	SetupRequired bool `json:"setupRequired"`
}

type setupFeishuTenantRequest struct {
	Token     string `json:"token"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	AppID     string `json:"appId"`
	AppSecret string `json:"appSecret"`
}

func registerSetupRoutes(group *echo.Group, tenants FeishuTenantService) {
	handler := setupHandler{tenants: tenants}
	// 令牌有 256 位随机性，限流只是额外防护：每个来源 IP 每秒 1 次，突发 5 次。
	limiter := middleware.RateLimiter(middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{
		Rate:  rate.Limit(1),
		Burst: 5,
	}))
	setup := group.Group("/setup", limiter)
	setup.GET("/status", handler.status)
	setup.POST("/feishu-tenant", handler.createFirstTenant)
}

func (h setupHandler) status(c echo.Context) error {
	if h.tenants == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "setup unavailable"})
	}
	required, err := h.tenants.SetupRequired(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	return c.JSON(http.StatusOK, setupStatusResponse{SetupRequired: required})
}

func (h setupHandler) createFirstTenant(c echo.Context) error {
	if h.tenants == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "setup unavailable"})
	}
	var request setupFeishuTenantRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid feishu tenant request"})
	}
	tenant, err := h.tenants.SetupFirstTenant(c.Request().Context(), request.Token, domain.FeishuTenantInput{
		ID:        request.ID,
		Name:      request.Name,
		AppID:     request.AppID,
		AppSecret: request.AppSecret,
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrPlatformSetupCompleted):
			return c.JSON(http.StatusConflict, map[string]string{"error": "platform setup already completed"})
		case errors.Is(err, domain.ErrInvalidSetupToken):
			return c.JSON(http.StatusForbidden, map[string]string{"error": "invalid setup token"})
		}
		return feishuTenantErrorResponse(c, err)
	}
	return c.JSON(http.StatusCreated, feishuTenantDetailResponse{Tenant: newFeishuTenantResponse(tenant)})
}
