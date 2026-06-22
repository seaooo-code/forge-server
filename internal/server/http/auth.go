package httpserver

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

const consoleSessionContextKey = "forge_console_session_context"

func requireConsoleSession(service FeishuAuthService) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if service == nil {
				return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth unavailable"})
			}

			session, err := resolveConsoleSession(c, service)
			if err != nil {
				return err
			}
			c.Set(consoleSessionContextKey, session)
			return next(c)
		}
	}
}

func requireArtifactToken(tokens []string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if len(tokens) == 0 {
				return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "artifact auth not configured"})
			}
			if !validArtifactBearerToken(c, tokens) {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}
			return next(c)
		}
	}
}

func requireArtifactAccess(service FeishuAuthService, tokens []string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if validArtifactBearerToken(c, tokens) {
				return next(c)
			}
			if service != nil {
				if session, err := resolveConsoleSession(c, service); err == nil {
					c.Set(consoleSessionContextKey, session)
					return next(c)
				}
			}
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
	}
}

func resolveConsoleSession(c echo.Context, service FeishuAuthService) (domain.FeishuAuthResult, error) {
	sessionToken, ok := feishuSessionToken(c)
	if !ok {
		return domain.FeishuAuthResult{}, c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	session, err := service.GetSession(c.Request().Context(), sessionToken)
	if err != nil {
		return domain.FeishuAuthResult{}, consoleSessionErrorResponse(c, err)
	}
	setFeishuSessionCookie(c, session)
	return session, nil
}

func consoleSession(c echo.Context) (domain.FeishuAuthResult, bool) {
	session, ok := c.Get(consoleSessionContextKey).(domain.FeishuAuthResult)
	return session, ok
}

func consoleSessionErrorResponse(c echo.Context, err error) error {
	if errors.Is(err, domain.ErrFeishuSessionNotFound) || errors.Is(err, domain.ErrFeishuSessionExpired) {
		clearFeishuSessionCookie(c)
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	if errors.Is(err, domain.ErrFeishuAuthNotConfigured) {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth not configured"})
	}
	if errors.Is(err, domain.ErrFeishuAuthorization) {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "feishu authorization failed"})
	}
	return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
}

func validArtifactBearerToken(c echo.Context, tokens []string) bool {
	token := bearerToken(c.Request().Header.Get(echo.HeaderAuthorization))
	if token == "" {
		return false
	}
	for _, configuredToken := range tokens {
		candidate := strings.TrimSpace(configuredToken)
		if candidate == "" || len(candidate) != len(token) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1 {
			return true
		}
	}
	return false
}

func bearerToken(header string) string {
	fields := strings.Fields(strings.TrimSpace(header))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return ""
	}
	return fields[1]
}
