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
	Environment environmentResponse     `json:"environment"`
	Detail      environmentDetailFields `json:"detail"`
}

type environmentResponse struct {
	ID                 string `json:"id"`
	Code               string `json:"code"`
	Name               string `json:"name"`
	Status             string `json:"status"`
	Tone               string `json:"tone"`
	Description        string `json:"description"`
	CurrentCombination string `json:"currentCombination"`
	Traffic            string `json:"traffic"`
	UpdatedAt          string `json:"updatedAt"`
}

type environmentDetailFields struct {
	ID             string                         `json:"id"`
	Endpoint       string                         `json:"endpoint"`
	CDNDomain      string                         `json:"cdnDomain"`
	BranchPolicy   string                         `json:"branchPolicy"`
	ReleasePolicy  string                         `json:"releasePolicy"`
	CurrentRelease string                         `json:"currentRelease"`
	BundleCount    int                            `json:"bundleCount"`
	TotalSize      string                         `json:"totalSize"`
	HealthScore    string                         `json:"healthScore"`
	Guardrails     []environmentGuardrailResponse `json:"guardrails"`
	Timeline       []environmentTimelineResponse  `json:"timeline"`
}

type environmentGuardrailResponse struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Value  string `json:"value"`
	Status string `json:"status"`
}

type environmentTimelineResponse struct {
	ID       string `json:"id"`
	Time     string `json:"time"`
	Action   string `json:"action"`
	Operator string `json:"operator"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
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
		ID:                 environment.Code,
		Code:               environment.ShortCode,
		Name:               environment.Name,
		Status:             environment.Status.Label(),
		Tone:               environment.Tone,
		Description:        environment.Description,
		CurrentCombination: environment.CurrentCombination,
		Traffic:            environment.TrafficLabel,
		UpdatedAt:          formatTime(environment.UpdatedAt),
	}
}

func newEnvironmentDetailResponse(detail domain.EnvironmentDetail) environmentDetailResponse {
	return environmentDetailResponse{
		Environment: newEnvironmentResponse(detail.Environment),
		Detail: environmentDetailFields{
			ID:             detail.ID,
			Endpoint:       detail.Endpoint,
			CDNDomain:      detail.CDNDomain,
			BranchPolicy:   detail.BranchPolicy,
			ReleasePolicy:  detail.ReleasePolicy,
			CurrentRelease: detail.CurrentRelease,
			BundleCount:    detail.BundleCount,
			TotalSize:      detail.TotalSize,
			HealthScore:    detail.HealthScore,
			Guardrails:     newEnvironmentGuardrailResponses(detail.Guardrails),
			Timeline:       newEnvironmentTimelineResponses(detail.Timeline),
		},
	}
}

func newEnvironmentGuardrailResponses(guardrails []domain.EnvironmentGuardrail) []environmentGuardrailResponse {
	response := make([]environmentGuardrailResponse, 0, len(guardrails))
	for _, guardrail := range guardrails {
		response = append(response, environmentGuardrailResponse{
			ID:     guardrail.ID,
			Label:  guardrail.Label,
			Value:  guardrail.Value,
			Status: guardrail.Status,
		})
	}
	return response
}

func newEnvironmentTimelineResponses(events []domain.EnvironmentTimelineEvent) []environmentTimelineResponse {
	response := make([]environmentTimelineResponse, 0, len(events))
	for _, event := range events {
		response = append(response, environmentTimelineResponse{
			ID:       event.ID,
			Time:     event.DisplayTime,
			Action:   event.Action,
			Operator: event.Operator,
			Status:   event.Status,
			Detail:   event.Detail,
		})
	}
	return response
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}
