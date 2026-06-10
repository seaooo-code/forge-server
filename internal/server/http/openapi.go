package httpserver

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
)

func registerOpenAPIRoutes(e *echo.Echo) {
	e.GET("/openapi.json", func(c echo.Context) error {
		return c.JSON(http.StatusOK, openAPIDocument())
	})
}

func openAPIDocument() *openapi3.T {
	return &openapi3.T{
		OpenAPI: "3.0.3",
		Info: &openapi3.Info{
			Title:       "forge-server API",
			Description: "RN Bundle platform backend API.",
			Version:     "0.1.0",
		},
		Servers: openapi3.Servers{
			{
				URL:         "/",
				Description: "Current host",
			},
		},
		Tags: openapi3.Tags{
			{
				Name:        "System",
				Description: "Service health and diagnostics.",
			},
			{
				Name:        "Environments",
				Description: "Runtime environment management.",
			},
		},
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/healthz", &openapi3.PathItem{
				Get: operation("getHealth", "Health check", []string{"System"}, responses(
					response(http.StatusOK, "Service is alive.", schemaRef("HealthResponse")),
				)),
			}),
			openapi3.WithPath("/readyz", &openapi3.PathItem{
				Get: operation("getReadiness", "Readiness check", []string{"System"}, responses(
					response(http.StatusOK, "Service dependencies are ready.", schemaRef("ReadinessResponse")),
					response(http.StatusServiceUnavailable, "One or more dependencies are unavailable.", schemaRef("ReadinessResponse")),
				)),
			}),
			openapi3.WithPath("/api/v1/ping", &openapi3.PathItem{
				Get: operation("ping", "API smoke check", []string{"System"}, responses(
					response(http.StatusOK, "API is reachable.", schemaRef("PingResponse")),
				)),
			}),
			openapi3.WithPath("/api/v1/environments", &openapi3.PathItem{
				Get: operation("listEnvironments", "List environments", []string{"Environments"}, responses(
					response(http.StatusOK, "Environment list.", schemaRef("EnvironmentsResponse")),
					response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
				)),
			}),
			openapi3.WithPath("/api/v1/environments/{code}", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("getEnvironmentDetail", "Get environment detail", []string{"Environments"}, responses(
						response(http.StatusOK, "Environment detail.", schemaRef("EnvironmentDetailResponse")),
						response(http.StatusNotFound, "Environment not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = openapi3.Parameters{
						{
							Value: openapi3.NewPathParameter("code").
								WithDescription("Environment code.").
								WithSchema(enumStringSchema("Environment code.", "dev", "staging", "preview", "preProduct", "product")),
						},
					}
					return op
				}(),
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{
				"ErrorResponse":             schemaValue(errorResponseSchema()),
				"HealthResponse":            schemaValue(healthResponseSchema()),
				"ReadinessResponse":         schemaValue(readinessResponseSchema()),
				"PingResponse":              schemaValue(pingResponseSchema()),
				"Environment":               schemaValue(environmentSchema()),
				"EnvironmentMetric":         schemaValue(environmentMetricSchema()),
				"EnvironmentGuardrail":      schemaValue(environmentGuardrailSchema()),
				"EnvironmentTimelineEvent":  schemaValue(environmentTimelineEventSchema()),
				"EnvironmentDetail":         schemaValue(environmentDetailSchema()),
				"EnvironmentsResponse":      schemaValue(environmentsResponseSchema()),
				"EnvironmentDetailResponse": schemaValue(environmentDetailResponseSchema()),
			},
		},
	}
}

func operation(id string, summary string, tags []string, responses *openapi3.Responses) *openapi3.Operation {
	return &openapi3.Operation{
		OperationID: id,
		Summary:     summary,
		Tags:        tags,
		Responses:   responses,
	}
}

func responses(items ...openapi3.NewResponsesOption) *openapi3.Responses {
	return openapi3.NewResponses(items...)
}

func response(status int, description string, schema *openapi3.SchemaRef) openapi3.NewResponsesOption {
	return openapi3.WithStatus(status, &openapi3.ResponseRef{
		Value: openapi3.NewResponse().
			WithDescription(description).
			WithJSONSchemaRef(schema),
	})
}

func schemaRef(name string) *openapi3.SchemaRef {
	return openapi3.NewSchemaRef("#/components/schemas/"+name, nil)
}

func schemaValue(schema *openapi3.Schema) *openapi3.SchemaRef {
	return openapi3.NewSchemaRef("", schema)
}

func errorResponseSchema() *openapi3.Schema {
	return objectSchema("error").
		WithProperty("error", stringSchema("Error message."))
}

func healthResponseSchema() *openapi3.Schema {
	return objectSchema("status").
		WithProperty("status", stringSchema("Health status."))
}

func readinessResponseSchema() *openapi3.Schema {
	return objectSchema("status").
		WithProperty("status", stringSchema("Readiness status.")).
		WithProperty("checks", openapi3.NewObjectSchema().
			WithAdditionalProperties(stringSchema("Dependency check result.")))
}

func pingResponseSchema() *openapi3.Schema {
	return objectSchema("app", "env", "status").
		WithProperty("app", stringSchema("Application name.")).
		WithProperty("env", stringSchema("Runtime environment.")).
		WithProperty("status", stringSchema("API status."))
}

func environmentSchema() *openapi3.Schema {
	return objectSchema(
		"id",
		"code",
		"name",
		"status",
		"tone",
		"description",
		"currentCombination",
		"traffic",
		"updatedAt",
	).
		WithProperty("id", enumStringSchema("Environment id.", "dev", "staging", "preview", "preProduct", "product")).
		WithProperty("code", enumStringSchema("Short environment code.", "DEV", "STAGING", "PREVIEW", "PREPRODUCT", "PRODUCT")).
		WithProperty("name", stringSchema("Environment display name.")).
		WithProperty("status", enumStringSchema("Environment status.", "健康", "灰度中")).
		WithProperty("tone", enumStringSchema("UI tone.", "amber", "blue", "emerald", "rose", "sky")).
		WithProperty("description", stringSchema("Environment description.")).
		WithProperty("currentCombination", stringSchema("Current bundle combination name.")).
		WithProperty("traffic", stringSchema("Traffic label.")).
		WithProperty("updatedAt", dateTimeSchema("Last update time."))
}

func environmentMetricSchema() *openapi3.Schema {
	return objectSchema("id", "label", "value", "detail", "tone").
		WithProperty("id", stringSchema("Metric id.")).
		WithProperty("label", stringSchema("Metric label.")).
		WithProperty("value", stringSchema("Metric value.")).
		WithProperty("detail", stringSchema("Metric detail.")).
		WithProperty("tone", enumStringSchema("UI tone.", "amber", "blue", "emerald", "rose", "slate"))
}

func environmentGuardrailSchema() *openapi3.Schema {
	return objectSchema("id", "label", "value", "status").
		WithProperty("id", stringSchema("Guardrail id.")).
		WithProperty("label", stringSchema("Guardrail label.")).
		WithProperty("value", stringSchema("Guardrail result detail.")).
		WithProperty("status", enumStringSchema("Guardrail status.", "通过", "关注"))
}

func environmentTimelineEventSchema() *openapi3.Schema {
	return objectSchema("id", "time", "action", "operator", "status", "detail").
		WithProperty("id", stringSchema("Timeline event id.")).
		WithProperty("time", stringSchema("Display time.")).
		WithProperty("action", stringSchema("Action label.")).
		WithProperty("operator", stringSchema("Operator name.")).
		WithProperty("status", enumStringSchema("Timeline status.", "成功", "进行中", "等待")).
		WithProperty("detail", stringSchema("Timeline event detail."))
}

func environmentDetailSchema() *openapi3.Schema {
	return objectSchema(
		"id",
		"endpoint",
		"cdnDomain",
		"branchPolicy",
		"releasePolicy",
		"currentRelease",
		"bundleCount",
		"totalSize",
		"healthScore",
		"metrics",
		"guardrails",
		"timeline",
	).
		WithProperty("id", enumStringSchema("Environment id.", "dev", "staging", "preview", "preProduct", "product")).
		WithProperty("endpoint", stringSchema("Runtime endpoint.")).
		WithProperty("cdnDomain", stringSchema("CDN domain.")).
		WithProperty("branchPolicy", stringSchema("Branch policy.")).
		WithProperty("releasePolicy", stringSchema("Release policy.")).
		WithProperty("currentRelease", stringSchema("Current release id or label.")).
		WithProperty("bundleCount", int32Schema("Bundle count.")).
		WithProperty("totalSize", stringSchema("Total bundle size label.")).
		WithProperty("healthScore", stringSchema("Health score label.")).
		WithPropertyRef("metrics", arraySchemaRef("EnvironmentMetric")).
		WithPropertyRef("guardrails", arraySchemaRef("EnvironmentGuardrail")).
		WithPropertyRef("timeline", arraySchemaRef("EnvironmentTimelineEvent"))
}

func environmentsResponseSchema() *openapi3.Schema {
	return objectSchema("environments").
		WithPropertyRef("environments", arraySchemaRef("Environment"))
}

func environmentDetailResponseSchema() *openapi3.Schema {
	return objectSchema("environment", "detail").
		WithPropertyRef("environment", schemaRef("Environment")).
		WithPropertyRef("detail", schemaRef("EnvironmentDetail"))
}

func objectSchema(required ...string) *openapi3.Schema {
	return openapi3.NewObjectSchema().WithRequired(required)
}

func stringSchema(description string) *openapi3.Schema {
	schema := openapi3.NewStringSchema()
	schema.Description = description
	return schema
}

func enumStringSchema(description string, values ...string) *openapi3.Schema {
	enumValues := make([]any, 0, len(values))
	for _, value := range values {
		enumValues = append(enumValues, value)
	}
	return stringSchema(description).WithEnum(enumValues...)
}

func dateTimeSchema(description string) *openapi3.Schema {
	schema := openapi3.NewDateTimeSchema()
	schema.Description = description
	return schema
}

func int32Schema(description string) *openapi3.Schema {
	schema := openapi3.NewInt32Schema()
	schema.Description = description
	return schema
}

func arraySchemaRef(itemSchema string) *openapi3.SchemaRef {
	schema := openapi3.NewArraySchema()
	schema.Items = schemaRef(itemSchema)
	return schemaValue(schema)
}
