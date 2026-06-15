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
			{
				Name:        "Applications",
				Description: "Host application management.",
			},
			{
				Name:        "Storage",
				Description: "S3-compatible storage configuration management.",
			},
			{
				Name:        "Auth",
				Description: "Authentication integrations.",
			},
			{
				Name:        "Feishu",
				Description: "Feishu contact integrations.",
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
				Post: func() *openapi3.Operation {
					op := operation("createEnvironment", "Create environment", []string{"Environments"}, responses(
						response(http.StatusCreated, "Environment created.", schemaRef("EnvironmentDetailResponse")),
						response(http.StatusBadRequest, "Invalid environment request.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.RequestBody = requestBody("Environment creation payload.", schemaRef("EnvironmentCreateRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/environments/{code}", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("getEnvironmentDetail", "Get environment detail", []string{"Environments"}, responses(
						response(http.StatusOK, "Environment detail.", schemaRef("EnvironmentDetailResponse")),
						response(http.StatusNotFound, "Environment not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = environmentCodeParameters()
					return op
				}(),
				Patch: func() *openapi3.Operation {
					op := operation("updateEnvironment", "Update environment", []string{"Environments"}, responses(
						response(http.StatusOK, "Environment updated.", schemaRef("EnvironmentDetailResponse")),
						response(http.StatusBadRequest, "Invalid environment request.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Environment not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = environmentCodeParameters()
					op.RequestBody = requestBody("Environment update payload.", schemaRef("EnvironmentUpdateRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/applications", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("listApplications", "List applications", []string{"Applications"}, responses(
						response(http.StatusOK, "Application list.", schemaRef("ApplicationsResponse")),
						response(http.StatusBadRequest, "Invalid application list filter.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = applicationListParameters()
					return op
				}(),
				Post: func() *openapi3.Operation {
					op := operation("createApplication", "Create application", []string{"Applications"}, responses(
						response(http.StatusCreated, "Application created.", schemaRef("ApplicationDetailResponse")),
						response(http.StatusBadRequest, "Invalid application request.", schemaRef("ErrorResponse")),
						response(http.StatusConflict, "Application slug already exists.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.RequestBody = requestBody("Application create payload.", schemaRef("ApplicationCreateRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/applications/options", &openapi3.PathItem{
				Get: operation("getApplicationOptions", "Get application form options", []string{"Applications"}, responses(
					response(http.StatusOK, "Application form options.", schemaRef("ApplicationOptions")),
					response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
				)),
			}),
			openapi3.WithPath("/api/v1/applications/{slug}", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("getApplicationDetail", "Get application detail", []string{"Applications"}, responses(
						response(http.StatusOK, "Application detail.", schemaRef("ApplicationDetailResponse")),
						response(http.StatusNotFound, "Application not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = applicationSlugParameters()
					return op
				}(),
				Patch: func() *openapi3.Operation {
					op := operation("updateApplication", "Update application", []string{"Applications"}, responses(
						response(http.StatusOK, "Application updated.", schemaRef("ApplicationDetailResponse")),
						response(http.StatusBadRequest, "Invalid application request.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Application not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = applicationSlugParameters()
					op.RequestBody = requestBody("Application update payload. Slug is immutable.", schemaRef("ApplicationUpdateRequest"))
					return op
				}(),
				Delete: func() *openapi3.Operation {
					op := operation("deleteApplication", "Soft delete application", []string{"Applications"}, responses(
						response(http.StatusOK, "Application was marked offline.", schemaRef("ApplicationDetailResponse")),
						response(http.StatusNotFound, "Application not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = applicationSlugParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/storage/configurations", &openapi3.PathItem{
				Get: operation("listStorageConfigurations", "List storage configurations", []string{"Storage"}, responses(
					response(http.StatusOK, "Storage configuration list.", schemaRef("StorageConfigurationsResponse")),
					response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
				)),
				Post: func() *openapi3.Operation {
					op := operation("createStorageConfiguration", "Create storage configuration", []string{"Storage"}, responses(
						response(http.StatusCreated, "Storage configuration created.", schemaRef("StorageConfigurationDetailResponse")),
						response(http.StatusBadRequest, "Invalid storage configuration request.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.RequestBody = requestBody("Storage configuration create payload.", schemaRef("StorageConfigurationCreateRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/storage/configurations/test", &openapi3.PathItem{
				Post: func() *openapi3.Operation {
					op := operation("testStorageConfiguration", "Test storage configuration connectivity", []string{"Storage"}, responses(
						response(http.StatusOK, "Storage configuration test passed.", schemaRef("StorageConfigurationTestResponse")),
						response(http.StatusBadRequest, "Invalid storage configuration request.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Stored storage configuration not found.", schemaRef("ErrorResponse")),
						response(http.StatusBadGateway, "Storage backend could not be reached.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.RequestBody = requestBody("Storage configuration test payload. Provide id to reuse stored credentials when secret fields are empty.", schemaRef("StorageConfigurationTestRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/storage/configurations/{id}", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("getStorageConfiguration", "Get storage configuration", []string{"Storage"}, responses(
						response(http.StatusOK, "Storage configuration detail.", schemaRef("StorageConfigurationDetailResponse")),
						response(http.StatusNotFound, "Storage configuration not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = storageConfigurationIDParameters()
					return op
				}(),
				Patch: func() *openapi3.Operation {
					op := operation("updateStorageConfiguration", "Update storage configuration", []string{"Storage"}, responses(
						response(http.StatusOK, "Storage configuration updated.", schemaRef("StorageConfigurationDetailResponse")),
						response(http.StatusBadRequest, "Invalid storage configuration request.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Storage configuration not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = storageConfigurationIDParameters()
					op.RequestBody = requestBody("Storage configuration update payload. Empty secret fields keep current credentials.", schemaRef("StorageConfigurationUpdateRequest"))
					return op
				}(),
				Delete: func() *openapi3.Operation {
					op := operation("deleteStorageConfiguration", "Delete storage configuration", []string{"Storage"}, responses(
						response(http.StatusOK, "Storage configuration deleted.", schemaRef("StorageConfigurationDetailResponse")),
						response(http.StatusNotFound, "Storage configuration not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = storageConfigurationIDParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/auth/feishu/authorize", &openapi3.PathItem{
				Get: operation("authorizeFeishu", "Redirect to Feishu OAuth authorization page", []string{"Auth"}, responses(
					responseNoBody(http.StatusFound, "Redirect to Feishu authorization page."),
					response(http.StatusServiceUnavailable, "Feishu auth is not configured.", schemaRef("ErrorResponse")),
					response(http.StatusBadGateway, "Feishu authorization URL could not be created.", schemaRef("ErrorResponse")),
				)),
			}),
			openapi3.WithPath("/api/v1/auth/feishu/callback", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("completeFeishuAuthorization", "Complete Feishu OAuth authorization", []string{"Auth"}, responses(
						response(http.StatusOK, "Feishu user authorized.", schemaRef("FeishuAuthResponse")),
						response(http.StatusBadRequest, "Invalid authorization callback.", schemaRef("ErrorResponse")),
						response(http.StatusBadGateway, "Feishu authorization failed.", schemaRef("ErrorResponse")),
						response(http.StatusServiceUnavailable, "Feishu auth is not configured.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = openapi3.Parameters{
						{
							Value: openapi3.NewQueryParameter("code").
								WithDescription("Feishu OAuth authorization code.").
								WithSchema(stringSchema("Authorization code.")),
						},
						{
							Value: openapi3.NewQueryParameter("state").
								WithDescription("OAuth state returned by Feishu.").
								WithSchema(stringSchema("OAuth state.")),
						},
						{
							Value: openapi3.NewQueryParameter("error").
								WithDescription("Authorization error returned by Feishu when user denies consent.").
								WithSchema(stringSchema("Authorization error.")),
						},
					}
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/auth/session", &openapi3.PathItem{
				Get: operation("getAuthSession", "Get current auth session and refresh token when needed", []string{"Auth"}, responses(
					response(http.StatusOK, "Current auth session.", schemaRef("FeishuAuthResponse")),
					response(http.StatusUnauthorized, "Session is missing or expired.", schemaRef("ErrorResponse")),
					response(http.StatusBadGateway, "Feishu token refresh failed.", schemaRef("ErrorResponse")),
					response(http.StatusServiceUnavailable, "Feishu auth is not configured.", schemaRef("ErrorResponse")),
					response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
				)),
			}),
			openapi3.WithPath("/api/v1/auth/logout", &openapi3.PathItem{
				Post: operation("logoutAuthSession", "Logout current auth session", []string{"Auth"}, responses(
					responseNoBody(http.StatusNoContent, "Current auth session was removed."),
					response(http.StatusServiceUnavailable, "Feishu auth is unavailable.", schemaRef("ErrorResponse")),
					response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
				)),
			}),
			openapi3.WithPath("/api/v1/feishu/users/search", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("searchFeishuUsers", "Search Feishu users with current user access token", []string{"Feishu"}, responses(
						response(http.StatusOK, "Feishu user search result.", schemaRef("FeishuUserSearchResponse")),
						response(http.StatusBadRequest, "Invalid search request.", schemaRef("ErrorResponse")),
						response(http.StatusUnauthorized, "Session is missing or expired.", schemaRef("ErrorResponse")),
						response(http.StatusBadGateway, "Feishu API request failed.", schemaRef("ErrorResponse")),
						response(http.StatusServiceUnavailable, "Feishu integration is not configured.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = openapi3.Parameters{
						{
							Value: openapi3.NewQueryParameter("query").
								WithDescription("Search keyword, such as name, email, mobile or user id.").
								WithSchema(stringSchema("Search keyword.")),
						},
						{
							Value: openapi3.NewQueryParameter("page_size").
								WithDescription("Result size, min 1, max 200, default 20.").
								WithSchema(int32Schema("Page size.")),
						},
						{
							Value: openapi3.NewQueryParameter("page_token").
								WithDescription("Pagination token returned by the previous search response.").
								WithSchema(stringSchema("Page token.")),
						},
					}
					return op
				}(),
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{
				"ErrorResponse":                      schemaValue(errorResponseSchema()),
				"HealthResponse":                     schemaValue(healthResponseSchema()),
				"ReadinessResponse":                  schemaValue(readinessResponseSchema()),
				"PingResponse":                       schemaValue(pingResponseSchema()),
				"Environment":                        schemaValue(environmentSchema()),
				"EnvironmentCreateRequest":           schemaValue(environmentCreateRequestSchema()),
				"EnvironmentUpdateRequest":           schemaValue(environmentCreateRequestSchema()),
				"EnvironmentsResponse":               schemaValue(environmentsResponseSchema()),
				"EnvironmentDetailResponse":          schemaValue(environmentDetailResponseSchema()),
				"Application":                        schemaValue(applicationSchema()),
				"ApplicationOption":                  schemaValue(applicationOptionSchema()),
				"ApplicationOptions":                 schemaValue(applicationOptionsSchema()),
				"ApplicationSummary":                 schemaValue(applicationSummarySchema()),
				"ApplicationMetric":                  schemaValue(applicationMetricSchema()),
				"ApplicationEnvironment":             schemaValue(applicationEnvironmentSchema()),
				"ApplicationModule":                  schemaValue(applicationModuleSchema()),
				"ApplicationMember":                  schemaValue(applicationMemberSchema()),
				"ApplicationPipeline":                schemaValue(applicationPipelineSchema()),
				"ApplicationDetail":                  schemaValue(applicationDetailSchema()),
				"ApplicationsResponse":               schemaValue(applicationsResponseSchema()),
				"ApplicationDetailResponse":          schemaValue(applicationDetailResponseSchema()),
				"ApplicationCreateRequest":           schemaValue(applicationRequestSchema(true)),
				"ApplicationUpdateRequest":           schemaValue(applicationRequestSchema(false)),
				"ApplicationEnvironmentRequest":      schemaValue(applicationEnvironmentRequestSchema()),
				"ApplicationMemberRequest":           schemaValue(applicationMemberRequestSchema()),
				"StorageConfiguration":               schemaValue(storageConfigurationSchema()),
				"StorageConfigurationsResponse":      schemaValue(storageConfigurationsResponseSchema()),
				"StorageConfigurationDetailResponse": schemaValue(storageConfigurationDetailResponseSchema()),
				"StorageConfigurationCreateRequest":  schemaValue(storageConfigurationRequestSchema(true)),
				"StorageConfigurationUpdateRequest":  schemaValue(storageConfigurationRequestSchema(false)),
				"StorageConfigurationTestRequest":    schemaValue(storageConfigurationTestRequestSchema()),
				"StorageConfigurationTestResponse":   schemaValue(storageConfigurationTestResponseSchema()),
				"FeishuAuthResponse":                 schemaValue(feishuAuthResponseSchema()),
				"FeishuToken":                        schemaValue(feishuTokenSchema()),
				"FeishuUser":                         schemaValue(feishuUserSchema()),
				"FeishuUserSearchItem":               schemaValue(feishuUserSearchItemSchema()),
				"FeishuUserSearchResponse":           schemaValue(feishuUserSearchResponseSchema()),
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

func responseNoBody(status int, description string) openapi3.NewResponsesOption {
	return openapi3.WithStatus(status, &openapi3.ResponseRef{
		Value: openapi3.NewResponse().WithDescription(description),
	})
}

func requestBody(description string, schema *openapi3.SchemaRef) *openapi3.RequestBodyRef {
	return &openapi3.RequestBodyRef{
		Value: openapi3.NewRequestBody().
			WithDescription(description).
			WithRequired(true).
			WithJSONSchemaRef(schema),
	}
}

func schemaRef(name string) *openapi3.SchemaRef {
	return openapi3.NewSchemaRef("#/components/schemas/"+name, nil)
}

func schemaValue(schema *openapi3.Schema) *openapi3.SchemaRef {
	return openapi3.NewSchemaRef("", schema)
}

func errorResponseSchema() *openapi3.Schema {
	return objectSchema("error").
		WithProperty("error", stringSchema("Error message.")).
		WithProperty("feishuCode", int32Schema("Feishu API error code.")).
		WithProperty("feishuEndpoint", stringSchema("Feishu API endpoint.")).
		WithProperty("feishuMsg", stringSchema("Feishu API error message."))
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
		"name",
		"status",
		"tone",
		"description",
		"storageConfigurationId",
		"storageConfigurationName",
		"updatedAt",
	).
		WithProperty("id", stringSchema("Environment id.")).
		WithProperty("name", stringSchema("Environment display name.")).
		WithProperty("status", enumStringSchema("Environment status.", "健康", "灰度中")).
		WithProperty("tone", enumStringSchema("UI tone.", "amber", "blue", "emerald", "rose", "sky")).
		WithProperty("description", stringSchema("Environment description.")).
		WithProperty("storageConfigurationId", stringSchema("Linked storage configuration id.")).
		WithProperty("storageConfigurationName", stringSchema("Linked storage configuration display name.")).
		WithProperty("updatedAt", dateTimeSchema("Last update time."))
}

func environmentCreateRequestSchema() *openapi3.Schema {
	return objectSchema("code", "name", "status", "tone", "storageConfigurationId").
		WithProperty("code", stringSchema("Environment id.")).
		WithProperty("name", stringSchema("Environment display name.")).
		WithProperty("status", enumStringSchema("Environment status code.", "healthy", "gray")).
		WithProperty("tone", enumStringSchema("UI tone.", "amber", "blue", "emerald", "rose", "sky")).
		WithProperty("description", stringSchema("Environment description.")).
		WithProperty("storageConfigurationId", stringSchema("Linked storage configuration id."))
}

func environmentDetailSchema() *openapi3.Schema {
	return environmentSchema()
}

func environmentsResponseSchema() *openapi3.Schema {
	return objectSchema("environments").
		WithPropertyRef("environments", arraySchemaRef("Environment"))
}

func environmentDetailResponseSchema() *openapi3.Schema {
	return objectSchema("environment").
		WithPropertyRef("environment", schemaRef("Environment"))
}

func environmentCodeParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewPathParameter("code").
				WithDescription("Environment id.").
				WithSchema(stringSchema("Environment id.")),
		},
	}
}

func applicationSlugParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewPathParameter("slug").
				WithDescription("Immutable application slug.").
				WithSchema(stringSchema("Application slug.")),
		},
	}
}

func storageConfigurationIDParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewPathParameter("id").
				WithDescription("Storage configuration id.").
				WithSchema(stringSchema("Storage configuration id.")),
		},
	}
}

func applicationListParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewQueryParameter("platform").
				WithDescription("Platform filter. Matches a supported platform token, such as iOS or Android.").
				WithSchema(stringSchema("Platform filter.")),
		},
		{
			Value: openapi3.NewQueryParameter("status").
				WithDescription("Application status filter.").
				WithSchema(enumStringSchema("Application status code.", "running", "maintenance", "offline")),
		},
		{
			Value: openapi3.NewQueryParameter("owner").
				WithDescription("Owner name filter.").
				WithSchema(stringSchema("Owner name filter.")),
		},
		{
			Value: openapi3.NewQueryParameter("keyword").
				WithDescription("Fuzzy application name keyword.").
				WithSchema(stringSchema("Application name keyword.")),
		},
	}
}

func applicationSchema() *openapi3.Schema {
	return objectSchema(
		"id",
		"name",
		"slug",
		"platform",
		"ownerUserId",
		"owner",
		"ownerEmail",
		"ownerAvatarUrl",
		"ownerDepartmentIds",
		"moduleCount",
		"environmentCount",
		"createdAt",
		"status",
	).
		WithProperty("id", stringSchema("Application id.")).
		WithProperty("name", stringSchema("Application display name.")).
		WithProperty("slug", stringSchema("Application immutable slug.")).
		WithProperty("platform", stringSchema("Supported platforms.")).
		WithProperty("ownerUserId", stringSchema("Application owner user id.")).
		WithProperty("owner", stringSchema("Application owner account.")).
		WithProperty("ownerEmail", stringSchema("Application owner email.")).
		WithProperty("ownerAvatarUrl", stringSchema("Application owner avatar url.")).
		WithProperty("ownerDepartmentIds", stringArraySchema("Feishu department ids of the owner.")).
		WithProperty("moduleCount", int32Schema("Module count.")).
		WithProperty("environmentCount", int32Schema("Enabled environment count.")).
		WithProperty("createdAt", stringSchema("Application creation date.")).
		WithProperty("status", enumStringSchema("Application status.", "运行中", "维护中", "已下线"))
}

func applicationOptionSchema() *openapi3.Schema {
	return objectSchema("label", "value").
		WithProperty("label", stringSchema("Display label.")).
		WithProperty("value", stringSchema("Option value."))
}

func applicationOptionsSchema() *openapi3.Schema {
	return objectSchema("applicationStatuses", "environments", "owners", "platforms").
		WithPropertyRef("applicationStatuses", arraySchemaRef("ApplicationOption")).
		WithPropertyRef("environments", arraySchemaRef("ApplicationOption")).
		WithPropertyRef("owners", arraySchemaRef("ApplicationOption")).
		WithPropertyRef("platforms", arraySchemaRef("ApplicationOption"))
}

func applicationSummarySchema() *openapi3.Schema {
	return objectSchema("total", "running", "maintenance", "offline").
		WithProperty("total", int32Schema("Total applications.")).
		WithProperty("running", int32Schema("Running applications.")).
		WithProperty("maintenance", int32Schema("Applications under maintenance.")).
		WithProperty("offline", int32Schema("Offline applications."))
}

func applicationMetricSchema() *openapi3.Schema {
	return objectSchema("id", "label", "value", "detail", "tone").
		WithProperty("id", stringSchema("Metric id.")).
		WithProperty("label", stringSchema("Metric label.")).
		WithProperty("value", stringSchema("Metric value.")).
		WithProperty("detail", stringSchema("Metric detail.")).
		WithProperty("tone", enumStringSchema("Metric tone.", "amber", "blue", "emerald", "rose", "slate"))
}

func applicationEnvironmentSchema() *openapi3.Schema {
	return objectSchema("id", "environment", "traffic", "updatedAt", "status").
		WithProperty("id", stringSchema("Environment binding id.")).
		WithProperty("environment", stringSchema("Environment code.")).
		WithProperty("traffic", stringSchema("Traffic label.")).
		WithProperty("updatedAt", dateTimeSchema("Last update time.")).
		WithProperty("status", enumStringSchema("Environment binding status.", "健康", "灰度中", "维护中"))
}

func applicationModuleSchema() *openapi3.Schema {
	return objectSchema("id", "name", "kind", "repository", "owner", "latestVersion", "pipeline", "status").
		WithProperty("id", stringSchema("Module id.")).
		WithProperty("name", stringSchema("Module name.")).
		WithProperty("kind", enumStringSchema("Module kind.", "Common", "Feature")).
		WithProperty("repository", stringSchema("Module repository.")).
		WithProperty("owner", stringSchema("Module owner.")).
		WithProperty("latestVersion", stringSchema("Latest online version.")).
		WithProperty("pipeline", stringSchema("Pipeline name.")).
		WithProperty("status", enumStringSchema("Module status.", "活跃", "冻结"))
}

func applicationMemberSchema() *openapi3.Schema {
	return objectSchema("id", "name", "role", "team").
		WithProperty("id", stringSchema("Member id.")).
		WithProperty("name", stringSchema("Member account or name.")).
		WithProperty("role", enumStringSchema("Member role.", "Owner", "Developer", "QA", "Release")).
		WithProperty("team", stringSchema("Member team."))
}

func applicationPipelineSchema() *openapi3.Schema {
	return objectSchema("id", "runId", "moduleName", "branch", "status", "duration", "operator", "startedAt").
		WithProperty("id", stringSchema("Pipeline record id.")).
		WithProperty("runId", stringSchema("Pipeline run id.")).
		WithProperty("moduleName", stringSchema("Module name.")).
		WithProperty("branch", stringSchema("Git branch.")).
		WithProperty("status", enumStringSchema("Pipeline status.", "success", "running", "failed", "queued")).
		WithProperty("duration", stringSchema("Pipeline duration.")).
		WithProperty("operator", stringSchema("Operator account.")).
		WithProperty("startedAt", stringSchema("Pipeline start display time."))
}

func applicationDetailSchema() *openapi3.Schema {
	return objectSchema(
		"id",
		"appKey",
		"slug",
		"name",
		"description",
		"ownerUserId",
		"owner",
		"ownerEmail",
		"ownerAvatarUrl",
		"ownerDepartmentIds",
		"metrics",
		"environments",
		"modules",
		"members",
		"pipelines",
	).
		WithProperty("id", stringSchema("Application id.")).
		WithProperty("appKey", stringSchema("Application key.")).
		WithProperty("slug", stringSchema("Application slug.")).
		WithProperty("name", stringSchema("Application name.")).
		WithProperty("description", stringSchema("Application description.")).
		WithProperty("ownerUserId", stringSchema("Application owner user id.")).
		WithProperty("owner", stringSchema("Application owner account.")).
		WithProperty("ownerEmail", stringSchema("Application owner email.")).
		WithProperty("ownerAvatarUrl", stringSchema("Application owner avatar url.")).
		WithProperty("ownerDepartmentIds", stringArraySchema("Feishu department ids of the owner.")).
		WithPropertyRef("metrics", arraySchemaRef("ApplicationMetric")).
		WithPropertyRef("environments", arraySchemaRef("ApplicationEnvironment")).
		WithPropertyRef("modules", arraySchemaRef("ApplicationModule")).
		WithPropertyRef("members", arraySchemaRef("ApplicationMember")).
		WithPropertyRef("pipelines", arraySchemaRef("ApplicationPipeline"))
}

func applicationsResponseSchema() *openapi3.Schema {
	return objectSchema("applications", "summary").
		WithPropertyRef("applications", arraySchemaRef("Application")).
		WithPropertyRef("summary", schemaRef("ApplicationSummary"))
}

func applicationDetailResponseSchema() *openapi3.Schema {
	return objectSchema("application", "detail").
		WithPropertyRef("application", schemaRef("Application")).
		WithPropertyRef("detail", schemaRef("ApplicationDetail"))
}

func applicationRequestSchema(includeSlug bool) *openapi3.Schema {
	required := []string{
		"name",
		"platform",
		"owner",
		"description",
		"environments",
	}
	if includeSlug {
		required = append([]string{"slug"}, required...)
	}

	schema := objectSchema(required...).
		WithProperty("name", stringSchema("Application display name.")).
		WithProperty("platform", stringSchema("Supported platforms.")).
		WithProperty("ownerUserId", stringSchema("Owner user id. Use the id returned by Feishu user search when available.")).
		WithProperty("owner", stringSchema("Owner account.")).
		WithProperty("ownerEmail", stringSchema("Owner email.")).
		WithProperty("ownerAvatarUrl", stringSchema("Owner avatar url.")).
		WithProperty("ownerDepartmentIds", stringArraySchema("Feishu department ids of the owner.")).
		WithProperty("ownerFeishuUnionId", stringSchema("Owner Feishu union_id.")).
		WithProperty("ownerFeishuOpenId", stringSchema("Owner Feishu open_id.")).
		WithProperty("ownerFeishuUserId", stringSchema("Owner Feishu user_id.")).
		WithProperty("description", stringSchema("Application description.")).
		WithProperty("status", enumStringSchema("Application status code.", "running", "maintenance", "offline")).
		WithPropertyRef("environments", arraySchemaRef("ApplicationEnvironmentRequest")).
		WithPropertyRef("members", arraySchemaRef("ApplicationMemberRequest"))
	if includeSlug {
		schema.WithProperty("slug", stringSchema("Immutable application slug."))
	}
	return schema
}

func applicationEnvironmentRequestSchema() *openapi3.Schema {
	return objectSchema("environment").
		WithProperty("environment", stringSchema("Environment code."))
}

func applicationMemberRequestSchema() *openapi3.Schema {
	return objectSchema("name", "role", "team").
		WithProperty("name", stringSchema("Member account or name.")).
		WithProperty("role", enumStringSchema("Member role.", "Owner", "Developer", "QA", "Release")).
		WithProperty("team", stringSchema("Member team."))
}

func storageConfigurationSchema() *openapi3.Schema {
	return objectSchema(
		"id",
		"name",
		"adapter",
		"bucket",
		"endpoint",
		"lastVerifiedAt",
		"ownerUserId",
		"owner",
		"ownerEmail",
		"ownerAvatarUrl",
		"publicBaseUrl",
		"region",
		"status",
	).
		WithProperty("id", stringSchema("Storage configuration id.")).
		WithProperty("name", stringSchema("Storage configuration display name.")).
		WithProperty("adapter", enumStringSchema("Storage adapter.", "S3")).
		WithProperty("bucket", stringSchema("S3-compatible bucket name.")).
		WithProperty("endpoint", stringSchema("S3-compatible endpoint.")).
		WithProperty("lastVerifiedAt", stringSchema("Last verification display time.")).
		WithProperty("ownerUserId", stringSchema("Configuration owner user id.")).
		WithProperty("owner", stringSchema("Configuration owner.")).
		WithProperty("ownerEmail", stringSchema("Configuration owner email.")).
		WithProperty("ownerAvatarUrl", stringSchema("Configuration owner avatar url.")).
		WithProperty("publicBaseUrl", stringSchema("Public base URL for object distribution.")).
		WithProperty("region", stringSchema("Provider region.")).
		WithProperty("status", enumStringSchema("Storage status label.", "正常", "异常", "已停用"))
}

func storageConfigurationsResponseSchema() *openapi3.Schema {
	return objectSchema("configurations").
		WithPropertyRef("configurations", arraySchemaRef("StorageConfiguration"))
}

func storageConfigurationDetailResponseSchema() *openapi3.Schema {
	return objectSchema("configuration").
		WithPropertyRef("configuration", schemaRef("StorageConfiguration"))
}

func storageConfigurationRequestSchema(includeSecret bool) *openapi3.Schema {
	required := []string{"name", "adapter", "bucket", "endpoint"}
	if includeSecret {
		required = append(required, "accessKeyId", "secretAccessKey")
	}

	return objectSchema(required...).
		WithProperty("name", stringSchema("Storage configuration display name.")).
		WithProperty("adapter", enumStringSchema("Storage adapter.", "S3")).
		WithProperty("bucket", stringSchema("S3-compatible bucket name.")).
		WithProperty("endpoint", stringSchema("S3-compatible endpoint.")).
		WithProperty("ownerUserId", stringSchema("Configuration owner user id. Use the id returned by Feishu user search when available.")).
		WithProperty("owner", stringSchema("Configuration owner.")).
		WithProperty("ownerEmail", stringSchema("Configuration owner email.")).
		WithProperty("ownerAvatarUrl", stringSchema("Configuration owner avatar url.")).
		WithProperty("ownerFeishuUnionId", stringSchema("Owner Feishu union_id.")).
		WithProperty("ownerFeishuOpenId", stringSchema("Owner Feishu open_id.")).
		WithProperty("ownerFeishuUserId", stringSchema("Owner Feishu user_id.")).
		WithProperty("publicBaseUrl", stringSchema("Public base URL for object distribution.")).
		WithProperty("region", stringSchema("Provider region.")).
		WithProperty("status", enumStringSchema("Storage status label.", "正常", "异常", "已停用")).
		WithProperty("accessKeyId", stringSchema("Access key id. Not returned by read APIs.")).
		WithProperty("secretAccessKey", stringSchema("Secret access key. Not returned by read APIs. Empty on update keeps current value."))
}

func storageConfigurationTestRequestSchema() *openapi3.Schema {
	return objectSchema("name", "adapter", "bucket", "endpoint").
		WithProperty("id", stringSchema("Existing storage configuration id. When provided, empty secret fields reuse stored credentials.")).
		WithProperty("name", stringSchema("Storage configuration display name.")).
		WithProperty("adapter", enumStringSchema("Storage adapter.", "S3")).
		WithProperty("bucket", stringSchema("S3-compatible bucket name.")).
		WithProperty("endpoint", stringSchema("S3-compatible endpoint.")).
		WithProperty("ownerUserId", stringSchema("Configuration owner user id. Use the id returned by Feishu user search when available.")).
		WithProperty("owner", stringSchema("Configuration owner.")).
		WithProperty("ownerEmail", stringSchema("Configuration owner email.")).
		WithProperty("ownerAvatarUrl", stringSchema("Configuration owner avatar url.")).
		WithProperty("ownerFeishuUnionId", stringSchema("Owner Feishu union_id.")).
		WithProperty("ownerFeishuOpenId", stringSchema("Owner Feishu open_id.")).
		WithProperty("ownerFeishuUserId", stringSchema("Owner Feishu user_id.")).
		WithProperty("publicBaseUrl", stringSchema("Public base URL for object distribution.")).
		WithProperty("region", stringSchema("Provider region.")).
		WithProperty("status", enumStringSchema("Storage status label.", "正常", "异常", "已停用")).
		WithProperty("accessKeyId", stringSchema("Access key id. Required when id is empty.")).
		WithProperty("secretAccessKey", stringSchema("Secret access key. Required when id is empty."))
}

func storageConfigurationTestResponseSchema() *openapi3.Schema {
	return objectSchema("ok", "testedAt").
		WithProperty("ok", boolSchema("Whether the connectivity test passed.")).
		WithProperty("testedAt", dateTimeSchema("Test completion time."))
}

func feishuAuthResponseSchema() *openapi3.Schema {
	return objectSchema("user", "token").
		WithPropertyRef("user", schemaRef("FeishuUser")).
		WithPropertyRef("token", schemaRef("FeishuToken"))
}

func feishuUserSchema() *openapi3.Schema {
	return objectSchema("unionId").
		WithProperty("unionId", stringSchema("Feishu union_id.")).
		WithProperty("openId", stringSchema("Feishu open_id.")).
		WithProperty("userId", stringSchema("Feishu user_id.")).
		WithProperty("name", stringSchema("User name.")).
		WithProperty("enName", stringSchema("User English name.")).
		WithProperty("avatarUrl", stringSchema("Avatar URL.")).
		WithProperty("avatarThumb", stringSchema("Avatar thumbnail URL.")).
		WithProperty("avatarMiddle", stringSchema("Avatar middle URL.")).
		WithProperty("avatarBig", stringSchema("Avatar big URL.")).
		WithProperty("email", stringSchema("User email.")).
		WithProperty("enterpriseEmail", stringSchema("Enterprise email.")).
		WithProperty("mobile", stringSchema("Mobile phone.")).
		WithProperty("tenantKey", stringSchema("Tenant key.")).
		WithProperty("employeeNo", stringSchema("Employee number."))
}

func feishuTokenSchema() *openapi3.Schema {
	return objectSchema("expiresIn", "tokenType", "hasRefreshToken").
		WithProperty("expiresIn", int32Schema("Access token lifetime in seconds.")).
		WithProperty("refreshTokenExpiresIn", int32Schema("Refresh token lifetime in seconds.")).
		WithProperty("tokenType", stringSchema("Token type.")).
		WithProperty("scope", stringSchema("Granted scopes.")).
		WithProperty("hasRefreshToken", boolSchema("Whether a refresh token was returned."))
}

func feishuUserSearchItemSchema() *openapi3.Schema {
	return objectSchema("id", "name", "departmentIds").
		WithProperty("id", stringSchema("Best available Feishu user id.")).
		WithProperty("unionId", stringSchema("Feishu union_id.")).
		WithProperty("openId", stringSchema("Feishu open_id.")).
		WithProperty("userId", stringSchema("Feishu user_id.")).
		WithProperty("name", stringSchema("User name.")).
		WithProperty("enName", stringSchema("User English name.")).
		WithProperty("avatarUrl", stringSchema("Avatar URL.")).
		WithProperty("email", stringSchema("User email.")).
		WithProperty("mobile", stringSchema("Mobile phone.")).
		WithProperty("departmentIds", stringArraySchema("Feishu department ids."))
}

func feishuUserSearchResponseSchema() *openapi3.Schema {
	return objectSchema("users", "hasMore", "pageToken").
		WithPropertyRef("users", arraySchemaRef("FeishuUserSearchItem")).
		WithProperty("hasMore", boolSchema("Whether more users are available.")).
		WithProperty("pageToken", stringSchema("Token for the next page."))
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

func boolSchema(description string) *openapi3.Schema {
	schema := openapi3.NewBoolSchema()
	schema.Description = description
	return schema
}

func stringArraySchema(description string) *openapi3.Schema {
	schema := openapi3.NewArraySchema()
	schema.Description = description
	schema.Items = schemaValue(stringSchema(""))
	return schema
}

func arraySchemaRef(itemSchema string) *openapi3.SchemaRef {
	schema := openapi3.NewArraySchema()
	schema.Items = schemaRef(itemSchema)
	return schemaValue(schema)
}
