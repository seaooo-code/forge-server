package httpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

func registerHealthRoutes(e *echo.Echo, deps Dependencies) {
	e.GET("/healthz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, healthResponse{Status: "ok"})
	})

	e.GET("/readyz", func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Second)
		defer cancel()

		checks := map[string]string{
			"postgres": "ok",
		}

		statusCode := http.StatusOK
		status := "ready"

		if err := deps.Postgres.Ping(ctx); err != nil {
			checks["postgres"] = err.Error()
			statusCode = http.StatusServiceUnavailable
			status = "not_ready"
		}

		return c.JSON(statusCode, healthResponse{
			Status: status,
			Checks: checks,
		})
	})
}
