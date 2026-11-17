package httpserver

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

// registerMetricsRoutes 以 Prometheus 文本格式暴露构建队列指标，供外部扩缩容（例如按等待数或最久等待时长加减 worker）使用。
// 配置了 FORGE_METRICS_TOKEN 时要求 Bearer 令牌；没配置时与 /healthz 一样不鉴权，只应在内网暴露。
func registerMetricsRoutes(e *echo.Echo, metricsToken string, queue BuildQueueService) {
	if queue == nil {
		return
	}
	e.GET("/metrics", func(c echo.Context) error {
		if metricsToken != "" {
			provided, ok := strings.CutPrefix(c.Request().Header.Get(echo.HeaderAuthorization), "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(provided)), []byte(metricsToken)) != 1 {
				return c.String(http.StatusUnauthorized, "unauthorized\n")
			}
		}
		stats, err := queue.GetBuildQueueStats(c.Request().Context())
		if err != nil {
			return c.String(http.StatusServiceUnavailable, "query build queue stats failed\n")
		}
		return c.Blob(http.StatusOK, "text/plain; version=0.0.4; charset=utf-8", []byte(formatBuildQueueMetrics(stats)))
	})
}

func formatBuildQueueMetrics(stats domain.BuildQueueStats) string {
	var b strings.Builder
	gauge := func(name string, help string, value any) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %v\n", name, help, name, name, value)
	}
	gauge("forge_build_queue_waiting", "Builds waiting for a worker, including running builds whose lease expired.", stats.Waiting)
	gauge("forge_build_queue_oldest_wait_seconds", "Seconds the longest-waiting build has been queued, 0 when the queue is empty.", stats.OldestWaitSeconds)
	gauge("forge_build_running", "Builds running under a valid lease.", stats.Running)
	gauge("forge_build_workers_active", "Worker processes holding at least one valid build lease.", stats.ActiveWorkers)
	return b.String()
}
