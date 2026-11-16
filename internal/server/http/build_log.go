package httpserver

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

type BuildLogService interface {
	ListBuildLogs(ctx context.Context, buildID string, afterSeq int) (domain.BuildLogPage, error)
}

type buildLogHandler struct {
	service BuildLogService
}

type buildLogsResponse struct {
	Chunks   []buildLogChunkResponse `json:"chunks"`
	Finished bool                    `json:"finished"`
}

type buildLogChunkResponse struct {
	Seq     int    `json:"seq"`
	Content string `json:"content"`
}

// registerBuildLogRoutes 注册构建日志查询。权限与查看构建详情一致：登录控制台即可查看。
func registerBuildLogRoutes(group *echo.Group, service BuildLogService) {
	handler := buildLogHandler{service: service}
	group.GET("/builds/:id/logs", handler.list)
}

func (h buildLogHandler) list(c echo.Context) error {
	if h.service == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "build log service unavailable"})
	}

	afterSeq := -1
	if raw := strings.TrimSpace(c.QueryParam("afterSeq")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return buildErrorResponse(c, domain.ErrInvalidBuildInput)
		}
		afterSeq = value
	}

	page, err := h.service.ListBuildLogs(c.Request().Context(), c.Param("id"), afterSeq)
	if err != nil {
		return buildErrorResponse(c, err)
	}

	chunks := make([]buildLogChunkResponse, 0, len(page.Chunks))
	for _, chunk := range page.Chunks {
		chunks = append(chunks, buildLogChunkResponse{Seq: chunk.Seq, Content: chunk.Content})
	}
	return c.JSON(http.StatusOK, buildLogsResponse{Chunks: chunks, Finished: page.Finished})
}
