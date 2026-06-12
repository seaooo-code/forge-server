package httpserver

import (
	"errors"
	"net/http"
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

type environmentResponse struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Tone        string `json:"tone"`
	Description string `json:"description"`
	Traffic     string `json:"traffic"`
	UpdatedAt   string `json:"updatedAt"`
}

func registerEnvironmentRoutes(group *echo.Group, service EnvironmentService) {
	handler := environmentHandler{service: service}

	group.GET("/environments", handler.list)
	group.GET("/environments/:code", handler.detail)
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

func (h environmentHandler) detail(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "environment service unavailable"})
	}

	detail, err := h.service.GetEnvironmentDetail(c.Request().Context(), c.Param("code"))
	if err != nil {
		if errors.Is(err, domain.ErrEnvironmentNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "environment not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	return c.JSON(http.StatusOK, newEnvironmentDetailResponse(detail))
}

func newEnvironmentResponse(environment domain.Environment) environmentResponse {
	return environmentResponse{
		ID:          environment.Code,
		Code:        environment.ShortCode,
		Name:        environment.Name,
		Status:      environment.Status.Label(),
		Tone:        environment.Tone,
		Description: environment.Description,
		Traffic:     environment.TrafficLabel,
		UpdatedAt:   formatTime(environment.UpdatedAt),
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
