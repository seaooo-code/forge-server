package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

const consoleSessionContextKey = "forge_console_session_context"

// errResponseWritten 表示鉴权辅助函数已经写出错误响应，调用方必须立即返回、不能继续执行业务逻辑。
// c.JSON 写出响应后返回 nil，如果直接把它当作错误返回值，调用方会误以为鉴权通过。
var errResponseWritten = errors.New("error response already written")

func writeErrorResponse(c echo.Context, status int, message string) error {
	if err := c.JSON(status, map[string]string{"error": message}); err != nil {
		return err
	}
	return errResponseWritten
}

// swallowWrittenResponseErrors 让已写出响应的 errResponseWritten 不再进入 Echo 的错误处理与日志。
func swallowWrittenResponseErrors(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if err := next(c); !errors.Is(err, errResponseWritten) {
			return err
		}
		return nil
	}
}

func requireConsoleSession(service FeishuAuthService) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if service == nil {
				return writeErrorResponse(c, http.StatusServiceUnavailable, "feishu auth unavailable")
			}

			session, err := resolveConsoleSession(c, service)
			if err != nil {
				return err
			}
			setConsoleSession(c, session)
			return next(c)
		}
	}
}

// setConsoleSession 保存会话，并把当前租户放进请求 context，之后的查询只能访问这个租户的数据。
func setConsoleSession(c echo.Context, session domain.ConsoleSession) {
	c.Set(consoleSessionContextKey, session)
	c.SetRequest(c.Request().WithContext(domain.WithTenant(c.Request().Context(), session.ActiveTenant.ID)))
}

// setArtifactTokenTenant 把令牌所属租户放进请求 context，令牌只能访问本租户的应用。
func setArtifactTokenTenant(c echo.Context, tenantID string) {
	c.SetRequest(c.Request().WithContext(domain.WithTenant(c.Request().Context(), tenantID)))
}

func requireArtifactToken(tokenService ArtifactAccessTokenService) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token := bearerToken(c.Request().Header.Get(echo.HeaderAuthorization))
			if token == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}

			result, err := validateArtifactBearerToken(c.Request().Context(), tokenService, token)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
			if !result.Configured {
				return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "artifact auth not configured"})
			}
			if !result.Allowed {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}
			setArtifactTokenTenant(c, result.TenantID)
			return next(c)
		}
	}
}

func requireArtifactAccess(service FeishuAuthService, tokenService ArtifactAccessTokenService) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if token := bearerToken(c.Request().Header.Get(echo.HeaderAuthorization)); token != "" {
				result, err := validateArtifactBearerToken(c.Request().Context(), tokenService, token)
				if err != nil {
					return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
				}
				if result.Allowed {
					setArtifactTokenTenant(c, result.TenantID)
					return next(c)
				}
			}
			if service != nil {
				session, err := resolveConsoleSession(c, service)
				if err != nil {
					return err
				}
				setConsoleSession(c, session)
				return next(c)
			}
			return writeErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		}
	}
}

func resolveConsoleSession(c echo.Context, service FeishuAuthService) (domain.ConsoleSession, error) {
	sessionToken, ok := feishuSessionToken(c)
	if !ok {
		return domain.ConsoleSession{}, writeErrorResponse(c, http.StatusUnauthorized, "unauthorized")
	}

	session, err := service.GetSession(c.Request().Context(), sessionToken)
	if err != nil {
		return domain.ConsoleSession{}, consoleSessionErrorResponse(c, err)
	}
	setFeishuSessionCookie(c, session)
	return session, nil
}

func consoleSession(c echo.Context) (domain.ConsoleSession, bool) {
	session, ok := c.Get(consoleSessionContextKey).(domain.ConsoleSession)
	return session, ok
}

func authorizeCurrentUser(c echo.Context, service AuthorizationService, action domain.AuthorizationAction, resource domain.AuthorizationResource) error {
	if service == nil {
		return writeErrorResponse(c, http.StatusForbidden, "forbidden")
	}
	session, ok := consoleSession(c)
	if !ok {
		return writeErrorResponse(c, http.StatusUnauthorized, "unauthorized")
	}
	if err := service.Authorize(c.Request().Context(), session.User.ID, action, resource); err != nil {
		if errors.Is(err, domain.ErrForbidden) {
			return writeErrorResponse(c, http.StatusForbidden, "forbidden")
		}
		if errors.Is(err, domain.ErrNotFound) {
			return writeErrorResponse(c, http.StatusNotFound, "not found")
		}
		return writeErrorResponse(c, http.StatusInternalServerError, "internal server error")
	}
	return nil
}

func consoleSessionErrorResponse(c echo.Context, err error) error {
	if errors.Is(err, domain.ErrSessionNotFound) ||
		errors.Is(err, domain.ErrSessionExpired) ||
		errors.Is(err, domain.ErrFeishuAuthorization) {
		clearFeishuSessionCookie(c)
		return writeErrorResponse(c, http.StatusUnauthorized, "unauthorized")
	}
	if errors.Is(err, domain.ErrFeishuAuthNotConfigured) {
		return writeErrorResponse(c, http.StatusServiceUnavailable, "feishu auth not configured")
	}
	return writeErrorResponse(c, http.StatusInternalServerError, "internal server error")
}

type artifactBearerTokenResult struct {
	Allowed    bool
	Configured bool
	TenantID   string
}

func validateArtifactBearerToken(ctx context.Context, tokenService ArtifactAccessTokenService, token string) (artifactBearerTokenResult, error) {
	result := artifactBearerTokenResult{Configured: tokenService != nil}
	if tokenService != nil {
		// 令牌查找发生在确定租户之前，用系统范围；查到后由调用方切到令牌所属租户。
		validation, err := tokenService.ValidateArtifactAccessToken(domain.WithSystemScope(ctx), token)
		if err != nil {
			return artifactBearerTokenResult{}, err
		}
		if validation.Matched {
			result.Allowed = validation.Allowed
			result.TenantID = validation.TenantID
			return result, nil
		}
	}
	return result, nil
}

func bearerToken(header string) string {
	fields := strings.Fields(strings.TrimSpace(header))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return ""
	}
	return fields[1]
}
