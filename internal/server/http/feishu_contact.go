package httpserver

import (
	"errors"
	"net/http"
	"strconv"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type feishuContactHandler struct {
	authService FeishuAuthService
	service     FeishuContactService
}

type feishuUserSearchResponse struct {
	Users     []feishuUserSearchItemResponse `json:"users"`
	HasMore   bool                           `json:"hasMore"`
	PageToken string                         `json:"pageToken"`
}

type feishuErrorResponse struct {
	Error          string `json:"error"`
	FeishuCode     int    `json:"feishuCode,omitempty"`
	FeishuEndpoint string `json:"feishuEndpoint,omitempty"`
	FeishuMsg      string `json:"feishuMsg,omitempty"`
}

type feishuUserSearchItemResponse struct {
	UnionID       string   `json:"unionId"`
	OpenID        string   `json:"openId"`
	UserID        string   `json:"userId"`
	Name          string   `json:"name"`
	EnName        string   `json:"enName"`
	AvatarURL     string   `json:"avatarUrl"`
	Email         string   `json:"email"`
	Mobile        string   `json:"mobile"`
	DepartmentIDs []string `json:"departmentIds"`
}

func registerFeishuContactRoutes(group *echo.Group, service FeishuContactService, authService FeishuAuthService) {
	handler := feishuContactHandler{
		authService: authService,
		service:     service,
	}

	group.GET("/feishu/users/search", handler.searchUsers)
}

func (h feishuContactHandler) searchUsers(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu contact unavailable"})
	}
	if h.authService == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth unavailable"})
	}

	sessionToken, ok := feishuSessionToken(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	session, err := h.authService.GetSession(c.Request().Context(), sessionToken)
	if err != nil {
		if errors.Is(err, domain.ErrFeishuSessionNotFound) || errors.Is(err, domain.ErrFeishuSessionExpired) {
			clearFeishuSessionCookie(c)
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
		if errors.Is(err, domain.ErrFeishuAuthNotConfigured) {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth not configured"})
		}
		if errors.Is(err, domain.ErrFeishuAuthorization) {
			return c.JSON(http.StatusBadGateway, newFeishuErrorResponse(err))
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	setFeishuSessionCookie(c, session)

	pageSize := 20
	if rawPageSize := c.QueryParam("page_size"); rawPageSize != "" {
		parsed, err := strconv.Atoi(rawPageSize)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid feishu search request"})
		}
		if parsed < 1 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid feishu search request"})
		}
		pageSize = parsed
	}
	pageToken := c.QueryParam("page_token")

	page, err := h.service.SearchUsers(c.Request().Context(), session.Token.AccessToken, c.QueryParam("query"), pageSize, pageToken)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidFeishuSearch) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid feishu search request"})
		}
		if errors.Is(err, domain.ErrFeishuSessionExpired) {
			clearFeishuSessionCookie(c)
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
		if errors.Is(err, domain.ErrFeishuAuthNotConfigured) {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth not configured"})
		}
		if errors.Is(err, domain.ErrFeishuAuthorization) {
			return c.JSON(http.StatusBadGateway, newFeishuErrorResponse(err))
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	return c.JSON(http.StatusOK, feishuUserSearchResponse{
		Users:     newFeishuUserSearchItemResponses(page.Users),
		HasMore:   page.HasMore,
		PageToken: page.PageToken,
	})
}

func newFeishuErrorResponse(err error) feishuErrorResponse {
	response := feishuErrorResponse{
		Error: domain.ErrFeishuAuthorization.Error(),
	}

	var feishuErr *domain.FeishuAPIError
	if errors.As(err, &feishuErr) {
		response.FeishuCode = feishuErr.Code
		response.FeishuEndpoint = feishuErr.Endpoint
		response.FeishuMsg = feishuErr.Msg
	}

	return response
}

func newFeishuUserSearchItemResponses(users []domain.FeishuUserSearchResult) []feishuUserSearchItemResponse {
	response := make([]feishuUserSearchItemResponse, 0, len(users))
	for _, user := range users {
		response = append(response, feishuUserSearchItemResponse{
			UnionID:       user.UnionID,
			OpenID:        user.OpenID,
			UserID:        user.UserID,
			Name:          user.Name,
			EnName:        user.EnName,
			AvatarURL:     user.AvatarURL,
			Email:         user.Email,
			Mobile:        user.Mobile,
			DepartmentIDs: user.DepartmentIDs,
		})
	}
	return response
}
