package httpserver

import (
	"errors"
	"net/http"
	"time"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type artifactAccessTokenHandler struct {
	authService AuthorizationService
	service     ArtifactAccessTokenService
}

type artifactAccessTokensResponse struct {
	Tokens []artifactAccessTokenResponse `json:"tokens"`
}

type artifactAccessTokenDetailResponse struct {
	Token artifactAccessTokenResponse `json:"token"`
}

type artifactAccessTokenGenerateResponse struct {
	PlainToken string `json:"plainToken"`
}

type artifactAccessTokenResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	TokenPreview string `json:"tokenPreview"`
	Status       string `json:"status"`
	CreatedBy    string `json:"createdBy"`
	CreatedAt    string `json:"createdAt"`
	LastUsedAt   string `json:"lastUsedAt"`
	ExpiresAt    string `json:"expiresAt"`
}

type artifactAccessTokenCreateRequest struct {
	Name      string `json:"name"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

type artifactAccessTokenUpdateRequest struct {
	Status string `json:"status"`
}

func registerArtifactAccessTokenRoutes(group *echo.Group, service ArtifactAccessTokenService, authService AuthorizationService) {
	handler := artifactAccessTokenHandler{authService: authService, service: service}

	group.GET("/artifact-access-tokens", handler.list)
	group.POST("/artifact-access-tokens/generate", handler.generate)
	group.POST("/artifact-access-tokens", handler.create)
	group.PATCH("/artifact-access-tokens/:id", handler.update)
	group.DELETE("/artifact-access-tokens/:id", handler.delete)
}

func (h artifactAccessTokenHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "artifact access token service unavailable"})
	}
	tokens, err := h.service.ListArtifactAccessTokens(c.Request().Context())
	if err != nil {
		return artifactAccessTokenErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, artifactAccessTokensResponse{Tokens: newArtifactAccessTokenResponses(tokens)})
}

func (h artifactAccessTokenHandler) generate(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "artifact access token service unavailable"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionAccessTokenManage, domain.AuthorizationResource{}); err != nil {
		return err
	}
	result, err := h.service.GenerateArtifactAccessToken(c.Request().Context())
	if err != nil {
		return artifactAccessTokenErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, artifactAccessTokenGenerateResponse{PlainToken: result.PlainToken})
}

func (h artifactAccessTokenHandler) create(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "artifact access token service unavailable"})
	}

	var request artifactAccessTokenCreateRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid artifact access token request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionAccessTokenManage, domain.AuthorizationResource{}); err != nil {
		return err
	}

	createdByUserID := ""
	if session, ok := consoleSession(c); ok {
		createdByUserID = session.User.ID
	}
	if createdByUserID == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	expiresAt, ok := parseOptionalRFC3339(request.ExpiresAt)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid artifact access token request"})
	}
	result, err := h.service.CreateArtifactAccessToken(c.Request().Context(), domain.ArtifactAccessTokenCreateInput{
		Name:            request.Name,
		Token:           request.Token,
		CreatedByUserID: createdByUserID,
		ExpiresAt:       expiresAt,
	})
	if err != nil {
		return artifactAccessTokenErrorResponse(c, err)
	}
	return c.JSON(http.StatusCreated, artifactAccessTokenDetailResponse{Token: newArtifactAccessTokenResponse(result.Token)})
}

func (h artifactAccessTokenHandler) update(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "artifact access token service unavailable"})
	}

	var request artifactAccessTokenUpdateRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid artifact access token request"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionAccessTokenManage, domain.AuthorizationResource{}); err != nil {
		return err
	}
	token, err := h.service.UpdateArtifactAccessToken(c.Request().Context(), c.Param("id"), domain.ArtifactAccessTokenUpdateInput{
		Status: domain.ArtifactAccessTokenStatus(request.Status),
	})
	if err != nil {
		return artifactAccessTokenErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, artifactAccessTokenDetailResponse{Token: newArtifactAccessTokenResponse(token)})
}

func (h artifactAccessTokenHandler) delete(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "artifact access token service unavailable"})
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionAccessTokenManage, domain.AuthorizationResource{}); err != nil {
		return err
	}
	if err := h.service.DeleteArtifactAccessToken(c.Request().Context(), c.Param("id")); err != nil {
		return artifactAccessTokenErrorResponse(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func artifactAccessTokenErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidArtifactAccessTokenInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid artifact access token request"})
	case errors.Is(err, domain.ErrArtifactAccessTokenAlreadyExists):
		return c.JSON(http.StatusConflict, map[string]string{"error": "artifact access token already exists"})
	case errors.Is(err, domain.ErrArtifactAccessTokenNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "artifact access token not found"})
	case errors.Is(err, domain.ErrForbidden):
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func newArtifactAccessTokenResponses(tokens []domain.ArtifactAccessToken) []artifactAccessTokenResponse {
	responses := make([]artifactAccessTokenResponse, 0, len(tokens))
	for _, token := range tokens {
		responses = append(responses, newArtifactAccessTokenResponse(token))
	}
	return responses
}

func newArtifactAccessTokenResponse(token domain.ArtifactAccessToken) artifactAccessTokenResponse {
	return artifactAccessTokenResponse{
		ID:           token.ID,
		Name:         token.Name,
		TokenPreview: token.TokenPreview,
		Status:       string(token.Status),
		CreatedBy:    token.CreatedBy,
		CreatedAt:    formatTime(token.CreatedAt),
		LastUsedAt:   formatOptionalTime(token.LastUsedAt),
		ExpiresAt:    formatOptionalTime(token.ExpiresAt),
	}
}

func parseOptionalRFC3339(value string) (*time.Time, bool) {
	if value == "" {
		return nil, true
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, false
	}
	return &parsed, true
}
