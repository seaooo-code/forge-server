package httpserver

import (
	"context"
	"net/http"

	"forge-server/internal/config"
	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9"
)

type Dependencies struct {
	EnvironmentService EnvironmentService
	Postgres           *pgxpool.Pool
	Redis              *redis.Client
}

type EnvironmentService interface {
	GetEnvironmentDetail(ctx context.Context, code string) (domain.EnvironmentDetail, error)
	ListEnvironments(ctx context.Context) ([]domain.Environment, error)
}

func New(cfg config.Config, deps Dependencies) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())
	e.Use(middleware.CORS())
	e.Use(middleware.Logger())

	registerHealthRoutes(e, deps)

	api := e.Group("/api/v1")
	api.GET("/ping", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"app":    "forge-server",
			"env":    cfg.App.Env,
			"status": "ok",
		})
	})
	registerEnvironmentRoutes(api, deps.EnvironmentService)

	return e
}
