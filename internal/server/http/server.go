package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"forge-server/internal/config"
	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	otellog "go.opentelemetry.io/otel/log"
	logglobal "go.opentelemetry.io/otel/log/global"
)

const httpExchangeStartedAtKey = "http_exchange_started_at"

type Dependencies struct {
	ApplicationService   ApplicationService
	CodeSourceService    CodeSourceService
	EnvironmentService   EnvironmentService
	FeishuAuthService    FeishuAuthService
	FeishuContactService FeishuContactService
	ModuleService        ModuleService
	StorageService       StorageService
	Postgres             *pgxpool.Pool
	Redis                *redis.Client
}

type ApplicationService interface {
	CheckApplicationDelete(ctx context.Context, slug string) (domain.ApplicationDeleteCheck, error)
	CreateApplication(ctx context.Context, input domain.ApplicationInput) (domain.ApplicationDetail, error)
	DeleteApplication(ctx context.Context, slug string) error
	GetApplicationDetail(ctx context.Context, slug string) (domain.ApplicationDetail, error)
	GetApplicationOptions(ctx context.Context) (domain.ApplicationOptions, error)
	ListApplications(ctx context.Context, filters domain.ApplicationListFilters) ([]domain.Application, domain.ApplicationSummary, error)
	UpdateApplication(ctx context.Context, slug string, input domain.ApplicationInput) (domain.ApplicationDetail, error)
	UpdateApplicationStatus(ctx context.Context, slug string, status domain.ApplicationStatus) (domain.ApplicationDetail, error)
}

type EnvironmentService interface {
	CreateEnvironment(ctx context.Context, input domain.EnvironmentInput) (domain.Environment, error)
	GetEnvironmentDetail(ctx context.Context, code string) (domain.Environment, error)
	ListEnvironments(ctx context.Context) ([]domain.Environment, error)
	UpdateEnvironment(ctx context.Context, code string, input domain.EnvironmentInput) (domain.Environment, error)
}

type CodeSourceService interface {
	CompleteAppManifest(ctx context.Context, provider domain.CodeSourceProvider, code string) (domain.CodeSourceInstallURL, error)
	CompleteInstallation(ctx context.Context, provider domain.CodeSourceProvider, ownerUnionID string, installationID int64, setupAction string) (domain.CodeSourceApp, error)
	DeleteInstallations(ctx context.Context, provider domain.CodeSourceProvider, ownerUnionID string) error
	GetAppManifest(ctx context.Context, provider domain.CodeSourceProvider) (domain.CodeSourceAppManifest, error)
	GetInstallURL(ctx context.Context, provider domain.CodeSourceProvider, ownerUnionID string) (domain.CodeSourceInstallURL, error)
	ListCodeSources(ctx context.Context, ownerUnionID string) ([]domain.CodeSourceApp, error)
	SearchRepositories(ctx context.Context, input domain.CodeSourceRepositorySearchInput) (domain.CodeSourceRepositorySearchResult, error)
}

type ModuleService interface {
	CreateModule(ctx context.Context, input domain.ModuleInput) (domain.Module, error)
	DeleteModule(ctx context.Context, id string) error
	GetModule(ctx context.Context, id string) (domain.Module, error)
	ListModules(ctx context.Context, filters domain.ModuleListFilters) ([]domain.Module, domain.ModuleSummary, error)
	UpdateModule(ctx context.Context, id string, input domain.ModuleInput) (domain.Module, error)
}

type FeishuAuthService interface {
	AuthorizationURL(state string) (string, error)
	CompleteAuthorization(ctx context.Context, code string) (domain.FeishuAuthResult, error)
	GetSession(ctx context.Context, sessionToken string) (domain.FeishuAuthResult, error)
	Logout(ctx context.Context, sessionToken string) error
}

type FeishuContactService interface {
	SearchUsers(ctx context.Context, accessToken string, query string, pageSize int, pageToken string) (domain.FeishuUserSearchPage, error)
}

type StorageService interface {
	CreateStorageConfiguration(ctx context.Context, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error)
	DeleteStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error)
	GetStorageConfiguration(ctx context.Context, id string) (domain.StorageConfiguration, error)
	ListStorageConfigurations(ctx context.Context) ([]domain.StorageConfiguration, error)
	TestStorageConfiguration(ctx context.Context, input domain.StorageConfigurationInput) (domain.StorageConfigurationTestResult, error)
	UpdateStorageConfiguration(ctx context.Context, id string, input domain.StorageConfigurationInput) (domain.StorageConfiguration, error)
}

func New(cfg config.Config, deps Dependencies) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     cfg.HTTP.CORSAllowOrigins,
		AllowHeaders:     cfg.HTTP.CORSAllowHeaders,
		AllowCredentials: cfg.HTTP.CORSAllowCredentials,
	}))
	if cfg.Telemetry.Enabled {
		e.Use(otelecho.Middleware(cfg.Telemetry.ServiceName))
		e.Use(logHTTPExchanges(logglobal.Logger("forge-server/internal/server/http")))
	}
	e.Use(middleware.Logger())

	registerHealthRoutes(e, deps)
	registerOpenAPIRoutes(e)

	api := e.Group("/api/v1")
	api.GET("/ping", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"app":    "forge-server",
			"env":    cfg.App.Env,
			"status": "ok",
		})
	})
	registerEnumRoutes(api)
	registerApplicationRoutes(api, deps.ApplicationService)
	registerCodeSourceRoutes(api, deps.CodeSourceService, deps.FeishuAuthService, cfg.GitHub.InstallRedirectURL)
	registerEnvironmentRoutes(api, deps.EnvironmentService)
	registerFeishuAuthRoutes(api, deps.FeishuAuthService)
	registerFeishuContactRoutes(api, deps.FeishuContactService, deps.FeishuAuthService)
	registerModuleRoutes(api, deps.ModuleService)
	registerStorageRoutes(api, deps.StorageService)

	return e
}

func logHTTPExchanges(logger otellog.Logger) echo.MiddlewareFunc {
	dump := middleware.BodyDump(func(c echo.Context, requestBody []byte, responseBody []byte) {
		emitHTTPExchangeLog(logger, c, requestBody, responseBody)
	})

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return dump(func(c echo.Context) error {
			c.Set(httpExchangeStartedAtKey, time.Now())
			return next(c)
		})
	}
}

func emitHTTPExchangeLog(logger otellog.Logger, c echo.Context, requestBody []byte, responseBody []byte) {
	now := time.Now()
	startedAt := now
	if value, ok := c.Get(httpExchangeStartedAtKey).(time.Time); ok {
		startedAt = value
	}

	request := c.Request()
	response := c.Response()
	statusCode := response.Status
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	record := otellog.Record{}
	record.SetEventName("http.exchange")
	record.SetTimestamp(startedAt)
	record.SetObservedTimestamp(now)
	record.SetSeverity(otellog.SeverityInfo)
	record.SetSeverityText("INFO")
	record.SetBody(otellog.MapValue(
		otellog.Map("request",
			otellog.String("method", request.Method),
			otellog.String("path", request.URL.Path),
			otellog.String("route", c.Path()),
			otellog.String("remoteAddr", request.RemoteAddr),
			otellog.Map("query",
				otellog.String("raw", c.QueryString()),
				otellog.KeyValue{Key: "params", Value: queryLogValue(c.QueryParams())},
			),
			otellog.KeyValue{Key: "headers", Value: headerLogValue(request.Header)},
			otellog.KeyValue{Key: "body", Value: bodyLogValue(requestBody)},
			otellog.Int("bodySize", len(requestBody)),
		),
		otellog.Map("response",
			otellog.Int("statusCode", statusCode),
			otellog.KeyValue{Key: "headers", Value: headerLogValue(response.Header())},
			otellog.KeyValue{Key: "body", Value: bodyLogValue(responseBody)},
			otellog.Int("bodySize", len(responseBody)),
		),
		otellog.Int64("durationMs", now.Sub(startedAt).Milliseconds()),
	))
	record.AddAttributes(
		otellog.String("event.name", "http.exchange"),
		otellog.String("http.request.method", request.Method),
		otellog.String("url.path", request.URL.Path),
		otellog.String("http.route", c.Path()),
		otellog.Int("http.response.status_code", statusCode),
	)

	logger.Emit(request.Context(), record)
}

func bodyLogValue(body []byte) otellog.Value {
	if len(body) == 0 {
		return otellog.Value{}
	}

	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return otellog.StringValue(string(body))
	}

	return anyLogValue(decoded)
}

func anyLogValue(value any) otellog.Value {
	switch typed := value.(type) {
	case nil:
		return otellog.Value{}
	case map[string]any:
		items := make([]otellog.KeyValue, 0, len(typed))
		for key, value := range typed {
			items = append(items, otellog.KeyValue{Key: key, Value: anyLogValue(value)})
		}
		return otellog.MapValue(items...)
	case []any:
		items := make([]otellog.Value, 0, len(typed))
		for _, value := range typed {
			items = append(items, anyLogValue(value))
		}
		return otellog.SliceValue(items...)
	case string:
		return otellog.StringValue(typed)
	case bool:
		return otellog.BoolValue(typed)
	case json.Number:
		if intValue, err := typed.Int64(); err == nil {
			return otellog.Int64Value(intValue)
		}
		if floatValue, err := typed.Float64(); err == nil {
			return otellog.Float64Value(floatValue)
		}
		return otellog.StringValue(typed.String())
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return otellog.StringValue("")
		}
		return otellog.StringValue(string(data))
	}
}

func headerLogValue(header http.Header) otellog.Value {
	if len(header) == 0 {
		return otellog.MapValue()
	}

	items := make([]otellog.KeyValue, 0, len(header))
	for key, values := range header {
		items = append(items, otellog.KeyValue{Key: key, Value: stringSliceLogValue(values)})
	}
	return otellog.MapValue(items...)
}

func queryLogValue(query url.Values) otellog.Value {
	if len(query) == 0 {
		return otellog.MapValue()
	}

	items := make([]otellog.KeyValue, 0, len(query))
	for key, values := range query {
		items = append(items, otellog.KeyValue{Key: key, Value: stringSliceLogValue(values)})
	}
	return otellog.MapValue(items...)
}

func stringSliceLogValue(values []string) otellog.Value {
	items := make([]otellog.Value, 0, len(values))
	for _, value := range values {
		items = append(items, otellog.StringValue(value))
	}
	return otellog.SliceValue(items...)
}
