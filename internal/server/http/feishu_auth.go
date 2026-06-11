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
	feishuOAuthStateMaxAge     = 5 * time.Minute
	feishuSessionCookieName    = "forge_console_session"
)

type feishuAuthHandler struct {
	service FeishuAuthService
}

type feishuAuthResponse struct {
	User  feishuUserResponse  `json:"user"`
	Token feishuTokenResponse `json:"token"`
}

type feishuUserResponse struct {
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

func registerFeishuAuthRoutes(group *echo.Group, service FeishuAuthService) {
	handler := feishuAuthHandler{service: service}

	group.GET("/auth/feishu/authorize", handler.authorize)
	group.GET("/auth/feishu/callback", handler.callback)
	group.GET("/auth/session", handler.session)
	group.POST("/auth/logout", handler.logout)
}

func (h feishuAuthHandler) authorize(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth unavailable"})
	}

	state, err := randomOAuthState()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	authURL, err := h.service.AuthorizationURL(state)
	if err != nil {
		if errors.Is(err, domain.ErrFeishuAuthNotConfigured) {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth not configured"})
		}
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "feishu authorization failed"})
	}

	setFeishuStateCookie(c, state)
	return c.Redirect(http.StatusFound, authURL)
}

func (h feishuAuthHandler) callback(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth unavailable"})
	}

	clearFeishuStateCookie(c)

	if denied := c.QueryParam("error"); denied != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "feishu authorization denied"})
	}

	code := c.QueryParam("code")
	state := c.QueryParam("state")
	if code == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing authorization code"})
	}
	if !validFeishuOAuthState(c, state) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid oauth state"})
	}

	result, err := h.service.CompleteAuthorization(c.Request().Context(), code)
	if err != nil {
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
		User: feishuUserResponse{
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

func setFeishuStateCookie(c echo.Context, state string) {
	c.SetCookie(&http.Cookie{
		Name:     feishuOAuthStateCookieName,
		Value:    state,
		Path:     "/api/v1/auth/feishu",
		MaxAge:   int(feishuOAuthStateMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearFeishuStateCookie(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     feishuOAuthStateCookieName,
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
