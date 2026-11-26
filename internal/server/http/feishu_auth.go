package httpserver

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

const (
	feishuOAuthStateCookieName = "forge_feishu_oauth_state"
	// feishuOAuthTenantCookieName 记录发起授权的租户，回调时用同一个租户的应用换取 token。
	feishuOAuthTenantCookieName = "forge_feishu_oauth_tenant"
	// feishuOAuthRedirectCookieName 记录发起授权时的回调地址，换取 token 时飞书要求与之一致。
	feishuOAuthRedirectCookieName = "forge_feishu_oauth_redirect"
	feishuOAuthStateMaxAge        = 5 * time.Minute
	feishuSessionCookieName       = "forge_console_session"
)

type feishuAuthHandler struct {
	service FeishuAuthService
	tenants TenantService
}

type authSessionResponse struct {
	Capabilities domain.TenantCapabilities `json:"capabilities"`
	Roles        []domain.Role             `json:"roles"`
	User         sessionUserResponse       `json:"user"`
	// HomeTenant 是用户所属租户，ActiveTenant 是当前所在租户；只有 SuperAdmin 切换后两者才不同。
	HomeTenant   tenantSummaryResponse `json:"homeTenant"`
	ActiveTenant tenantSummaryResponse `json:"activeTenant"`
	Token        sessionTokenResponse  `json:"token"`
}

// tenantSummaryResponse 是登录页和会话里可见的租户信息，不含飞书应用凭证。
type tenantSummaryResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type loginTenantsResponse struct {
	Tenants []tenantSummaryResponse `json:"tenants"`
}

type sessionUserResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatarUrl"`
}

type sessionTokenResponse struct {
	ExpiresIn             int    `json:"expiresIn"`
	RefreshTokenExpiresIn int    `json:"refreshTokenExpiresIn,omitempty"`
	TokenType             string `json:"tokenType"`
	Scope                 string `json:"scope"`
	HasRefreshToken       bool   `json:"hasRefreshToken"`
}

type switchTenantRequest struct {
	TenantID string `json:"tenantId"`
}

func registerFeishuAuthRoutes(group *echo.Group, service FeishuAuthService, tenants TenantService) {
	handler := feishuAuthHandler{service: service, tenants: tenants}

	group.GET("/auth/tenants", handler.loginTenants)
	group.GET("/auth/feishu/authorize", handler.authorize)
	group.GET("/auth/feishu/callback", handler.callback)
	group.GET("/auth/session", handler.session)
	group.PUT("/auth/session/tenant", handler.switchTenant)
	group.POST("/auth/logout", handler.logout)
}

// loginTenants 返回登录页可选的租户，不需要登录。
func (h feishuAuthHandler) loginTenants(c echo.Context) error {
	if h.tenants == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth unavailable"})
	}
	tenants, err := h.tenants.ListActiveTenants(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	response := loginTenantsResponse{Tenants: make([]tenantSummaryResponse, 0, len(tenants))}
	for _, tenant := range tenants {
		response.Tenants = append(response.Tenants, tenantSummaryResponse{ID: tenant.ID, Name: tenant.Name})
	}
	return c.JSON(http.StatusOK, response)
}

func (h feishuAuthHandler) authorize(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth unavailable"})
	}

	state, err := randomOAuthState()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	redirectURI := c.QueryParam("redirect")
	authURL, tenantID, err := h.service.AuthorizationURL(c.Request().Context(), c.QueryParam("tenant"), redirectURI, state)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidFeishuRedirectURI):
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid redirect uri"})
		case errors.Is(err, domain.ErrFeishuAuthNotConfigured):
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth not configured"})
		case errors.Is(err, domain.ErrTenantRequired):
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "tenant required"})
		case errors.Is(err, domain.ErrTenantNotFound):
			return c.JSON(http.StatusNotFound, map[string]string{"error": "tenant not found"})
		}
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "feishu authorization failed"})
	}

	setFeishuStateCookie(c, feishuOAuthStateCookieName, state)
	setFeishuStateCookie(c, feishuOAuthTenantCookieName, tenantID)
	setFeishuStateCookie(c, feishuOAuthRedirectCookieName, redirectURI)
	return c.Redirect(http.StatusFound, authURL)
}

func (h feishuAuthHandler) callback(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth unavailable"})
	}

	tenantID, redirectURI := "", ""
	if cookie, err := c.Cookie(feishuOAuthTenantCookieName); err == nil {
		tenantID = cookie.Value
	}
	if cookie, err := c.Cookie(feishuOAuthRedirectCookieName); err == nil {
		redirectURI = cookie.Value
	}
	clearFeishuStateCookie(c, feishuOAuthStateCookieName)
	clearFeishuStateCookie(c, feishuOAuthTenantCookieName)
	clearFeishuStateCookie(c, feishuOAuthRedirectCookieName)

	if denied := c.QueryParam("error"); denied != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "feishu authorization denied"})
	}

	code := c.QueryParam("code")
	state := c.QueryParam("state")
	if code == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing authorization code"})
	}
	if !validFeishuOAuthState(c, state) || tenantID == "" || redirectURI == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid oauth state"})
	}

	result, err := h.service.CompleteAuthorization(c.Request().Context(), tenantID, redirectURI, code)
	if err != nil {
		if errors.Is(err, domain.ErrTenantNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "tenant not found"})
		}
		if errors.Is(err, domain.ErrFeishuAuthNotConfigured) {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth not configured"})
		}
		if errors.Is(err, domain.ErrFeishuAuthorization) {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "feishu authorization failed"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	setFeishuSessionCookie(c, result)
	return c.JSON(http.StatusOK, newAuthSessionResponse(result))
}

func (h feishuAuthHandler) session(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth unavailable"})
	}

	sessionToken, ok := feishuSessionToken(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	result, err := h.service.GetSession(c.Request().Context(), sessionToken)
	if err != nil {
		if errors.Is(err, domain.ErrSessionNotFound) ||
			errors.Is(err, domain.ErrSessionExpired) ||
			errors.Is(err, domain.ErrFeishuAuthorization) {
			clearFeishuSessionCookie(c)
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
		if errors.Is(err, domain.ErrFeishuAuthNotConfigured) {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth not configured"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	setFeishuSessionCookie(c, result)
	return c.JSON(http.StatusOK, newAuthSessionResponse(result))
}

func (h feishuAuthHandler) logout(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth unavailable"})
	}

	if sessionToken, ok := feishuSessionToken(c); ok {
		if err := h.service.Logout(c.Request().Context(), sessionToken); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
	}
	clearFeishuSessionCookie(c)
	return c.NoContent(http.StatusNoContent)
}

// switchTenant 把当前会话切换到另一个租户，只有 SuperAdmin 可以切到所属租户以外的租户。
func (h feishuAuthHandler) switchTenant(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth unavailable"})
	}
	sessionToken, ok := feishuSessionToken(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	var request switchTenantRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	result, err := h.service.SwitchTenant(c.Request().Context(), sessionToken, request.TenantID)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrTenantSwitchForbidden):
			return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
		case errors.Is(err, domain.ErrTenantNotFound):
			return c.JSON(http.StatusNotFound, map[string]string{"error": "tenant not found"})
		}
		return consoleSessionErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newAuthSessionResponse(result))
}

func newAuthSessionResponse(result domain.ConsoleSession) authSessionResponse {
	return authSessionResponse{
		Capabilities: result.Capabilities,
		Roles:        result.Roles,
		User: sessionUserResponse{
			ID:        result.User.ID,
			Name:      result.User.Name,
			Email:     result.User.Email,
			AvatarURL: result.User.AvatarURL,
		},
		HomeTenant:   tenantSummaryResponse{ID: result.HomeTenant.ID, Name: result.HomeTenant.Name},
		ActiveTenant: tenantSummaryResponse{ID: result.ActiveTenant.ID, Name: result.ActiveTenant.Name},
		Token: sessionTokenResponse{
			ExpiresIn:             result.ProviderToken.ExpiresIn,
			RefreshTokenExpiresIn: result.ProviderToken.RefreshTokenExpiresIn,
			TokenType:             result.ProviderToken.TokenType,
			Scope:                 result.ProviderToken.Scope,
			HasRefreshToken:       result.ProviderToken.RefreshToken != "",
		},
	}
}

func randomOAuthState() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

func setFeishuStateCookie(c echo.Context, name string, value string) {
	c.SetCookie(&http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/api/v1/auth/feishu",
		MaxAge:   int(feishuOAuthStateMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearFeishuStateCookie(c echo.Context, name string) {
	c.SetCookie(&http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/api/v1/auth/feishu",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func setFeishuSessionCookie(c echo.Context, result domain.ConsoleSession) {
	if result.SessionToken == "" {
		return
	}

	maxAge := int(time.Until(result.ExpiresAt).Seconds())
	if maxAge <= 0 {
		return
	}

	c.SetCookie(&http.Cookie{
		Name:     feishuSessionCookieName,
		Value:    result.SessionToken,
		Path:     "/api/v1",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearFeishuSessionCookie(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     feishuSessionCookieName,
		Value:    "",
		Path:     "/api/v1",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func feishuSessionToken(c echo.Context) (string, bool) {
	cookie, err := c.Cookie(feishuSessionCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return cookie.Value, true
}

func validFeishuOAuthState(c echo.Context, state string) bool {
	if state == "" {
		return false
	}

	cookie, err := c.Cookie(feishuOAuthStateCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) == 1
}
