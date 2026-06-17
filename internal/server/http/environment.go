package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type environmentHandler struct {
	service EnvironmentService
}

type environmentsResponse struct {
	Environments []environmentResponse `json:"environments"`
}

type environmentDetailResponse struct {
	Environment environmentResponse `json:"environment"`
}

type environmentRequest struct {
	Code                   string `json:"code"`
	Description            string `json:"description"`
	Name                   string `json:"name"`
	Status                 string `json:"status"`
	StorageConfigurationID string `json:"storageConfigurationId"`
	Tone                   string `json:"tone"`
}

type environmentResponse struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	Status                   string `json:"status"`
	Tone                     string `json:"tone"`
	Description              string `json:"description"`
	StorageConfigurationID   string `json:"storageConfigurationId"`
	StorageConfigurationName string `json:"storageConfigurationName"`
	UpdatedAt                string `json:"updatedAt"`
}

func registerEnvironmentRoutes(group *echo.Group, service EnvironmentService) {
	handler := environmentHandler{service: service}

	group.GET("/environments", handler.list)
	group.POST("/environments", handler.create)
	group.GET("/environments/:code", handler.detail)
	group.PATCH("/environments/:code", handler.update)
}

func (h environmentHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "environment service unavailable"})
	}

	environments, err := h.service.ListEnvironments(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	response := environmentsResponse{
		Environments: make([]environmentResponse, 0, len(environments)),
	}
	for _, environment := range environments {
		response.Environments = append(response.Environments, newEnvironmentResponse(environment))
	}

	return c.JSON(http.StatusOK, response)
}

func (h environmentHandler) create(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "environment service unavailable"})
	}

	input, ok := bindEnvironmentRequest(c)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid environment request"})
	}

	environment, err := h.service.CreateEnvironment(c.Request().Context(), input)
	if err != nil {
		return environmentErrorResponse(c, err)
	}

	return c.JSON(http.StatusCreated, newEnvironmentDetailResponse(environment))
}

func (h environmentHandler) detail(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "environment service unavailable"})
	}

	detail, err := h.service.GetEnvironmentDetail(c.Request().Context(), c.Param("code"))
	if err != nil {
		return environmentErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, newEnvironmentDetailResponse(detail))
}

func (h environmentHandler) update(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "environment service unavailable"})
	}

	input, ok := bindEnvironmentRequest(c)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid environment request"})
	}

	environment, err := h.service.UpdateEnvironment(c.Request().Context(), c.Param("code"), input)
	if err != nil {
		return environmentErrorResponse(c, err)
	}

	return c.JSON(http.StatusOK, newEnvironmentDetailResponse(environment))
}

func bindEnvironmentRequest(c echo.Context) (domain.EnvironmentInput, bool) {
	var request environmentRequest
	if err := c.Bind(&request); err != nil {
		return domain.EnvironmentInput{}, false
	}

	return domain.EnvironmentInput{
		Code:                   request.Code,
		Name:                   request.Name,
		Status:                 domain.EnvironmentStatus(request.Status),
		Tone:                   request.Tone,
		Description:            request.Description,
		StorageConfigurationID: request.StorageConfigurationID,
	}, true
}

func environmentErrorResponse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrEnvironmentNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "environment not found"})
	case errors.Is(err, domain.ErrInvalidEnvironmentInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid environment request"})
	case errors.Is(err, domain.ErrStorageConfigurationNotFound):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "storage configuration not found"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func newEnvironmentResponse(environment domain.Environment) environmentResponse {
	return environmentResponse{
		ID:                       environment.Code,
		Name:                     environment.Name,
		Status:                   string(environment.Status),
		Tone:                     environment.Tone,
		Description:              environment.Description,
		StorageConfigurationID:   environment.StorageConfigurationID,
		StorageConfigurationName: environment.StorageConfigurationName,
		UpdatedAt:                formatTime(environment.UpdatedAt),
	}
}

func newEnvironmentDetailResponse(environment domain.Environment) environmentDetailResponse {
	return environmentDetailResponse{
		Environment: newEnvironmentResponse(environment),
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTime(*value)
}

func int64String(value int64) string {
	return strconv.FormatInt(value, 10)
}
