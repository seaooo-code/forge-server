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
	feishuOAuthStateMaxAge      = 5 * time.Minute
	feishuSessionCookieName     = "forge_console_session"
)

type feishuAuthHandler struct {
	service FeishuAuthService
	tenants FeishuTenantService
}

type feishuAuthResponse struct {
	Capabilities domain.GlobalCapabilities `json:"capabilities"`
	Roles        []domain.GlobalRole       `json:"roles"`
	Tenant       feishuLoginTenantResponse `json:"tenant"`
	User         feishuUserResponse        `json:"user"`
	Token        feishuTokenResponse       `json:"token"`
}

// feishuLoginTenantResponse 是登录页可见的租户信息，不含 App ID 和密钥。
type feishuLoginTenantResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type feishuLoginTenantsResponse struct {
	Tenants []feishuLoginTenantResponse `json:"tenants"`
}

type feishuUserResponse struct {
	ID              string `json:"id"`
	UnionID         string `json:"unionId"`
	OpenID          string `json:"openId"`
	UserID          string `json:"userId"`
	Name            string `json:"name"`
	EnName          string `json:"enName"`
	AvatarURL       string `json:"avatarUrl"`
	AvatarThumb     string `json:"avatarThumb"`
	AvatarMiddle    string `json:"avatarMiddle"`
	AvatarBig       string `json:"avatarBig"`
	Email           string `json:"email"`
	EnterpriseEmail string `json:"enterpriseEmail"`
	Mobile          string `json:"mobile"`
	TenantKey       string `json:"tenantKey"`
	EmployeeNo      string `json:"employeeNo"`
}

type feishuTokenResponse struct {
	ExpiresIn             int    `json:"expiresIn"`
	RefreshTokenExpiresIn int    `json:"refreshTokenExpiresIn,omitempty"`
	TokenType             string `json:"tokenType"`
	Scope                 string `json:"scope"`
	HasRefreshToken       bool   `json:"hasRefreshToken"`
}

func registerFeishuAuthRoutes(group *echo.Group, service FeishuAuthService, tenants FeishuTenantService) {
	handler := feishuAuthHandler{service: service, tenants: tenants}

	group.GET("/auth/feishu/tenants", handler.loginTenants)
	group.GET("/auth/feishu/authorize", handler.authorize)
	group.GET("/auth/feishu/callback", handler.callback)
	group.GET("/auth/session", handler.session)
	group.POST("/auth/logout", handler.logout)
}

// loginTenants 返回登录页可选的租户，不需要登录。
func (h feishuAuthHandler) loginTenants(c echo.Context) error {
	if h.tenants == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth unavailable"})
	}
	tenants, err := h.tenants.ListActiveFeishuTenants(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	response := feishuLoginTenantsResponse{Tenants: make([]feishuLoginTenantResponse, 0, len(tenants))}
	for _, tenant := range tenants {
		response.Tenants = append(response.Tenants, feishuLoginTenantResponse{ID: tenant.ID, Name: tenant.Name})
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

	authURL, tenantID, err := h.service.AuthorizationURL(c.Request().Context(), c.QueryParam("tenant"), state)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrFeishuAuthNotConfigured):
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth not configured"})
		case errors.Is(err, domain.ErrFeishuTenantRequired):
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "feishu tenant required"})
		case errors.Is(err, domain.ErrFeishuTenantNotFound):
			return c.JSON(http.StatusNotFound, map[string]string{"error": "feishu tenant not found"})
		}
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "feishu authorization failed"})
	}

	setFeishuStateCookie(c, feishuOAuthStateCookieName, state)
	setFeishuStateCookie(c, feishuOAuthTenantCookieName, tenantID)
	return c.Redirect(http.StatusFound, authURL)
}

func (h feishuAuthHandler) callback(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth unavailable"})
	}

	tenantID := ""
	if cookie, err := c.Cookie(feishuOAuthTenantCookieName); err == nil {
		tenantID = cookie.Value
	}
	clearFeishuStateCookie(c, feishuOAuthStateCookieName)
	clearFeishuStateCookie(c, feishuOAuthTenantCookieName)

	if denied := c.QueryParam("error"); denied != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "feishu authorization denied"})
	}

	code := c.QueryParam("code")
	state := c.QueryParam("state")
	if code == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing authorization code"})
	}
	if !validFeishuOAuthState(c, state) || tenantID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid oauth state"})
	}

	result, err := h.service.CompleteAuthorization(c.Request().Context(), tenantID, code)
	if err != nil {
		if errors.Is(err, domain.ErrFeishuTenantNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "feishu tenant not found"})
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
	return c.JSON(http.StatusOK, newFeishuAuthResponse(result))
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
		if errors.Is(err, domain.ErrFeishuSessionNotFound) ||
			errors.Is(err, domain.ErrFeishuSessionExpired) ||
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
	return c.JSON(http.StatusOK, newFeishuAuthResponse(result))
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

func newFeishuAuthResponse(result domain.FeishuAuthResult) feishuAuthResponse {
	return feishuAuthResponse{
		Capabilities: result.Capabilities,
		Roles:        result.Roles,
		Tenant:       feishuLoginTenantResponse{ID: result.Tenant.ID, Name: result.Tenant.Name},
		User: feishuUserResponse{
			ID:              domain.ConsoleUserIDFromFeishu(result.User),
			UnionID:         result.User.UnionID,
			OpenID:          result.User.OpenID,
			UserID:          result.User.UserID,
			Name:            result.User.Name,
			EnName:          result.User.EnName,
			AvatarURL:       result.User.AvatarURL,
			AvatarThumb:     result.User.AvatarThumb,
			AvatarMiddle:    result.User.AvatarMiddle,
			AvatarBig:       result.User.AvatarBig,
			Email:           result.User.Email,
			EnterpriseEmail: result.User.EnterpriseEmail,
			Mobile:          result.User.Mobile,
			TenantKey:       result.User.TenantKey,
			EmployeeNo:      result.User.EmployeeNo,
		},
		Token: feishuTokenResponse{
			ExpiresIn:             result.Token.ExpiresIn,
			RefreshTokenExpiresIn: result.Token.RefreshTokenExpiresIn,
			TokenType:             result.Token.TokenType,
			Scope:                 result.Token.Scope,
			HasRefreshToken:       result.Token.RefreshToken != "",
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

func setFeishuSessionCookie(c echo.Context, result domain.FeishuAuthResult) {
	if result.SessionToken == "" {
		return
	}

	maxAge := int(time.Until(result.SessionExpiresAt).Seconds())
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
