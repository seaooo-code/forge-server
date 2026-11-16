package httpserver

import (
	"errors"
	"io"
	"net/http"
	"net/textproto"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

// GitHub 文档中 webhook payload 上限为 25 MB。
const maxWebhookBodyBytes = 25 << 20

type webhookHandler struct {
	service WebhookService
}

// registerWebhookRoutes 注册代码托管方回调，不走飞书 session，来源校验由各 provider 的 ParseWebhook 完成。
func registerWebhookRoutes(group *echo.Group, service WebhookService) {
	handler := webhookHandler{service: service}

	// GitHub App 的所有 installation 共用一个回调地址。
	group.POST("/webhooks/github", handler.handle(domain.CodeSourceProviderGitHub))
	// GitLab webhook 按项目配置，回调地址带连接 id。
	group.POST("/webhooks/gitlab/:connectionId", handler.handle(domain.CodeSourceProviderGitLab))
}

func (h webhookHandler) handle(provider domain.CodeSourceProvider) echo.HandlerFunc {
	return func(c echo.Context) error {
		if h.service == nil {
			return codeSourceUnavailable(c)
		}

		body, err := io.ReadAll(io.LimitReader(c.Request().Body, maxWebhookBodyBytes+1))
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid webhook payload"})
		}
		if len(body) > maxWebhookBodyBytes {
			return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "webhook payload too large"})
		}

		headers := make(map[string]string, len(c.Request().Header))
		for key, values := range c.Request().Header {
			if len(values) > 0 {
				headers[textproto.CanonicalMIMEHeaderKey(key)] = values[0]
			}
		}

		result, err := h.service.HandleWebhook(c.Request().Context(), domain.WebhookRequest{
			Provider:     provider,
			ConnectionID: c.Param("connectionId"),
			Headers:      headers,
			Body:         body,
		})
		if err != nil {
			return webhookErrorResponse(c, err)
		}
		if !result.Handled {
			return c.NoContent(http.StatusNoContent)
		}
		return c.NoContent(http.StatusAccepted)
	}
}

func webhookErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrCodeSourceWebhookUnauthorized):
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid webhook signature"})
	case errors.Is(err, domain.ErrInvalidCodeSourceWebhookPayload):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid webhook payload"})
	case errors.Is(err, domain.ErrCodeSourceConnectionNotFound), errors.Is(err, domain.ErrCodeSourceProviderNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "code source connection not found"})
	case errors.Is(err, domain.ErrCodeSourceAuthNotConfigured):
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "code source webhook not configured"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}
