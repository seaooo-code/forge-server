package httpserver

import (
	"errors"
	"net/http"
	"strconv"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type artifactPullStatsHandler struct {
	service ArtifactPullStatsService
}

type artifactPullStatsResponse struct {
	From  string                     `json:"from"`
	To    string                     `json:"to"`
	Stats []artifactPullStatResponse `json:"stats"`
}

type artifactPullStatResponse struct {
	Day            string `json:"day"`
	Environment    string `json:"environment"`
	ModuleID       string `json:"moduleId"`
	ModuleName     string `json:"moduleName"`
	RuntimeVersion string `json:"runtimeVersion"`
	Platform       string `json:"platform"`
	Channel        string `json:"channel"`
	Outcome        string `json:"outcome"`
	BuildRecordID  string `json:"buildRecordId"`
	BuildNumber    int64  `json:"buildNumber"`
	Requests       int64  `json:"requests"`
}

func registerArtifactPullStatsRoutes(group *echo.Group, service ArtifactPullStatsService) {
	handler := artifactPullStatsHandler{service: service}
	group.GET("/artifact-pull-stats", handler.list)
}

func (h artifactPullStatsHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "artifact pull stats service unavailable"})
	}
	days := 0
	if raw := c.QueryParam("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid artifact pull stats request"})
		}
		days = parsed
	}
	rows, from, to, err := h.service.ListArtifactPullStats(c.Request().Context(), domain.ArtifactPullStatsFilters{
		ApplicationID: c.QueryParam("applicationId"),
		Environment:   c.QueryParam("environment"),
		ModuleID:      c.QueryParam("moduleId"),
		BuildRecordID: c.QueryParam("buildRecordId"),
		Days:          days,
	})
	if errors.Is(err, domain.ErrInvalidArtifactPullStatsInput) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid artifact pull stats request"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	response := artifactPullStatsResponse{From: from.Format(dateLayout), To: to.Format(dateLayout), Stats: make([]artifactPullStatResponse, 0, len(rows))}
	for _, row := range rows {
		response.Stats = append(response.Stats, artifactPullStatResponse{
			Day:            row.Day.Format(dateLayout),
			Environment:    row.Environment,
			ModuleID:       row.ModuleID,
			ModuleName:     row.ModuleName,
			RuntimeVersion: row.RuntimeVersion,
			Platform:       row.Platform,
			Channel:        row.Channel,
			Outcome:        row.Outcome,
			BuildRecordID:  row.BuildRecordID,
			BuildNumber:    row.BuildNumber,
			Requests:       row.Requests,
		})
	}
	return c.JSON(http.StatusOK, response)
}

const dateLayout = "2006-01-02"
