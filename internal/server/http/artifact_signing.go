package httpserver

import (
	"context"
	"errors"
	"net/http"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type ArtifactSigningService interface {
	GetApplicationSigning(ctx context.Context, applicationSlug string) (domain.ApplicationSigning, error)
	CreateSigningKey(ctx context.Context, applicationSlug string, actorUserID string) (domain.SigningKey, error)
	DeleteSigningKey(ctx context.Context, applicationSlug string, keyID string) error
	SetRequireSignedReleases(ctx context.Context, applicationSlug string, require bool) (domain.ApplicationSigning, error)
}

type artifactSigningHandler struct {
	authService AuthorizationService
	service     ArtifactSigningService
}

type applicationSigningResponse struct {
	RequireSignedReleases bool                 `json:"requireSignedReleases"`
	Keys                  []signingKeyResponse `json:"keys"`
}

type signingKeyResponse struct {
	KeyID        string       `json:"keyId"`
	Algorithm    string       `json:"algorithm"`
	PublicKey    string       `json:"publicKey"`
	Status       string       `json:"status"`
	CreatedBy    *userRefJSON `json:"createdBy"`
	CreatedAt    string       `json:"createdAt"`
	RetiredAt    string       `json:"retiredAt"`
	SignedBuilds int          `json:"signedBuilds"`
}

type applicationSigningUpdateRequest struct {
	RequireSignedReleases *bool `json:"requireSignedReleases"`
}

func registerArtifactSigningRoutes(group *echo.Group, service ArtifactSigningService, authService AuthorizationService) {
	handler := artifactSigningHandler{authService: authService, service: service}

	group.GET("/applications/:slug/signing", handler.get)
	group.PATCH("/applications/:slug/signing", handler.update)
	group.POST("/applications/:slug/signing/keys", handler.createKey)
	group.DELETE("/applications/:slug/signing/keys/:keyId", handler.deleteKey)
}

func (h artifactSigningHandler) get(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "signing service unavailable"})
	}
	signing, err := h.service.GetApplicationSigning(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return artifactSigningErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newApplicationSigningResponse(signing))
}

func (h artifactSigningHandler) update(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "signing service unavailable"})
	}
	var request applicationSigningUpdateRequest
	if err := c.Bind(&request); err != nil || request.RequireSignedReleases == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid signing request"})
	}
	if _, err := h.signingActor(c); err != nil {
		return err
	}
	signing, err := h.service.SetRequireSignedReleases(c.Request().Context(), c.Param("slug"), *request.RequireSignedReleases)
	if err != nil {
		return artifactSigningErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newApplicationSigningResponse(signing))
}

func (h artifactSigningHandler) createKey(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "signing service unavailable"})
	}
	actor, err := h.signingActor(c)
	if err != nil {
		return err
	}
	key, err := h.service.CreateSigningKey(c.Request().Context(), c.Param("slug"), actor)
	if err != nil {
		return artifactSigningErrorResponse(c, err)
	}
	return c.JSON(http.StatusCreated, newSigningKeyResponse(key))
}

func (h artifactSigningHandler) deleteKey(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "signing service unavailable"})
	}
	if _, err := h.signingActor(c); err != nil {
		return err
	}
	if err := h.service.DeleteSigningKey(c.Request().Context(), c.Param("slug"), c.Param("keyId")); err != nil {
		return artifactSigningErrorResponse(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// signingActor 校验当前用户能修改该应用（密钥管理与应用配置共用 application:update），返回操作人。
func (h artifactSigningHandler) signingActor(c echo.Context) (string, error) {
	if err := authorizeCurrentUser(c, h.authService, domain.ActionApplicationUpdate, domain.AuthorizationResource{ApplicationSlug: c.Param("slug")}); err != nil {
		return "", err
	}
	session, ok := consoleSession(c)
	if !ok {
		return "", c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	actor := session.User.ID
	if actor == "" {
		return "", c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	return actor, nil
}

func newApplicationSigningResponse(signing domain.ApplicationSigning) applicationSigningResponse {
	response := applicationSigningResponse{RequireSignedReleases: signing.RequireSignedReleases, Keys: make([]signingKeyResponse, 0, len(signing.Keys))}
	for _, key := range signing.Keys {
		response.Keys = append(response.Keys, newSigningKeyResponse(key))
	}
	return response
}

func newSigningKeyResponse(key domain.SigningKey) signingKeyResponse {
	response := signingKeyResponse{
		KeyID:        key.KeyID,
		Algorithm:    key.Algorithm,
		PublicKey:    key.PublicKey,
		Status:       string(key.Status),
		CreatedAt:    formatTime(key.CreatedAt),
		SignedBuilds: key.SignedBuilds,
	}
	if key.CreatedByUserID != "" {
		response.CreatedBy = &userRefJSON{ID: key.CreatedByUserID, Name: key.CreatedBy}
	}
	if key.RetiredAt != nil {
		response.RetiredAt = formatTime(*key.RetiredAt)
	}
	return response
}

func artifactSigningErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidSigningInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid signing request"})
	case errors.Is(err, domain.ErrApplicationNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "application not found"})
	case errors.Is(err, domain.ErrSigningKeyNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "signing key not found"})
	case errors.Is(err, domain.ErrSigningKeyActive):
		return c.JSON(http.StatusConflict, map[string]string{"error": "signing key active"})
	case errors.Is(err, domain.ErrSigningKeyRequired):
		return c.JSON(http.StatusConflict, map[string]string{"error": "signing key required"})
	case errors.Is(err, domain.ErrForbidden):
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}
