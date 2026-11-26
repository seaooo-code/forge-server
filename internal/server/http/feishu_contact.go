package httpserver

import (
	"errors"
	"net/http"
	"strconv"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

// 选人接口：搜索飞书通讯录、把选中的人写成当前租户的用户、搜索当前租户已有的用户。
// 表单里的负责人、成员字段只传 Forge 用户 ID。
type feishuContactHandler struct {
	service FeishuContactService
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
	UnionID   string `json:"unionId"`
	OpenID    string `json:"openId"`
	UserID    string `json:"userId"`
	Name      string `json:"name"`
	EnName    string `json:"enName"`
	AvatarURL string `json:"avatarUrl"`
	Email     string `json:"email"`
}

type userResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatarUrl"`
}

type userDetailResponse struct {
	User userResponse `json:"user"`
}

type usersResponse struct {
	Users []userResponse `json:"users"`
}

func registerFeishuContactRoutes(group *echo.Group, service FeishuContactService) {
	handler := feishuContactHandler{service: service}

	group.GET("/feishu/users/search", handler.searchFeishuUsers)
	group.POST("/feishu/users", handler.resolveFeishuUser)
	group.GET("/users", handler.searchUsers)
}

func (h feishuContactHandler) searchFeishuUsers(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu contact unavailable"})
	}
	session, ok := consoleSession(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	pageSize := 20
	if rawPageSize := c.QueryParam("page_size"); rawPageSize != "" {
		parsed, err := strconv.Atoi(rawPageSize)
		if err != nil || parsed < 1 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid feishu search request"})
		}
		pageSize = parsed
	}

	page, err := h.service.SearchUsers(c.Request().Context(), session, c.QueryParam("query"), pageSize, c.QueryParam("page_token"))
	if err != nil {
		return feishuContactErrorResponse(c, err)
	}
	users := make([]feishuUserSearchItemResponse, 0, len(page.Users))
	for _, user := range page.Users {
		users = append(users, feishuUserSearchItemResponse{
			UnionID:   user.UnionID,
			OpenID:    user.OpenID,
			UserID:    user.UserID,
			Name:      user.Name,
			EnName:    user.EnName,
			AvatarURL: user.AvatarURL,
			Email:     user.Email,
		})
	}
	return c.JSON(http.StatusOK, feishuUserSearchResponse{Users: users, HasMore: page.HasMore, PageToken: page.PageToken})
}

// resolveFeishuUser 把通讯录搜索结果里的一个人写成当前租户的用户，返回 Forge 用户 ID。
func (h feishuContactHandler) resolveFeishuUser(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu contact unavailable"})
	}
	session, ok := consoleSession(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	var request feishuUserSearchItemResponse
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid feishu user"})
	}

	user, err := h.service.ResolveUser(c.Request().Context(), session, domain.FeishuUserSearchResult{
		UnionID:   request.UnionID,
		OpenID:    request.OpenID,
		UserID:    request.UserID,
		Name:      request.Name,
		EnName:    request.EnName,
		AvatarURL: request.AvatarURL,
		Email:     request.Email,
	})
	if err != nil {
		if errors.Is(err, domain.ErrInvalidFeishuSearch) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid feishu user"})
		}
		return feishuContactErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, userDetailResponse{User: newUserResponse(user)})
}

// searchUsers 在当前租户已有的用户里搜索。
func (h feishuContactHandler) searchUsers(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "user search unavailable"})
	}
	limit := 20
	if rawLimit := c.QueryParam("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 50 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid user search request"})
		}
		limit = parsed
	}
	users, err := h.service.SearchLocalUsers(c.Request().Context(), c.QueryParam("query"), limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	response := usersResponse{Users: make([]userResponse, 0, len(users))}
	for _, user := range users {
		response.Users = append(response.Users, newUserResponse(user))
	}
	return c.JSON(http.StatusOK, response)
}

func feishuContactErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidFeishuSearch):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid feishu search request"})
	case errors.Is(err, domain.ErrForbidden):
		// SuperAdmin 进入其他租户时不能用自己的飞书 token 搜索对方通讯录。
		return c.JSON(http.StatusForbidden, map[string]string{"error": "feishu contact only available in home tenant"})
	case errors.Is(err, domain.ErrSessionExpired):
		clearFeishuSessionCookie(c)
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	case errors.Is(err, domain.ErrFeishuAuthNotConfigured):
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "feishu auth not configured"})
	case errors.Is(err, domain.ErrFeishuAuthorization):
		return c.JSON(http.StatusBadGateway, newFeishuErrorResponse(err))
	}
	return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
}

func newFeishuErrorResponse(err error) feishuErrorResponse {
	response := feishuErrorResponse{Error: domain.ErrFeishuAuthorization.Error()}
	var feishuErr *domain.FeishuAPIError
	if errors.As(err, &feishuErr) {
		response.FeishuCode = feishuErr.Code
		response.FeishuEndpoint = feishuErr.Endpoint
		response.FeishuMsg = feishuErr.Msg
	}
	return response
}

func newUserResponse(user domain.User) userResponse {
	return userResponse{ID: user.ID, Name: user.Name, Email: user.Email, AvatarURL: user.AvatarURL}
}
