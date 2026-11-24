package httpserver

import (
	"context"
	"errors"
	"net/http"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type ReleaseChannelService interface {
	ListReleaseChannels(ctx context.Context, applicationSlug string) ([]domain.ReleaseChannel, error)
	CreateReleaseChannel(ctx context.Context, applicationSlug string, input domain.ReleaseChannelInput) (domain.ReleaseChannel, error)
	UpdateReleaseChannel(ctx context.Context, applicationSlug string, code string, input domain.ReleaseChannelInput) (domain.ReleaseChannel, error)
	DeleteReleaseChannel(ctx context.Context, applicationSlug string, code string) error
}

type releaseChannelHandler struct {
	authService AuthorizationService
	service     ReleaseChannelService
}

type releaseChannelRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type releaseChannelResponse struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	ActiveReleases int    `json:"activeReleases"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

type releaseChannelsResponse struct {
	Channels []releaseChannelResponse `json:"channels"`
}

func registerReleaseChannelRoutes(group *echo.Group, service ReleaseChannelService, authService AuthorizationService) {
	handler := releaseChannelHandler{authService: authService, service: service}

	group.GET("/applications/:slug/channels", handler.list)
	group.POST("/applications/:slug/channels", handler.create)
	group.PATCH("/applications/:slug/channels/:code", handler.update)
	group.DELETE("/applications/:slug/channels/:code", handler.delete)
}

func (h releaseChannelHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release channel service unavailable"})
	}
	channels, err := h.service.ListReleaseChannels(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return releaseChannelErrorResponse(c, err)
	}
	response := releaseChannelsResponse{Channels: make([]releaseChannelResponse, 0, len(channels))}
	for _, channel := range channels {
		response.Channels = append(response.Channels, newReleaseChannelResponse(channel))
	}
	return c.JSON(http.StatusOK, response)
}

func (h releaseChannelHandler) create(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release channel service unavailable"})
	}
	var request releaseChannelRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid release channel request"})
	}
	actor, err := h.channelActor(c)
	if err != nil {
		return err
	}
	channel, err := h.service.CreateReleaseChannel(c.Request().Context(), c.Param("slug"), domain.ReleaseChannelInput{
		Code: request.Code, Name: request.Name, Description: request.Description, CreatedByUserID: actor,
	})
	if err != nil {
		return releaseChannelErrorResponse(c, err)
	}
	return c.JSON(http.StatusCreated, newReleaseChannelResponse(channel))
}

func (h releaseChannelHandler) update(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release channel service unavailable"})
	}
	var request releaseChannelRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid release channel request"})
	}
	if _, err := h.channelActor(c); err != nil {
		return err
	}
	channel, err := h.service.UpdateReleaseChannel(c.Request().Context(), c.Param("slug"), c.Param("code"), domain.ReleaseChannelInput{
		Name: request.Name, Description: request.Description,
	})
	if err != nil {
		return releaseChannelErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newReleaseChannelResponse(channel))
}

func (h releaseChannelHandler) delete(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "release channel service unavailable"})
	}
	if _, err := h.channelActor(c); err != nil {
		return err
	}
	if err := h.service.DeleteReleaseChannel(c.Request().Context(), c.Param("slug"), c.Param("code")); err != nil {
		return releaseChannelErrorResponse(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// channelActor 校验当前用户能在该应用发布（渠道管理与发布共用 release:create），返回操作人。
func (h releaseChannelHandler) channelActor(c echo.Context) (string, error) {
	if err := authorizeCurrentUser(c, h.authService, domain.ActionReleaseCreate, domain.AuthorizationResource{ApplicationSlug: c.Param("slug")}); err != nil {
		return "", err
	}
	session, ok := consoleSession(c)
	if !ok {
		return "", c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	actor := domain.ConsoleUserIDFromFeishu(session.User)
	if actor == "" {
		return "", c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	return actor, nil
}

func newReleaseChannelResponse(channel domain.ReleaseChannel) releaseChannelResponse {
	return releaseChannelResponse{
		Code:           channel.Code,
		Name:           channel.Name,
		Description:    channel.Description,
		ActiveReleases: channel.ActiveReleases,
		CreatedAt:      formatTime(channel.CreatedAt),
		UpdatedAt:      formatTime(channel.UpdatedAt),
	}
}

func releaseChannelErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidReleaseChannelInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid release channel request"})
	case errors.Is(err, domain.ErrApplicationNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "application not found"})
	case errors.Is(err, domain.ErrReleaseChannelNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "release channel not found"})
	case errors.Is(err, domain.ErrReleaseChannelExists):
		return c.JSON(http.StatusConflict, map[string]string{"error": "release channel already exists"})
	case errors.Is(err, domain.ErrReleaseChannelInUse):
		return c.JSON(http.StatusConflict, map[string]string{"error": "release channel in use"})
	case errors.Is(err, domain.ErrForbidden):
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}
