package httpserver

import (
	"net/http"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type dashboardHandler struct {
	service DashboardService
}

type dashboardResponse struct {
	Metrics    []dashboardMetricResponse          `json:"metrics"`
	BuildTrend []dashboardBuildTrendPointResponse `json:"buildTrend"`
	Builds     []dashboardBuildRecordResponse     `json:"builds"`
	Releases   []dashboardReleaseRecordResponse   `json:"releases"`
	Resources  []dashboardResourceStatusResponse  `json:"resources"`
	UpdatedAt  string                             `json:"updatedAt"`
}

type dashboardMetricResponse struct {
	Description string `json:"description"`
	ID          string `json:"id"`
	Label       string `json:"label"`
	Value       string `json:"value"`
	Trend       string `json:"trend"`
	TrendTone   string `json:"trendTone"`
	Icon        string `json:"icon"`
	IconTone    string `json:"iconTone"`
	Sparkline   []int  `json:"sparkline"`
}

type dashboardBuildTrendPointResponse struct {
	Date         string `json:"date"`
	BuildCount   int    `json:"buildCount"`
	SuccessCount int    `json:"successCount"`
}

type dashboardBuildRecordResponse struct {
	Application string `json:"application"`
	Duration    string `json:"duration"`
	Environment string `json:"environment"`
	ID          string `json:"id"`
	Module      string `json:"module"`
	Operator    string `json:"operator"`
	StartedAt   string `json:"startedAt"`
	Status      string `json:"status"`
}

type dashboardReleaseRecordResponse struct {
	Application string `json:"application"`
	ID          string `json:"id"`
	Environment string `json:"environment"`
	Gray        string `json:"gray"`
	Module      string `json:"module"`
	Status      string `json:"status"`
	UpdatedAt   string `json:"updatedAt"`
}

type dashboardResourceStatusResponse struct {
	Description string `json:"description"`
	Href        string `json:"href"`
	ID          string `json:"id"`
	Label       string `json:"label"`
	Status      string `json:"status"`
	StatusLabel string `json:"statusLabel"`
	Value       string `json:"value"`
}

func registerDashboardRoutes(group *echo.Group, service DashboardService) {
	handler := dashboardHandler{service: service}

	group.GET("/dashboard", handler.get)
}

func (h dashboardHandler) get(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "dashboard service unavailable"})
	}

	result, err := h.service.GetDashboardSnapshot(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	return c.JSON(http.StatusOK, newDashboardResponse(result))
}

func newDashboardResponse(snapshot domain.DashboardSnapshot) dashboardResponse {
	return dashboardResponse{
		Metrics:    newDashboardMetricResponses(snapshot.Metrics),
		BuildTrend: newDashboardBuildTrendPointResponses(snapshot.BuildTrend),
		Builds:     newDashboardBuildRecordResponses(snapshot.Builds),
		Releases:   newDashboardReleaseRecordResponses(snapshot.Releases),
		Resources:  newDashboardResourceStatusResponses(snapshot.Resources),
		UpdatedAt:  formatTime(snapshot.UpdatedAt),
	}
}

func newDashboardMetricResponses(metrics []domain.DashboardMetric) []dashboardMetricResponse {
	responses := make([]dashboardMetricResponse, 0, len(metrics))
	for _, metric := range metrics {
		responses = append(responses, dashboardMetricResponse{
			Description: metric.Description,
			ID:          metric.ID,
			Label:       metric.Label,
			Value:       metric.Value,
			Trend:       metric.Trend,
			TrendTone:   metric.TrendTone,
			Icon:        metric.Icon,
			IconTone:    metric.IconTone,
			Sparkline:   metric.Sparkline,
		})
	}
	return responses
}

func newDashboardBuildTrendPointResponses(points []domain.DashboardBuildTrendPoint) []dashboardBuildTrendPointResponse {
	responses := make([]dashboardBuildTrendPointResponse, 0, len(points))
	for _, point := range points {
		responses = append(responses, dashboardBuildTrendPointResponse{
			Date:         point.Date,
			BuildCount:   point.BuildCount,
			SuccessCount: point.SuccessCount,
		})
	}
	return responses
}

func newDashboardBuildRecordResponses(records []domain.DashboardBuildRecord) []dashboardBuildRecordResponse {
	responses := make([]dashboardBuildRecordResponse, 0, len(records))
	for _, record := range records {
		responses = append(responses, dashboardBuildRecordResponse{
			Application: record.Application,
			Duration:    record.Duration,
			Environment: record.Environment,
			ID:          record.ID,
			Module:      record.Module,
			Operator:    record.Operator,
			StartedAt:   record.StartedAt,
			Status:      record.Status,
		})
	}
	return responses
}

func newDashboardReleaseRecordResponses(records []domain.DashboardReleaseRecord) []dashboardReleaseRecordResponse {
	responses := make([]dashboardReleaseRecordResponse, 0, len(records))
	for _, record := range records {
		responses = append(responses, dashboardReleaseRecordResponse{
			Application: record.Application,
			ID:          record.ID,
			Environment: record.Environment,
			Gray:        record.Gray,
			Module:      record.Module,
			Status:      record.Status,
			UpdatedAt:   record.UpdatedAt,
		})
	}
	return responses
}

func newDashboardResourceStatusResponses(resources []domain.DashboardResourceStatus) []dashboardResourceStatusResponse {
	responses := make([]dashboardResourceStatusResponse, 0, len(resources))
	for _, resource := range resources {
		responses = append(responses, dashboardResourceStatusResponse{
			Description: resource.Description,
			Href:        resource.Href,
			ID:          resource.ID,
			Label:       resource.Label,
			Status:      resource.Status,
			StatusLabel: resource.StatusLabel,
			Value:       resource.Value,
		})
	}
	return responses
}
