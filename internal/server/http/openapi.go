package httpserver

import (
	"net/http"

	"forge-server/internal/domain"

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
				Name:        "Dashboard",
				Description: "Forge console dashboard aggregation.",
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
				Name:        "Modules",
				Description: "RN module repository binding management.",
			},
			{
				Name:        "Builds",
				Description: "RN JS bundle build record management.",
			},
			{
				Name:        "BuildAdapters",
				Description: "Platform managed build adapters: reusable Dockerfile templates, helper files and parameter definitions.",
			},
			{
				Name:        "Artifacts",
				Description: "RN JS bundle artifact download and application manifest APIs.",
			},
			{
				Name:        "AccessTokens",
				Description: "Artifact access token management.",
			},
			{
				Name:        "Releases",
				Description: "RN JS bundle release routing and artifact resolution.",
			},
			{
				Name:        "CodeSources",
				Description: "Code source connections (GitHub App installations, GitLab access tokens) and repository discovery.",
			},
			{
				Name:        "Webhooks",
				Description: "Code source provider webhook receivers. No console session; verified by each provider.",
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
			openapi3.WithPath("/api/v1/enums", &openapi3.PathItem{
				Get: operation("listEnums", "List global enum options", []string{"System"}, responses(
					response(http.StatusOK, "Global enum options.", schemaRef("EnumsResponse")),
				)),
			}),
			openapi3.WithPath("/api/v1/dashboard", &openapi3.PathItem{
				Get: operation("getDashboard", "Get console dashboard snapshot", []string{"Dashboard"}, responses(
					response(http.StatusOK, "Dashboard snapshot.", schemaRef("DashboardResponse")),
					response(http.StatusUnauthorized, "Console session is required.", schemaRef("ErrorResponse")),
					response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					response(http.StatusServiceUnavailable, "Dashboard service is unavailable.", schemaRef("ErrorResponse")),
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
						response(http.StatusForbidden, "Current user cannot manage environments.", schemaRef("ErrorResponse")),
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
						response(http.StatusForbidden, "Current user cannot manage environments.", schemaRef("ErrorResponse")),
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
						response(http.StatusForbidden, "Current user cannot create applications.", schemaRef("ErrorResponse")),
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
						response(http.StatusForbidden, "Current user cannot update this application.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Application not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = applicationSlugParameters()
					op.RequestBody = requestBody("Application update payload. Slug is immutable.", schemaRef("ApplicationUpdateRequest"))
					return op
				}(),
				Delete: func() *openapi3.Operation {
					op := operation("deleteApplication", "Delete application", []string{"Applications"}, responses(
						responseNoBody(http.StatusNoContent, "Application was deleted."),
						response(http.StatusForbidden, "Current user cannot delete this application.", schemaRef("ErrorResponse")),
						response(http.StatusConflict, "Application is referenced by modules.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Application not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = applicationSlugParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/applications/{slug}/status", &openapi3.PathItem{
				Patch: func() *openapi3.Operation {
					op := operation("updateApplicationStatus", "Update application status", []string{"Applications"}, responses(
						response(http.StatusOK, "Application status updated.", schemaRef("ApplicationDetailResponse")),
						response(http.StatusBadRequest, "Invalid application status request.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot update this application status.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Application not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = applicationSlugParameters()
					op.RequestBody = requestBody("Application status update payload.", schemaRef("ApplicationStatusUpdateRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/applications/{slug}/delete-check", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("checkApplicationDelete", "Check application delete dependencies", []string{"Applications"}, responses(
						response(http.StatusOK, "Application delete dependency check.", schemaRef("ApplicationDeleteCheck")),
						response(http.StatusNotFound, "Application not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = applicationSlugParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/applications/{slug}/members", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("listApplicationMembers", "List application members", []string{"Applications"}, responses(
						response(http.StatusOK, "Application members.", schemaRef("ApplicationMembersResponse")),
						response(http.StatusNotFound, "Application not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = applicationSlugParameters()
					return op
				}(),
				Post: func() *openapi3.Operation {
					op := operation("upsertApplicationMember", "Add or update application member", []string{"Applications"}, responses(
						response(http.StatusOK, "Application member was added or updated.", schemaRef("ApplicationMember")),
						response(http.StatusBadRequest, "Invalid member request.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot manage application members.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Application not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = applicationSlugParameters()
					op.RequestBody = requestBody("Application member payload.", schemaRef("ApplicationMemberRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/applications/{slug}/members/{userId}", &openapi3.PathItem{
				Patch: func() *openapi3.Operation {
					op := operation("updateApplicationMember", "Update application member role", []string{"Applications"}, responses(
						response(http.StatusOK, "Application member was updated.", schemaRef("ApplicationMember")),
						response(http.StatusBadRequest, "Invalid member request.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot manage application members.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Application member or application not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = append(applicationSlugParameters(), applicationMemberUserIDParameter())
					op.RequestBody = requestBody("Application member role payload.", schemaRef("ApplicationMemberRoleUpdateRequest"))
					return op
				}(),
				Delete: func() *openapi3.Operation {
					op := operation("deleteApplicationMember", "Remove application member", []string{"Applications"}, responses(
						responseNoBody(http.StatusNoContent, "Application member was removed."),
						response(http.StatusBadRequest, "Invalid member request or application last owner.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot manage application members.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Application member or application not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = append(applicationSlugParameters(), applicationMemberUserIDParameter())
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/modules", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("listModules", "List modules", []string{"Modules"}, responses(
						response(http.StatusOK, "Module list.", schemaRef("ModulesResponse")),
						response(http.StatusBadRequest, "Invalid module list filter.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = moduleListParameters()
					return op
				}(),
				Post: func() *openapi3.Operation {
					op := operation("createModule", "Create module", []string{"Modules"}, responses(
						response(http.StatusCreated, "Module created.", schemaRef("ModuleDetailResponse")),
						response(http.StatusBadRequest, "Invalid module request.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot create modules for this application.", schemaRef("ErrorResponse")),
						response(http.StatusConflict, "Module id or repository already exists.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.RequestBody = requestBody("Module create payload.", schemaRef("ModuleCreateRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/modules/{id}", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("getModule", "Get module detail", []string{"Modules"}, responses(
						response(http.StatusOK, "Module detail.", schemaRef("ModuleDetailResponse")),
						response(http.StatusNotFound, "Module not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = moduleIDParameters()
					return op
				}(),
				Patch: func() *openapi3.Operation {
					op := operation("updateModule", "Update module", []string{"Modules"}, responses(
						response(http.StatusOK, "Module updated.", schemaRef("ModuleDetailResponse")),
						response(http.StatusBadRequest, "Invalid module request.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot update this module.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Module not found.", schemaRef("ErrorResponse")),
						response(http.StatusConflict, "Module repository already exists.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = moduleIDParameters()
					op.RequestBody = requestBody("Module update payload. Module id is immutable.", schemaRef("ModuleUpdateRequest"))
					return op
				}(),
				Delete: func() *openapi3.Operation {
					op := operation("deleteModule", "Delete module", []string{"Modules"}, responses(
						responseNoBody(http.StatusNoContent, "Module was deleted."),
						response(http.StatusForbidden, "Current user cannot delete this module.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Module not found.", schemaRef("ErrorResponse")),
						response(http.StatusConflict, "Module is referenced.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = moduleIDParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/modules/{id}/build-variables", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("listModuleBuildVariables", "List module build variables", []string{"Modules"}, responses(
						response(http.StatusOK, "Module build variables. Secret values are masked as ******.", schemaRef("BuildVariablesResponse")),
						response(http.StatusForbidden, "Current user cannot manage this module.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Module not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = moduleIDParameters()
					return op
				}(),
				Put: func() *openapi3.Operation {
					op := operation("upsertModuleBuildVariable", "Create or update a module build variable", []string{"Modules"}, responses(
						response(http.StatusOK, "Build variable saved. Secret values are masked as ******.", schemaRef("BuildVariableDetailResponse")),
						response(http.StatusBadRequest, "Invalid build variable request.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot manage this module.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Module or environment not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = moduleIDParameters()
					op.RequestBody = requestBody("Build variable payload. Upserts by key and environmentCode.", schemaRef("BuildVariableUpsertRequest"))
					return op
				}(),
				Delete: func() *openapi3.Operation {
					op := operation("deleteModuleBuildVariable", "Delete a module build variable", []string{"Modules"}, responses(
						responseNoBody(http.StatusNoContent, "Build variable deleted."),
						response(http.StatusBadRequest, "Invalid build variable request.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot manage this module.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Module or build variable not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = append(moduleIDParameters(), buildVariableDeleteParameters()...)
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/builds", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("listBuildRecords", "List RN JS bundle build records", []string{"Builds"}, responses(
						response(http.StatusOK, "Build record list.", schemaRef("BuildRecordsResponse")),
						response(http.StatusBadRequest, "Invalid build filter.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = buildListParameters()
					return op
				}(),
				Post: func() *openapi3.Operation {
					op := operation("createBuildRecord", "Create RN JS bundle build record", []string{"Builds"}, responses(
						response(http.StatusCreated, "Build record created.", schemaRef("BuildRecordDetailResponse")),
						response(http.StatusBadRequest, "Invalid build request.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot create builds for this module.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Module or environment not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.RequestBody = requestBody("Build creation payload.", schemaRef("BuildRecordCreateRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/builds/{id}", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("getBuildRecord", "Get RN JS bundle build record detail", []string{"Builds"}, responses(
						response(http.StatusOK, "Build record detail.", schemaRef("BuildRecordDetailResponse")),
						response(http.StatusNotFound, "Build record not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = buildRecordIDParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/builds/{id}/logs", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("listBuildLogs", "List build log chunks", []string{"Builds"}, responses(
						response(http.StatusOK, "Build log chunks after afterSeq.", schemaRef("BuildLogsResponse")),
						response(http.StatusBadRequest, "Invalid afterSeq.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Build record not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = append(buildRecordIDParameters(), buildLogParameters()...)
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/artifacts/applications/{applicationSlug}/modules/{moduleId}/environments/{environment}/builds/{buildId}/file", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("downloadArtifactFile", "Download a single build artifact file", []string{"Artifacts"}, responses(
						responseNoBody(http.StatusOK, "Artifact file stream."),
						response(http.StatusBadRequest, "Invalid artifact request.", schemaRef("ErrorResponse")),
						response(http.StatusUnauthorized, "Console session or artifact token is required.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Artifact not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = artifactDownloadParameters()
					op.Security = artifactAccessSecurityRequirements()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/artifacts/applications/{applicationSlug}/environments/{environment}/manifest", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("getApplicationArtifactManifest", "Get application module artifact manifest", []string{"Artifacts"}, responses(
						response(http.StatusOK, "Application module artifact manifest.", schemaRef("ArtifactManifestResponse")),
						response(http.StatusBadRequest, "Invalid artifact manifest request, including missing or invalid platform or runtimeVersion.", schemaRef("ErrorResponse")),
						response(http.StatusUnauthorized, "Artifact token is required.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Application environment manifest not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = artifactManifestParameters()
					op.Security = artifactTokenSecurityRequirements()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/releases", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("listReleases", "List RN JS bundle releases", []string{"Releases"}, responses(
						response(http.StatusOK, "Release list.", schemaRef("ReleasesResponse")),
						response(http.StatusBadRequest, "Invalid release filter.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = releaseListParameters()
					return op
				}(),
				Post: func() *openapi3.Operation {
					op := operation("createRelease", "Create RN JS bundle release", []string{"Releases"}, responses(
						response(http.StatusCreated, "Release created and route activated.", schemaRef("ReleaseDetailResponse")),
						response(http.StatusBadRequest, "Invalid release request.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot create releases for this build.", schemaRef("ErrorResponse")),
						response(http.StatusConflict, "Release target is already active for the same build.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Build record not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.RequestBody = requestBody("Release creation payload.", schemaRef("ReleaseCreateRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/releases/{id}/gray-percentage", &openapi3.PathItem{
				Patch: func() *openapi3.Operation {
					op := operation("updateReleaseGrayPercentage", "Update percentage gray release", []string{"Releases"}, responses(
						response(http.StatusOK, "Release gray percentage updated.", schemaRef("ReleaseDetailResponse")),
						response(http.StatusBadRequest, "Invalid release request.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot update this release.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Release not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = openapi3.Parameters{
						{
							Value: openapi3.NewPathParameter("id").
								WithDescription("Release UUID.").
								WithRequired(true).
								WithSchema(stringSchema("Release UUID.")),
						},
					}
					op.RequestBody = requestBody("Gray percentage update payload.", schemaRef("ReleaseGrayPercentageUpdateRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/releases/{id}/inactivate", &openapi3.PathItem{
				Patch: func() *openapi3.Operation {
					op := operation("inactivateTargetedGrayRelease", "Inactivate targeted gray release", []string{"Releases"}, responses(
						response(http.StatusOK, "Targeted gray release inactivated.", schemaRef("ReleaseDetailResponse")),
						response(http.StatusBadRequest, "Release is not an active targeted gray release.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot rollback this release.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Release not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = openapi3.Parameters{
						{
							Value: openapi3.NewPathParameter("id").
								WithDescription("Release UUID.").
								WithRequired(true).
								WithSchema(stringSchema("Release UUID.")),
						},
					}
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/releases/resolve", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("resolveReleaseRoute", "Resolve active release route", []string{"Releases"}, responses(
						response(http.StatusOK, "Resolved release route.", schemaRef("ReleaseResolveResponse")),
						response(http.StatusBadRequest, "Invalid release resolve request.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Release route not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = releaseResolveParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/code-sources", &openapi3.PathItem{
				Get: operation("listCodeSources", "List code source apps (GitHub App status)", []string{"CodeSources"}, responses(
					response(http.StatusOK, "Code source app list.", schemaRef("CodeSourcesResponse")),
					response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
					response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
				)),
			}),
			openapi3.WithPath("/api/v1/code-sources/github/manifest", &openapi3.PathItem{
				Get: operation("getCodeSourceAppManifest", "Get GitHub App manifest creation payload", []string{"CodeSources"}, codeSourceResponses(
					response(http.StatusOK, "GitHub App manifest creation payload.", schemaRef("CodeSourceAppManifest")),
				)),
			}),
			openapi3.WithPath("/api/v1/code-sources/github/manifest/callback", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("completeCodeSourceAppManifest", "Complete GitHub App manifest callback", []string{"CodeSources"}, responses(
						response(http.StatusFound, "Redirects to GitHub App installation URL.", nil),
						response(http.StatusBadRequest, "Invalid code source manifest callback.", schemaRef("ErrorResponse")),
						response(http.StatusServiceUnavailable, "Code source auth is not configured.", schemaRef("ErrorResponse")),
						response(http.StatusBadGateway, "Code source provider request failed.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = codeSourceManifestCallbackParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/build-adapters", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("listBuildAdapters", "List build adapters", []string{"BuildAdapters"}, responses(
						response(http.StatusOK, "Build adapter list with referencing module counts.", schemaRef("BuildAdaptersResponse")),
						response(http.StatusBadRequest, "Invalid status filter.", schemaRef("BuildAdapterErrorResponse")),
						response(http.StatusForbidden, "Current user cannot manage modules in any application.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = openapi3.Parameters{
						{
							Value: openapi3.NewQueryParameter("status").
								WithDescription("Status filter.").
								WithSchema(enumStringSchema("Build adapter status.", "active", "disabled")),
						},
					}
					return op
				}(),
				Post: func() *openapi3.Operation {
					op := operation("createBuildAdapter", "Create build adapter", []string{"BuildAdapters"}, responses(
						response(http.StatusCreated, "Build adapter created with version 1.", schemaRef("BuildAdapterDetailResponse")),
						response(http.StatusBadRequest, "Invalid build adapter. Template errors include the template name and line.", schemaRef("BuildAdapterErrorResponse")),
						response(http.StatusForbidden, "Only PlatformAdmin or SuperAdmin can manage build adapters.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.RequestBody = requestBody("Build adapter create payload.", schemaRef("BuildAdapterCreateRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/build-adapters/render-preview", &openapi3.PathItem{
				Post: func() *openapi3.Operation {
					op := operation("previewBuildAdapterDraft", "Render an unsaved build adapter draft with sample context or a module, without building", []string{"BuildAdapters"}, responses(
						response(http.StatusOK, "Render result. Invalid drafts, template errors and parameter errors return ok=false with error details.", schemaRef("BuildAdapterRenderPreviewResponse")),
						response(http.StatusBadRequest, "Invalid request body.", schemaRef("BuildAdapterErrorResponse")),
						response(http.StatusForbidden, "Only PlatformAdmin or SuperAdmin can preview drafts.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Module not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.RequestBody = requestBody("Draft render preview payload.", schemaRef("BuildAdapterDraftRenderPreviewRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/build-adapters/{id}", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("getBuildAdapter", "Get build adapter", []string{"BuildAdapters"}, responses(
						response(http.StatusOK, "Build adapter detail.", schemaRef("BuildAdapterDetailResponse")),
						response(http.StatusForbidden, "Current user cannot manage modules in any application.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Build adapter not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = buildAdapterIDParameters()
					return op
				}(),
				Patch: func() *openapi3.Operation {
					op := operation("updateBuildAdapter", "Update build adapter. Each update increments version", []string{"BuildAdapters"}, responses(
						response(http.StatusOK, "Build adapter updated.", schemaRef("BuildAdapterDetailResponse")),
						response(http.StatusBadRequest, "Invalid build adapter. Template errors include the template name and line.", schemaRef("BuildAdapterErrorResponse")),
						response(http.StatusForbidden, "Only PlatformAdmin or SuperAdmin can manage build adapters.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Build adapter not found.", schemaRef("ErrorResponse")),
						response(http.StatusConflict, "Build adapter was modified concurrently.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = buildAdapterIDParameters()
					op.RequestBody = requestBody("Build adapter update payload. Omitted fields are unchanged; files and parameters replace the whole value.", schemaRef("BuildAdapterUpdateRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/build-adapters/{id}/disable", &openapi3.PathItem{
				Post: func() *openapi3.Operation {
					op := operation("disableBuildAdapter", "Disable build adapter. Referencing modules cannot start new builds", []string{"BuildAdapters"}, responses(
						response(http.StatusOK, "Build adapter disabled.", schemaRef("BuildAdapterDetailResponse")),
						response(http.StatusForbidden, "Only PlatformAdmin or SuperAdmin can manage build adapters.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Build adapter not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = buildAdapterIDParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/build-adapters/{id}/enable", &openapi3.PathItem{
				Post: func() *openapi3.Operation {
					op := operation("enableBuildAdapter", "Enable build adapter", []string{"BuildAdapters"}, responses(
						response(http.StatusOK, "Build adapter enabled.", schemaRef("BuildAdapterDetailResponse")),
						response(http.StatusForbidden, "Only PlatformAdmin or SuperAdmin can manage build adapters.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Build adapter not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = buildAdapterIDParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/build-adapters/{id}/render-preview", &openapi3.PathItem{
				Post: func() *openapi3.Operation {
					op := operation("previewBuildAdapter", "Render build adapter with sample context or a module, without building", []string{"BuildAdapters"}, responses(
						response(http.StatusOK, "Render result. Template and parameter errors return ok=false with error details.", schemaRef("BuildAdapterRenderPreviewResponse")),
						response(http.StatusBadRequest, "Invalid request body.", schemaRef("BuildAdapterErrorResponse")),
						response(http.StatusForbidden, "Current user cannot manage modules in any application.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Build adapter or module not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = buildAdapterIDParameters()
					op.RequestBody = requestBody("Render preview payload. Both fields are optional.", schemaRef("BuildAdapterRenderPreviewRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/code-sources/github/install-url", &openapi3.PathItem{
				Get: operation("getCodeSourceInstallURL", "Get GitHub App installation URL", []string{"CodeSources"}, codeSourceResponses(
					response(http.StatusOK, "GitHub App installation URL.", schemaRef("CodeSourceInstallURL")),
				)),
			}),
			openapi3.WithPath("/api/v1/code-sources/github/install/callback", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("completeCodeSourceInstallation", "Complete GitHub App installation callback and upsert the installation connection", []string{"CodeSources"}, codeSourceResponses(
						response(http.StatusFound, "Installation connection saved and redirects to console.", nil),
					))
					op.Parameters = codeSourceInstallCallbackParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/code-source-connections", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("listCodeSourceConnections", "List code source connections", []string{"CodeSources"}, codeSourceResponses(
						response(http.StatusOK, "Code source connection list. Deleted connections are excluded.", schemaRef("CodeSourceConnectionsResponse")),
					))
					op.Parameters = codeSourceConnectionListParameters()
					return op
				}(),
				Post: func() *openapi3.Operation {
					op := operation("createCodeSourceConnection", "Create a token based code source connection (GitLab)", []string{"CodeSources"}, codeSourceResponses(
						response(http.StatusCreated, "Connection verified and created.", schemaRef("CodeSourceConnectionDetailResponse")),
						response(http.StatusConflict, "Connection already exists.", schemaRef("ErrorResponse")),
					))
					op.RequestBody = requestBody("Code source connection create payload.", schemaRef("CodeSourceConnectionCreateRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/code-source-connections/{id}", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("getCodeSourceConnection", "Get code source connection", []string{"CodeSources"}, codeSourceResponses(
						response(http.StatusOK, "Code source connection detail.", schemaRef("CodeSourceConnectionDetailResponse")),
					))
					op.Parameters = codeSourceConnectionIDParameters()
					return op
				}(),
				Patch: func() *openapi3.Operation {
					op := operation("updateCodeSourceConnection", "Update code source connection name, credential or status", []string{"CodeSources"}, codeSourceResponses(
						response(http.StatusOK, "Code source connection updated.", schemaRef("CodeSourceConnectionDetailResponse")),
					))
					op.Parameters = codeSourceConnectionIDParameters()
					op.RequestBody = requestBody("Code source connection update payload. Omitted fields are unchanged.", schemaRef("CodeSourceConnectionUpdateRequest"))
					return op
				}(),
				Delete: func() *openapi3.Operation {
					op := operation("deleteCodeSourceConnection", "Delete code source connection", []string{"CodeSources"}, codeSourceResponses(
						responseNoBody(http.StatusNoContent, "Connection was deleted."),
						response(http.StatusConflict, "Connection is referenced by modules.", schemaRef("ErrorResponse")),
					))
					op.Parameters = codeSourceConnectionIDParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/code-source-connections/{id}/verify", &openapi3.PathItem{
				Post: func() *openapi3.Operation {
					op := operation("verifyCodeSourceConnection", "Verify code source connection credential", []string{"CodeSources"}, codeSourceResponses(
						response(http.StatusOK, "Connection verified.", schemaRef("CodeSourceConnectionDetailResponse")),
					))
					op.Parameters = codeSourceConnectionIDParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/code-source-connections/{id}/repositories", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("searchCodeSourceRepositories", "Search repositories accessible by a connection and refresh the repository cache", []string{"CodeSources"}, codeSourceResponses(
						response(http.StatusOK, "Repository search result.", schemaRef("CodeSourceRepositorySearchResponse")),
					))
					op.Parameters = codeSourceRepositorySearchParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/code-source-connections/{id}/webhook", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("getCodeSourceConnectionWebhook", "Get webhook configuration of a connection", []string{"CodeSources"}, codeSourceResponses(
						response(http.StatusOK, "Webhook path and secret.", schemaRef("CodeSourceConnectionWebhook")),
					))
					op.Parameters = codeSourceConnectionIDParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/webhooks/github", &openapi3.PathItem{
				Post: func() *openapi3.Operation {
					op := operation("receiveGitHubWebhook", "Receive GitHub App webhook", []string{"Webhooks"}, webhookResponses())
					op.Description = "Shared by all GitHub App installations. Verified with X-Hub-Signature-256 (HMAC-SHA256 of the App webhook secret); only push events are processed."
					op.Parameters = webhookHeaderParameters("X-Hub-Signature-256", "X-GitHub-Event", "X-GitHub-Delivery")
					op.RequestBody = requestBody("Raw GitHub webhook payload.", schemaValue(openapi3.NewObjectSchema()))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/webhooks/gitlab/{connectionId}", &openapi3.PathItem{
				Post: func() *openapi3.Operation {
					op := operation("receiveGitLabWebhook", "Receive GitLab project webhook", []string{"Webhooks"}, webhookResponses())
					op.Description = "Configured per GitLab project. Verified with X-Gitlab-Token against the connection webhook secret; only Push Hook branch pushes are processed."
					op.Parameters = append(openapi3.Parameters{
						{
							Value: openapi3.NewPathParameter("connectionId").
								WithDescription("Code source connection id.").
								WithSchema(stringSchema("Code source connection id.")),
						},
					}, webhookHeaderParameters("X-Gitlab-Token", "X-Gitlab-Event", "X-Gitlab-Event-UUID")...)
					op.RequestBody = requestBody("Raw GitLab webhook payload.", schemaValue(openapi3.NewObjectSchema()))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/artifact-access-tokens", &openapi3.PathItem{
				Get: operation("listArtifactAccessTokens", "List artifact access tokens", []string{"AccessTokens"}, responses(
					response(http.StatusOK, "Artifact access token list.", schemaRef("ArtifactAccessTokensResponse")),
					response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
					response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
				)),
				Post: func() *openapi3.Operation {
					op := operation("createArtifactAccessToken", "Create artifact access token", []string{"AccessTokens"}, responses(
						response(http.StatusCreated, "Artifact access token created.", schemaRef("ArtifactAccessTokenDetailResponse")),
						response(http.StatusBadRequest, "Invalid artifact access token request.", schemaRef("ErrorResponse")),
						response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot manage artifact access tokens.", schemaRef("ErrorResponse")),
						response(http.StatusConflict, "Artifact access token already exists.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.RequestBody = requestBody("Artifact access token create payload.", schemaRef("ArtifactAccessTokenCreateRequest"))
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/artifact-access-tokens/generate", &openapi3.PathItem{
				Post: operation("generateArtifactAccessToken", "Generate artifact access token", []string{"AccessTokens"}, responses(
					response(http.StatusOK, "One-time generated artifact access token.", schemaRef("ArtifactAccessTokenGenerateResponse")),
					response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
					response(http.StatusForbidden, "Current user cannot manage artifact access tokens.", schemaRef("ErrorResponse")),
					response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
				)),
			}),
			openapi3.WithPath("/api/v1/artifact-access-tokens/{id}", &openapi3.PathItem{
				Patch: func() *openapi3.Operation {
					op := operation("updateArtifactAccessToken", "Update artifact access token", []string{"AccessTokens"}, responses(
						response(http.StatusOK, "Artifact access token updated.", schemaRef("ArtifactAccessTokenDetailResponse")),
						response(http.StatusBadRequest, "Invalid artifact access token request.", schemaRef("ErrorResponse")),
						response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot manage artifact access tokens.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Artifact access token not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = artifactAccessTokenIDParameters()
					op.RequestBody = requestBody("Artifact access token update payload.", schemaRef("ArtifactAccessTokenUpdateRequest"))
					return op
				}(),
				Delete: func() *openapi3.Operation {
					op := operation("deleteArtifactAccessToken", "Delete artifact access token", []string{"AccessTokens"}, responses(
						responseNoBody(http.StatusNoContent, "Artifact access token deleted."),
						response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot manage artifact access tokens.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Artifact access token not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = artifactAccessTokenIDParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/storage/configurations", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("listStorageConfigurations", "List storage configurations", []string{"Storage"}, responses(
						response(http.StatusOK, "Storage configuration list.", schemaRef("StorageConfigurationsResponse")),
						response(http.StatusBadRequest, "Invalid pagination.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = paginationParameters()
					return op
				}(),
				Post: func() *openapi3.Operation {
					op := operation("createStorageConfiguration", "Create storage configuration", []string{"Storage"}, responses(
						response(http.StatusCreated, "Storage configuration created.", schemaRef("StorageConfigurationDetailResponse")),
						response(http.StatusBadRequest, "Invalid storage configuration request.", schemaRef("ErrorResponse")),
						response(http.StatusForbidden, "Current user cannot manage storage configurations.", schemaRef("ErrorResponse")),
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
						response(http.StatusForbidden, "Current user cannot manage storage configurations.", schemaRef("ErrorResponse")),
						response(http.StatusBadGateway, "Storage connectivity test failed; detail explains the reason without secrets.", schemaRef("StorageConfigurationTestFailedResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.RequestBody = requestBody("Storage configuration test payload.", schemaRef("StorageConfigurationTestRequest"))
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
						response(http.StatusForbidden, "Current user cannot manage storage configurations.", schemaRef("ErrorResponse")),
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
						response(http.StatusForbidden, "Current user cannot manage storage configurations.", schemaRef("ErrorResponse")),
						response(http.StatusConflict, "Storage configuration is referenced by environments.", schemaRef("ErrorResponse")),
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
			SecuritySchemes: openapi3.SecuritySchemes{
				"ConsoleSession": {Value: &openapi3.SecurityScheme{
					Type:        "apiKey",
					In:          "cookie",
					Name:        feishuSessionCookieName,
					Description: "Forge console session cookie.",
				}},
				"ArtifactBearer": {Value: &openapi3.SecurityScheme{
					Type:         "http",
					Scheme:       "bearer",
					BearerFormat: "artifact-token",
					Description:  "Artifact access token managed by the platform.",
				}},
			},
			Schemas: openapi3.Schemas{
				"ErrorResponse":                          schemaValue(errorResponseSchema()),
				"HealthResponse":                         schemaValue(healthResponseSchema()),
				"ReadinessResponse":                      schemaValue(readinessResponseSchema()),
				"PingResponse":                           schemaValue(pingResponseSchema()),
				"EnumOption":                             schemaValue(enumOptionSchema()),
				"EnumsResponse":                          schemaValue(enumsResponseSchema()),
				"DashboardResponse":                      schemaValue(dashboardResponseSchema()),
				"DashboardMetric":                        schemaValue(dashboardMetricSchema()),
				"DashboardBuildTrendPoint":               schemaValue(dashboardBuildTrendPointSchema()),
				"DashboardBuildRecord":                   schemaValue(dashboardBuildRecordSchema()),
				"DashboardReleaseRecord":                 schemaValue(dashboardReleaseRecordSchema()),
				"DashboardResourceStatus":                schemaValue(dashboardResourceStatusSchema()),
				"Environment":                            schemaValue(environmentSchema()),
				"EnvironmentCreateRequest":               schemaValue(environmentCreateRequestSchema()),
				"EnvironmentUpdateRequest":               schemaValue(environmentCreateRequestSchema()),
				"EnvironmentsResponse":                   schemaValue(environmentsResponseSchema()),
				"EnvironmentDetailResponse":              schemaValue(environmentDetailResponseSchema()),
				"Pagination":                             schemaValue(paginationSchema()),
				"Application":                            schemaValue(applicationSchema()),
				"ApplicationOption":                      schemaValue(applicationOptionSchema()),
				"ApplicationOptions":                     schemaValue(applicationOptionsSchema()),
				"ApplicationSummary":                     schemaValue(applicationSummarySchema()),
				"ApplicationMetric":                      schemaValue(applicationMetricSchema()),
				"ApplicationEnvironment":                 schemaValue(applicationEnvironmentSchema()),
				"ApplicationModule":                      schemaValue(applicationModuleSchema()),
				"ApplicationMember":                      schemaValue(applicationMemberSchema()),
				"ApplicationMembersResponse":             schemaValue(applicationMembersResponseSchema()),
				"ApplicationCapabilities":                schemaValue(applicationCapabilitiesSchema()),
				"ApplicationClientIntegration":           schemaValue(applicationClientIntegrationSchema()),
				"ApplicationClientIntegrationEndpoint":   schemaValue(applicationClientIntegrationEndpointSchema()),
				"ApplicationClientIntegrationField":      schemaValue(applicationClientIntegrationFieldSchema()),
				"ApplicationDetail":                      schemaValue(applicationDetailSchema()),
				"ApplicationsResponse":                   schemaValue(applicationsResponseSchema()),
				"ApplicationDetailResponse":              schemaValue(applicationDetailResponseSchema()),
				"ApplicationCreateRequest":               schemaValue(applicationRequestSchema(true)),
				"ApplicationUpdateRequest":               schemaValue(applicationRequestSchema(false)),
				"ApplicationStatusUpdateRequest":         schemaValue(applicationStatusUpdateRequestSchema()),
				"ApplicationEnvironmentRequest":          schemaValue(applicationEnvironmentRequestSchema()),
				"ApplicationMemberRequest":               schemaValue(applicationMemberRequestSchema()),
				"ApplicationMemberRoleUpdateRequest":     schemaValue(applicationMemberRoleUpdateRequestSchema()),
				"ApplicationDeleteCheck":                 schemaValue(applicationDeleteCheckSchema()),
				"ApplicationDeleteDependency":            schemaValue(applicationDeleteDependencySchema()),
				"Module":                                 schemaValue(moduleSchema()),
				"ModuleSummary":                          schemaValue(moduleSummarySchema()),
				"ModulesResponse":                        schemaValue(modulesResponseSchema()),
				"ModuleDetailResponse":                   schemaValue(moduleDetailResponseSchema()),
				"ModuleRepositoryRequest":                schemaValue(moduleRepositoryRequestSchema()),
				"ModuleBuildConfig":                      schemaValue(moduleBuildConfigSchema()),
				"ModuleCreateRequest":                    schemaValue(moduleRequestSchema(true)),
				"ModuleUpdateRequest":                    schemaValue(moduleRequestSchema(false)),
				"BuildRecord":                            schemaValue(buildRecordSchema()),
				"BuildAdapter":                           schemaValue(buildAdapterSchema()),
				"BuildAdapterParameter":                  schemaValue(buildAdapterParameterSchema()),
				"BuildAdaptersResponse":                  schemaValue(buildAdaptersResponseSchema()),
				"BuildAdapterDetailResponse":             schemaValue(buildAdapterDetailResponseSchema()),
				"BuildAdapterCreateRequest":              schemaValue(buildAdapterRequestSchema(true)),
				"BuildAdapterUpdateRequest":              schemaValue(buildAdapterRequestSchema(false).WithProperty("expectedVersion", int32Schema("Version the editor loaded. When set and different from the current version, the update is rejected with 409."))),
				"BuildAdapterErrorResponse":              schemaValue(buildAdapterErrorResponseSchema()),
				"BuildAdapterRenderPreviewRequest":       schemaValue(buildAdapterRenderPreviewRequestSchema()),
				"BuildAdapterDraftRenderPreviewRequest":  schemaValue(buildAdapterDraftRenderPreviewRequestSchema()),
				"BuildAdapterRenderPreviewResponse":      schemaValue(buildAdapterRenderPreviewResponseSchema()),
				"BuildAdapterRenderError":                schemaValue(buildAdapterRenderErrorSchema()),
				"UserSummary":                            schemaValue(userSummarySchema()),
				"BuildDependency":                        schemaValue(buildDependencySchema()),
				"BuildArtifactTree":                      schemaValue(buildArtifactTreeSchema()),
				"BuildArtifactTreeNode":                  schemaValue(buildArtifactTreeNodeSchema()),
				"BuildRecordCreateRequest":               schemaValue(buildRecordCreateRequestSchema()),
				"BuildRecordDetailResponse":              schemaValue(buildRecordDetailResponseSchema()),
				"BuildRecordSummary":                     schemaValue(buildRecordSummarySchema()),
				"BuildRecordsResponse":                   schemaValue(buildRecordsResponseSchema()),
				"BuildLogChunk":                          schemaValue(buildLogChunkSchema()),
				"BuildLogsResponse":                      schemaValue(buildLogsResponseSchema()),
				"BuildVariable":                          schemaValue(buildVariableSchema()),
				"BuildVariablesResponse":                 schemaValue(buildVariablesResponseSchema()),
				"BuildVariableDetailResponse":            schemaValue(buildVariableDetailResponseSchema()),
				"BuildVariableUpsertRequest":             schemaValue(buildVariableUpsertRequestSchema()),
				"ArtifactManifestResponse":               schemaValue(artifactManifestResponseSchema()),
				"ArtifactManifestApplication":            schemaValue(artifactManifestApplicationSchema()),
				"ArtifactManifestEnvironment":            schemaValue(artifactManifestEnvironmentSchema()),
				"ArtifactManifestModule":                 schemaValue(artifactManifestModuleSchema()),
				"ArtifactManifestFile":                   schemaValue(artifactManifestFileSchema()),
				"ArtifactMissingModule":                  schemaValue(artifactMissingModuleSchema()),
				"ReleaseRecord":                          schemaValue(releaseRecordSchema()),
				"ReleaseRecordSummary":                   schemaValue(releaseRecordSummarySchema()),
				"ReleaseRoute":                           schemaValue(releaseRouteSchema()),
				"ReleaseCreateRequest":                   schemaValue(releaseCreateRequestSchema()),
				"ReleaseGrayPercentageUpdateRequest":     schemaValue(releaseGrayPercentageUpdateRequestSchema()),
				"ReleaseDetailResponse":                  schemaValue(releaseDetailResponseSchema()),
				"ReleaseResolveResponse":                 schemaValue(releaseResolveResponseSchema()),
				"ReleasesResponse":                       schemaValue(releasesResponseSchema()),
				"CodeSourceApp":                          schemaValue(codeSourceAppSchema()),
				"CodeSourceAppManifest":                  schemaValue(codeSourceAppManifestSchema()),
				"CodeSourceInstallURL":                   schemaValue(codeSourceInstallURLSchema()),
				"CodeSourceConnection":                   schemaValue(codeSourceConnectionSchema()),
				"CodeSourceConnectionsResponse":          schemaValue(codeSourceConnectionsResponseSchema()),
				"CodeSourceConnectionDetailResponse":     schemaValue(codeSourceConnectionDetailResponseSchema()),
				"CodeSourceConnectionCreateRequest":      schemaValue(codeSourceConnectionCreateRequestSchema()),
				"CodeSourceConnectionUpdateRequest":      schemaValue(codeSourceConnectionUpdateRequestSchema()),
				"CodeSourceConnectionWebhook":            schemaValue(codeSourceConnectionWebhookSchema()),
				"CodeSourceRepository":                   schemaValue(codeSourceRepositorySchema()),
				"CodeSourceRepositorySearchResponse":     schemaValue(codeSourceRepositorySearchResponseSchema()),
				"CodeSourcesResponse":                    schemaValue(codeSourcesResponseSchema()),
				"ArtifactAccessToken":                    schemaValue(artifactAccessTokenSchema()),
				"ArtifactAccessTokensResponse":           schemaValue(artifactAccessTokensResponseSchema()),
				"ArtifactAccessTokenDetailResponse":      schemaValue(artifactAccessTokenDetailResponseSchema()),
				"ArtifactAccessTokenGenerateResponse":    schemaValue(artifactAccessTokenGenerateResponseSchema()),
				"ArtifactAccessTokenCreateRequest":       schemaValue(artifactAccessTokenCreateRequestSchema()),
				"ArtifactAccessTokenUpdateRequest":       schemaValue(artifactAccessTokenUpdateRequestSchema()),
				"StorageConfiguration":                   schemaValue(storageConfigurationSchema()),
				"StorageConfigurationsResponse":          schemaValue(storageConfigurationsResponseSchema()),
				"StorageConfigurationDetailResponse":     schemaValue(storageConfigurationDetailResponseSchema()),
				"StorageConfigurationCreateRequest":      schemaValue(storageConfigurationRequestSchema(true)),
				"StorageConfigurationUpdateRequest":      schemaValue(storageConfigurationRequestSchema(false)),
				"StorageConfigurationTestRequest":        schemaValue(storageConfigurationTestRequestSchema()),
				"StorageConfigurationTestResponse":       schemaValue(storageConfigurationTestResponseSchema()),
				"StorageConfigurationTestFailedResponse": schemaValue(objectSchema("error").WithProperty("error", stringSchema("Error message.")).WithProperty("detail", stringSchema("Human readable failure reason, e.g. HTTP status and S3 error code. Never contains credentials."))),
				"GlobalCapabilities":                     schemaValue(globalCapabilitiesSchema()),
				"FeishuAuthResponse":                     schemaValue(feishuAuthResponseSchema()),
				"FeishuToken":                            schemaValue(feishuTokenSchema()),
				"FeishuUser":                             schemaValue(feishuUserSchema()),
				"FeishuUserSearchItem":                   schemaValue(feishuUserSearchItemSchema()),
				"FeishuUserSearchResponse":               schemaValue(feishuUserSearchResponseSchema()),
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

func artifactAccessSecurityRequirements() *openapi3.SecurityRequirements {
	requirements := openapi3.SecurityRequirements{
		openapi3.SecurityRequirement{"ConsoleSession": []string{}},
		openapi3.SecurityRequirement{"ArtifactBearer": []string{}},
	}
	return &requirements
}

func artifactTokenSecurityRequirements() *openapi3.SecurityRequirements {
	requirements := openapi3.SecurityRequirements{
		openapi3.SecurityRequirement{"ArtifactBearer": []string{}},
	}
	return &requirements
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

// nullableSchemaRef 用 allOf 包一层 $ref，OpenAPI 3.0 下 $ref 旁的 nullable 会被忽略。
func nullableSchemaRef(name string, description string) *openapi3.Schema {
	schema := &openapi3.Schema{AllOf: openapi3.SchemaRefs{schemaRef(name)}, Description: description}
	return schema.WithNullable()
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

func enumOptionSchema() *openapi3.Schema {
	return objectSchema("label", "value").
		WithProperty("label", stringSchema("Display label.")).
		WithProperty("value", stringSchema("Enum value."))
}

func enumsResponseSchema() *openapi3.Schema {
	return objectSchema("applicationStatuses", "moduleKinds", "platforms").
		WithPropertyRef("applicationStatuses", arraySchemaRef("EnumOption")).
		WithPropertyRef("moduleKinds", arraySchemaRef("EnumOption")).
		WithPropertyRef("platforms", arraySchemaRef("EnumOption"))
}

func dashboardResponseSchema() *openapi3.Schema {
	return objectSchema("metrics", "buildTrend", "builds", "releases", "resources", "updatedAt").
		WithPropertyRef("metrics", arraySchemaRef("DashboardMetric")).
		WithPropertyRef("buildTrend", arraySchemaRef("DashboardBuildTrendPoint")).
		WithPropertyRef("builds", arraySchemaRef("DashboardBuildRecord")).
		WithPropertyRef("releases", arraySchemaRef("DashboardReleaseRecord")).
		WithPropertyRef("resources", arraySchemaRef("DashboardResourceStatus")).
		WithProperty("updatedAt", dateTimeSchema("Dashboard snapshot update time."))
}

func dashboardMetricSchema() *openapi3.Schema {
	return objectSchema("description", "id", "label", "value", "trend", "trendTone", "icon", "iconTone", "sparkline").
		WithProperty("description", stringSchema("Metric description.")).
		WithProperty("id", stringSchema("Metric id.")).
		WithProperty("label", stringSchema("Metric display label.")).
		WithProperty("value", stringSchema("Metric display value.")).
		WithProperty("trend", stringSchema("Metric secondary trend label.")).
		WithProperty("trendTone", enumStringSchema("Metric trend tone.", "positive", "negative", "neutral")).
		WithProperty("icon", enumStringSchema("Metric icon name.", "accessToken", "app", "build", "codeSource", "environment", "module", "release", "storage")).
		WithProperty("iconTone", enumStringSchema("Metric icon tone.", "amber", "blue", "cyan", "emerald", "indigo", "red", "slate", "violet")).
		WithProperty("sparkline", intArraySchema("Metric sparkline values."))
}

func dashboardBuildTrendPointSchema() *openapi3.Schema {
	return objectSchema("date", "buildCount", "successCount").
		WithProperty("date", stringSchema("Trend point date label.")).
		WithProperty("buildCount", int32Schema("Build count.")).
		WithProperty("successCount", int32Schema("Succeeded build count."))
}

func dashboardBuildRecordSchema() *openapi3.Schema {
	return objectSchema("application", "duration", "environment", "id", "module", "operator", "startedAt", "status").
		WithProperty("application", stringSchema("Application display name.")).
		WithProperty("duration", stringSchema("Build duration display label.")).
		WithProperty("environment", stringSchema("Environment code.")).
		WithProperty("id", stringSchema("Build record id.")).
		WithProperty("module", stringSchema("Module display name.")).
		WithProperty("operator", stringSchema("Worker or operator label.")).
		WithProperty("startedAt", stringSchema("Build start display time.")).
		WithProperty("status", enumStringSchema("Dashboard build status.", "failed", "queued", "running", "success"))
}

func dashboardReleaseRecordSchema() *openapi3.Schema {
	return objectSchema("application", "id", "environment", "gray", "module", "status", "updatedAt").
		WithProperty("application", stringSchema("Application display name.")).
		WithProperty("id", stringSchema("Release record id.")).
		WithProperty("environment", stringSchema("Environment code.")).
		WithProperty("gray", stringSchema("Release gray display label.")).
		WithProperty("module", stringSchema("Module display name.")).
		WithProperty("status", enumStringSchema("Release status.", "active", "inactive")).
		WithProperty("updatedAt", stringSchema("Release update display time."))
}

func dashboardResourceStatusSchema() *openapi3.Schema {
	return objectSchema("description", "href", "id", "label", "status", "statusLabel", "value").
		WithProperty("description", stringSchema("Resource description.")).
		WithProperty("href", stringSchema("Console route path.")).
		WithProperty("id", stringSchema("Resource id.")).
		WithProperty("label", stringSchema("Resource display label.")).
		WithProperty("status", enumStringSchema("Resource status tone.", "attention", "healthy", "neutral")).
		WithProperty("statusLabel", stringSchema("Resource status label.")).
		WithProperty("value", stringSchema("Resource count display value."))
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
		WithProperty("status", enumStringSchema("Environment status.", "healthy", "gray")).
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

func applicationMemberUserIDParameter() *openapi3.ParameterRef {
	return &openapi3.ParameterRef{
		Value: openapi3.NewPathParameter("userId").
			WithDescription("Console user id of the application member.").
			WithSchema(stringSchema("Application member user id.")),
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

func artifactAccessTokenIDParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewPathParameter("id").
				WithDescription("Artifact access token id.").
				WithSchema(stringSchema("Artifact access token id.")),
		},
	}
}

func moduleIDParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewPathParameter("id").
				WithDescription("Module id.").
				WithSchema(stringSchema("Module id.")),
		},
	}
}

func buildRecordIDParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewPathParameter("id").
				WithDescription("Build record UUID.").
				WithSchema(stringSchema("Build record UUID.")),
		},
	}
}

func artifactDownloadParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewPathParameter("applicationSlug").
				WithDescription("Application slug.").
				WithSchema(stringSchema("Application slug.")),
		},
		{
			Value: openapi3.NewPathParameter("moduleId").
				WithDescription("Module id.").
				WithSchema(stringSchema("Module id.")),
		},
		{
			Value: openapi3.NewPathParameter("environment").
				WithDescription("Environment code.").
				WithSchema(stringSchema("Environment code.")),
		},
		{
			Value: openapi3.NewPathParameter("buildId").
				WithDescription("Build record UUID.").
				WithSchema(stringSchema("Build record UUID.")),
		},
		{
			Value: openapi3.NewQueryParameter("platform").
				WithDescription("Optional platform. When present, path is relative to the platform directory, as returned by the application manifest.").
				WithSchema(enumStringSchema("Artifact platform.", domain.BuildPlatforms...)),
		},
		{
			Value: openapi3.NewQueryParameter("path").
				WithDescription("Artifact file relative path. Relative to the artifact root, or to the platform directory when platform is present.").
				WithRequired(true).
				WithSchema(stringSchema("Artifact file relative path.")),
		},
	}
}

func artifactManifestParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewPathParameter("applicationSlug").
				WithDescription("Application slug.").
				WithSchema(stringSchema("Application slug.")),
		},
		{
			Value: openapi3.NewPathParameter("environment").
				WithDescription("Environment code.").
				WithSchema(stringSchema("Environment code.")),
		},
		{
			Value: openapi3.NewQueryParameter("platform").
				WithDescription("Client platform. Only files under the {platform}/ directory are returned, with the platform prefix removed.").
				WithRequired(true).
				WithSchema(enumStringSchema("Client platform.", domain.BuildPlatforms...)),
		},
		{
			Value: openapi3.NewQueryParameter("runtimeVersion").
				WithDescription("Client runtimeVersion. Only routes with exactly this runtimeVersion are matched. 1-64 characters of [A-Za-z0-9._-].").
				WithRequired(true).
				WithSchema(stringSchema("Client runtimeVersion.")),
		},
		{
			Value: openapi3.NewQueryParameter("identifier").
				WithDescription("Optional targeting identifier. Targeted gray wins when present, percentage gray may use userId, otherwise default release is used.").
				WithSchema(stringSchema("Targeting identifier.")),
		},
		{
			Value: openapi3.NewQueryParameter("userId").
				WithDescription("Optional user id for percentage gray bucketing.").
				WithSchema(stringSchema("User id for percentage gray bucketing.")),
		},
	}
}

func codeSourceManifestCallbackParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewQueryParameter("code").
				WithDescription("Temporary GitHub App manifest code returned by GitHub.").
				WithSchema(stringSchema("GitHub App manifest code.")),
		},
	}
}

func codeSourceInstallCallbackParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewQueryParameter("installation_id").
				WithDescription("GitHub App installation id returned by setup URL.").
				WithSchema(int64Schema("GitHub App installation id.")),
		},
		{
			Value: openapi3.NewQueryParameter("setup_action").
				WithDescription("GitHub setup action, such as install or update.").
				WithSchema(enumStringSchema("GitHub setup action.", "install", "update")),
		},
		{
			Value: openapi3.NewQueryParameter("state").
				WithDescription("Opaque setup state returned by GitHub when present.").
				WithSchema(stringSchema("Setup state.")),
		},
	}
}

func codeSourceConnectionIDParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewPathParameter("id").
				WithDescription("Code source connection id.").
				WithSchema(stringSchema("Code source connection id.")),
		},
	}
}

func codeSourceConnectionListParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewQueryParameter("provider").
				WithDescription("Code source provider filter.").
				WithSchema(enumStringSchema("Code source provider.", "github", "gitlab")),
		},
		{
			Value: openapi3.NewQueryParameter("status").
				WithDescription("Connection status filter.").
				WithSchema(enumStringSchema("Connection status.", "active", "disabled")),
		},
	}
}

func codeSourceRepositorySearchParameters() openapi3.Parameters {
	return append(codeSourceConnectionIDParameters(),
		&openapi3.ParameterRef{
			Value: openapi3.NewQueryParameter("query").
				WithDescription("Repository search keyword.").
				WithSchema(stringSchema("Repository search keyword.")),
		},
		&openapi3.ParameterRef{
			Value: openapi3.NewQueryParameter("page").
				WithDescription("Page number, min 1, default 1.").
				WithSchema(int32Schema("Page number.")),
		},
		&openapi3.ParameterRef{
			Value: openapi3.NewQueryParameter("pageSize").
				WithDescription("Result size, min 1, max 100, default 20.").
				WithSchema(int32Schema("Page size.")),
		},
	)
}

func webhookHeaderParameters(names ...string) openapi3.Parameters {
	parameters := make(openapi3.Parameters, 0, len(names))
	for _, name := range names {
		parameters = append(parameters, &openapi3.ParameterRef{
			Value: openapi3.NewHeaderParameter(name).
				WithDescription("Provider webhook header " + name + ".").
				WithSchema(stringSchema(name)),
		})
	}
	return parameters
}

// codeSourceResponses 为代码源接口补齐公共错误响应。
func codeSourceResponses(items ...openapi3.NewResponsesOption) *openapi3.Responses {
	return responses(append(items,
		response(http.StatusBadRequest, "Invalid code source request or credential.", schemaRef("ErrorResponse")),
		response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
		response(http.StatusForbidden, "Current user cannot manage or use code source connections.", schemaRef("ErrorResponse")),
		response(http.StatusNotFound, "Code source connection, repository or installation not found.", schemaRef("ErrorResponse")),
		response(http.StatusServiceUnavailable, "Code source auth is not configured.", schemaRef("ErrorResponse")),
		response(http.StatusBadGateway, "Code source provider request failed.", schemaRef("ErrorResponse")),
		response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
	)...)
}

func webhookResponses() *openapi3.Responses {
	return responses(
		responseNoBody(http.StatusAccepted, "Push event matched at least one module auto build rule. Builds were created, or skipped because the same commit already has a pending, running or succeeded build."),
		responseNoBody(http.StatusNoContent, "Event ignored (not a branch push, branch deleted, tag push, connection disabled, or no module auto build rule matched)."),
		response(http.StatusBadRequest, "Invalid webhook payload.", schemaRef("ErrorResponse")),
		response(http.StatusUnauthorized, "Webhook signature or secret is invalid.", schemaRef("ErrorResponse")),
		response(http.StatusNotFound, "Code source connection not found.", schemaRef("ErrorResponse")),
		response(http.StatusRequestEntityTooLarge, "Webhook payload too large.", schemaRef("ErrorResponse")),
		response(http.StatusServiceUnavailable, "Webhook secret is not configured.", schemaRef("ErrorResponse")),
		response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
	)
}

func applicationListParameters() openapi3.Parameters {
	parameters := openapi3.Parameters{
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
				WithDescription("Owner user id filter.").
				WithSchema(stringSchema("Owner user id filter.")),
		},
		{
			Value: openapi3.NewQueryParameter("keyword").
				WithDescription("Fuzzy application name keyword.").
				WithSchema(stringSchema("Application name keyword.")),
		},
	}
	return append(parameters, paginationParameters()...)
}

func moduleListParameters() openapi3.Parameters {
	parameters := openapi3.Parameters{
		{
			Value: openapi3.NewQueryParameter("applicationId").
				WithDescription("Linked application id filter.").
				WithSchema(stringSchema("Application id filter.")),
		},
		{
			Value: openapi3.NewQueryParameter("kind").
				WithDescription("Module kind filter.").
				WithSchema(enumStringSchema("Module kind.", "Common", "Feature")),
		},
		{
			Value: openapi3.NewQueryParameter("owner").
				WithDescription("Owner user id filter.").
				WithSchema(stringSchema("Owner user id filter.")),
		},
		{
			Value: openapi3.NewQueryParameter("keyword").
				WithDescription("Fuzzy module keyword.").
				WithSchema(stringSchema("Module keyword.")),
		},
	}
	return append(parameters, paginationParameters()...)
}

func buildListParameters() openapi3.Parameters {
	parameters := openapi3.Parameters{
		{
			Value: openapi3.NewQueryParameter("applicationId").
				WithDescription("Linked application id filter.").
				WithSchema(stringSchema("Application id filter.")),
		},
		{
			Value: openapi3.NewQueryParameter("status").
				WithDescription("Build status filter.").
				WithSchema(enumStringSchema("Build status filter.", "pending", "running", "succeeded", "failed")),
		},
		{
			Value: openapi3.NewQueryParameter("moduleId").
				WithDescription("Module id filter.").
				WithSchema(stringSchema("Module id filter.")),
		},
		{
			Value: openapi3.NewQueryParameter("environment").
				WithDescription("Environment code filter.").
				WithSchema(stringSchema("Environment code filter.")),
		},
		{
			Value: openapi3.NewQueryParameter("triggerType").
				WithDescription("Build trigger type filter.").
				WithSchema(enumStringSchema("Build trigger type filter.", "manual", "webhook")),
		},
	}
	return append(parameters, paginationParameters()...)
}

func releaseListParameters() openapi3.Parameters {
	parameters := openapi3.Parameters{
		{
			Value: openapi3.NewQueryParameter("applicationId").
				WithDescription("Linked application id filter.").
				WithSchema(stringSchema("Application id filter.")),
		},
		{
			Value: openapi3.NewQueryParameter("moduleId").
				WithDescription("Module id filter.").
				WithSchema(stringSchema("Module id filter.")),
		},
		{
			Value: openapi3.NewQueryParameter("environment").
				WithDescription("Environment code filter.").
				WithSchema(stringSchema("Environment code filter.")),
		},
		{
			Value: openapi3.NewQueryParameter("status").
				WithDescription("Release status filter.").
				WithSchema(enumStringSchema("Release status filter.", "active", "inactive")),
		},
		{
			Value: openapi3.NewQueryParameter("releaseType").
				WithDescription("Release type filter.").
				WithSchema(enumStringSchema("Release type filter.", "full", "gray")),
		},
	}
	return append(parameters, paginationParameters()...)
}

func paginationParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewQueryParameter("page").
				WithDescription("One-based page number. Defaults to 1.").
				WithSchema(int32Schema("Page number.")),
		},
		{
			Value: openapi3.NewQueryParameter("pageSize").
				WithDescription("Rows per page, from 1 to 100. Defaults to 20.").
				WithSchema(int32Schema("Page size.")),
		},
	}
}

func releaseResolveParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewQueryParameter("moduleId").
				WithDescription("Module id.").
				WithRequired(true).
				WithSchema(stringSchema("Module id.")),
		},
		{
			Value: openapi3.NewQueryParameter("environment").
				WithDescription("Environment code.").
				WithRequired(true).
				WithSchema(stringSchema("Environment code.")),
		},
		{
			Value: openapi3.NewQueryParameter("runtimeVersion").
				WithDescription("Optional runtimeVersion. When present only routes of this runtimeVersion are resolved, otherwise the newest matching route of any runtimeVersion wins.").
				WithSchema(stringSchema("Route runtimeVersion.")),
		},
		{
			Value: openapi3.NewQueryParameter("identifier").
				WithDescription("Optional targeting identifier. Falls back to the default route when no identifier route exists.").
				WithSchema(stringSchema("Targeting identifier.")),
		},
		{
			Value: openapi3.NewQueryParameter("userId").
				WithDescription("Optional user id for percentage gray bucketing.").
				WithSchema(stringSchema("User id for percentage gray bucketing.")),
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
		WithProperty("status", enumStringSchema("Application status.", "running", "maintenance", "offline"))
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
		WithProperty("status", enumStringSchema("Environment binding status.", "healthy", "gray", "maintenance"))
}

func applicationModuleSchema() *openapi3.Schema {
	return objectSchema("id", "name", "kind", "repository", "owner").
		WithProperty("commonModuleId", stringSchema("Common module id for feature modules.")).
		WithProperty("commonModuleName", stringSchema("Common module name for feature modules.")).
		WithProperty("id", stringSchema("Module id.")).
		WithProperty("name", stringSchema("Module name.")).
		WithProperty("kind", enumStringSchema("Module kind.", "Common", "Feature")).
		WithProperty("repository", stringSchema("Module repository.")).
		WithProperty("owner", stringSchema("Module owner."))
}

func applicationMemberSchema() *openapi3.Schema {
	return objectSchema("id", "userId", "name", "email", "avatarUrl", "team", "role").
		WithProperty("id", stringSchema("Member id.")).
		WithProperty("userId", stringSchema("Console user id.")).
		WithProperty("name", stringSchema("Member name.")).
		WithProperty("email", stringSchema("Member email.")).
		WithProperty("avatarUrl", stringSchema("Member avatar url.")).
		WithProperty("team", stringSchema("Member department path.")).
		WithProperty("role", enumStringSchema("Member role.", "Owner", "Developer", "QA", "Release"))
}

func applicationMembersResponseSchema() *openapi3.Schema {
	return objectSchema("members").
		WithPropertyRef("members", arraySchemaRef("ApplicationMember"))
}

func applicationCapabilitiesSchema() *openapi3.Schema {
	return objectSchema("canEditApplication", "canManageMembers", "canUpdateStatus", "canCreateBuild", "canRelease").
		WithProperty("canEditApplication", boolSchema("Whether current user can edit this application.")).
		WithProperty("canManageMembers", boolSchema("Whether current user can manage this application members.")).
		WithProperty("canUpdateStatus", boolSchema("Whether current user can update this application status.")).
		WithProperty("canCreateBuild", boolSchema("Whether current user can create builds for this application.")).
		WithProperty("canRelease", boolSchema("Whether current user can create or rollback releases for this application."))
}

func applicationClientIntegrationSchema() *openapi3.Schema {
	return objectSchema("endpoint").
		WithPropertyRef("endpoint", schemaRef("ApplicationClientIntegrationEndpoint"))
}

func applicationClientIntegrationEndpointSchema() *openapi3.Schema {
	return objectSchema("baseUrl", "path", "method", "requestFields", "responseFields").
		WithProperty("baseUrl", stringSchema("Manifest API base URL for clients.")).
		WithProperty("path", stringSchema("Manifest API path.")).
		WithProperty("method", stringSchema("Manifest API HTTP method.")).
		WithProperty("description", stringSchema("Manifest API endpoint description.")).
		WithProperty("exampleUrl", stringSchema("Example manifest request URL.")).
		WithPropertyRef("requestFields", arraySchemaRef("ApplicationClientIntegrationField")).
		WithPropertyRef("responseFields", arraySchemaRef("ApplicationClientIntegrationField"))
}

func applicationClientIntegrationFieldSchema() *openapi3.Schema {
	return objectSchema("name", "description", "required", "type", "example").
		WithProperty("name", stringSchema("Field name.")).
		WithProperty("description", stringSchema("Field description.")).
		WithProperty("required", boolSchema("Whether the field is required.")).
		WithProperty("type", stringSchema("Field value type.")).
		WithProperty("example", stringSchema("Field example value."))
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
		"capabilities",
		"clientIntegration",
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
		WithPropertyRef("capabilities", schemaRef("ApplicationCapabilities")).
		WithPropertyRef("clientIntegration", schemaRef("ApplicationClientIntegration"))
}

func applicationsResponseSchema() *openapi3.Schema {
	return objectSchema("applications", "pagination", "summary").
		WithPropertyRef("applications", arraySchemaRef("Application")).
		WithPropertyRef("pagination", schemaRef("Pagination")).
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
		"ownerUserId",
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

func applicationStatusUpdateRequestSchema() *openapi3.Schema {
	return objectSchema("status").
		WithProperty("status", enumStringSchema("Application status code.", "running", "maintenance", "offline"))
}

func applicationDeleteCheckSchema() *openapi3.Schema {
	return objectSchema("deletable", "dependencies", "message").
		WithProperty("deletable", boolSchema("Whether application can be deleted.")).
		WithPropertyRef("dependencies", arraySchemaRef("ApplicationDeleteDependency")).
		WithProperty("message", stringSchema("Delete check message."))
}

func applicationDeleteDependencySchema() *openapi3.Schema {
	return objectSchema("id", "name", "type").
		WithProperty("id", stringSchema("Dependency id.")).
		WithProperty("name", stringSchema("Dependency display name.")).
		WithProperty("type", stringSchema("Dependency type.")).
		WithProperty("description", stringSchema("Dependency description."))
}

func applicationEnvironmentRequestSchema() *openapi3.Schema {
	return objectSchema("environment").
		WithProperty("environment", stringSchema("Environment code."))
}

func applicationMemberRequestSchema() *openapi3.Schema {
	return objectSchema("userId", "role").
		WithProperty("userId", stringSchema("Console user id to grant application role to.")).
		WithProperty("role", enumStringSchema("Member role.", "Owner", "Developer", "QA", "Release"))
}

func applicationMemberRoleUpdateRequestSchema() *openapi3.Schema {
	return objectSchema("role").
		WithProperty("role", enumStringSchema("Member role.", "Owner", "Developer", "QA", "Release"))
}

func modulesResponseSchema() *openapi3.Schema {
	return objectSchema("modules", "pagination", "summary").
		WithPropertyRef("modules", arraySchemaRef("Module")).
		WithPropertyRef("pagination", schemaRef("Pagination")).
		WithPropertyRef("summary", schemaRef("ModuleSummary"))
}

func moduleDetailResponseSchema() *openapi3.Schema {
	return objectSchema("module").
		WithPropertyRef("module", schemaRef("Module"))
}

func moduleSummarySchema() *openapi3.Schema {
	return objectSchema("total", "common", "feature").
		WithProperty("total", int32Schema("Module count.")).
		WithProperty("common", int32Schema("Common module count.")).
		WithProperty("feature", int32Schema("Feature module count."))
}

func moduleSchema() *openapi3.Schema {
	return objectSchema(
		"id",
		"name",
		"kind",
		"applicationId",
		"applicationName",
		"ownerUserId",
		"owner",
		"codeSourceConnectionId",
		"repositoryProvider",
		"repositoryId",
		"repositoryFullName",
		"repository",
		"buildConfig",
	).
		WithPropertyRef("buildConfig", schemaRef("ModuleBuildConfig")).
		WithProperty("codeSourceConnectionId", stringSchema("Code source connection id.")).
		WithProperty("codeSourceConnectionName", stringSchema("Code source connection display name.")).
		WithProperty("applicationId", stringSchema("Linked application id.")).
		WithProperty("applicationName", stringSchema("Linked application name.")).
		WithProperty("applicationSlug", stringSchema("Linked application slug.")).
		WithProperty("createdAt", dateTimeSchema("Module creation time.")).
		WithProperty("defaultBranch", stringSchema("Repository default branch.")).
		WithProperty("id", stringSchema("Module id.")).
		WithProperty("kind", enumStringSchema("Module kind.", "Common", "Feature")).
		WithProperty("commonModuleId", stringSchema("Common module id for feature modules.")).
		WithProperty("commonModuleName", stringSchema("Common module name for feature modules.")).
		WithProperty("name", stringSchema("Module display name.")).
		WithProperty("owner", stringSchema("Module owner.")).
		WithProperty("ownerAvatarUrl", stringSchema("Module owner avatar url.")).
		WithProperty("ownerEmail", stringSchema("Module owner email.")).
		WithProperty("ownerUserId", stringSchema("Module owner user id.")).
		WithProperty("repository", stringSchema("Repository display full name.")).
		WithProperty("repositoryCloneUrl", stringSchema("Repository HTTPS clone URL.")).
		WithProperty("repositoryFullName", stringSchema("Repository full name.")).
		WithProperty("repositoryId", stringSchema("Provider repository id (external repository id).")).
		WithProperty("repositoryProvider", enumStringSchema("Repository provider, decided by the connection.", "github", "gitlab")).
		WithProperty("repositoryWebUrl", stringSchema("Repository web URL.")).
		WithProperty("updatedAt", dateTimeSchema("Module update time."))
}

func moduleRequestSchema(includeID bool) *openapi3.Schema {
	required := []string{
		"name",
		"kind",
		"applicationId",
		"ownerUserId",
		"owner",
		"repository",
	}
	if includeID {
		required = append([]string{"id"}, required...)
	}
	schema := objectSchema(required...).
		WithProperty("applicationId", stringSchema("Linked application id.")).
		WithProperty("commonModuleId", stringSchema("Required for Feature modules. Must point to a Common module of the same application.")).
		WithProperty("kind", enumStringSchema("Module kind.", "Common", "Feature")).
		WithProperty("name", stringSchema("Module display name.")).
		WithProperty("owner", stringSchema("Module owner.")).
		WithProperty("ownerAvatarUrl", stringSchema("Module owner avatar url.")).
		WithProperty("ownerDepartmentIds", stringArraySchema("Feishu department ids of the owner.")).
		WithProperty("ownerEmail", stringSchema("Module owner email.")).
		WithProperty("ownerFeishuUnionId", stringSchema("Owner Feishu union_id.")).
		WithProperty("ownerFeishuOpenId", stringSchema("Owner Feishu open_id.")).
		WithProperty("ownerFeishuUserId", stringSchema("Owner Feishu user_id.")).
		WithProperty("ownerUserId", stringSchema("Module owner user id.")).
		WithPropertyRef("repository", schemaRef("ModuleRepositoryRequest")).
		WithPropertyRef("buildConfig", schemaRef("ModuleBuildConfig"))
	if includeID {
		schema.WithProperty("id", stringSchema("Immutable module id."))
	}
	return schema
}

func moduleRepositoryRequestSchema() *openapi3.Schema {
	return objectSchema("connectionId", "externalRepositoryId").
		WithProperty("connectionId", stringSchema("Code source connection id.")).
		WithProperty("externalRepositoryId", stringSchema("Repository id returned by the connection repository search. Repository name and URLs are taken from the server side repository cache."))
}

func moduleBuildConfigSchema() *openapi3.Schema {
	return objectSchema().
		WithProperty("buildMode", enumStringSchema("Build mode. dockerfile uses the repository Dockerfile (default); adapter uses a platform build adapter.", "dockerfile", "adapter")).
		WithProperty("buildAdapterId", stringSchema("Build adapter id. Required in adapter mode and must be empty in dockerfile mode. A newly selected adapter must be active.")).
		WithProperty("buildAdapterName", stringSchema("Build adapter display name. Read only.")).
		WithProperty("buildAdapterParams", stringMapSchema("Build adapter parameter values keyed by parameter key. Validated against the adapter parameter definitions: required parameters need a value and undeclared keys are rejected. Must be empty in dockerfile mode. Do not put secrets here; use build variables.")).
		WithProperty("autoBuildBranches", stringArraySchema("Branch globs that trigger builds on push. Empty disables automatic builds.")).
		WithProperty("autoBuildEnvironment", stringSchema("Environment code for automatic builds. Required when autoBuildBranches is not empty.")).
		WithProperty("autoPreviewRelease", boolSchema("Create a targeted release named after the branch when an automatic build succeeds.")).
		WithProperty("autoPreviewTtlHours", int32Schema("Preview release lifetime in hours, 1-8760, default 168.")).
		WithProperty("buildContext", stringSchema("Docker build context relative to the repository root, default \".\".")).
		WithProperty("dockerfilePath", stringSchema("Dockerfile path relative to the repository root. Defaults to FORGE_WORKER_DOCKERFILE. Only used in dockerfile mode."))
}

func buildRecordCreateRequestSchema() *openapi3.Schema {
	return objectSchema("moduleId", "environment").
		WithProperty("branch", stringSchema("Git branch to build. Defaults to the module default branch when omitted.")).
		WithProperty("moduleId", stringSchema("Module id to build.")).
		WithProperty("environment", stringSchema("Environment code whose storage configuration receives artifacts. Console builds are always manual."))
}

func buildRecordDetailResponseSchema() *openapi3.Schema {
	return objectSchema("build").
		WithPropertyRef("build", schemaRef("BuildRecord"))
}

func buildRecordsResponseSchema() *openapi3.Schema {
	return objectSchema("builds", "pagination", "summary").
		WithPropertyRef("builds", arraySchemaRef("BuildRecord")).
		WithPropertyRef("pagination", schemaRef("Pagination")).
		WithPropertyRef("summary", schemaRef("BuildRecordSummary"))
}

func buildRecordSummarySchema() *openapi3.Schema {
	return objectSchema("failed", "pending", "running", "succeeded", "total").
		WithProperty("failed", int32Schema("Failed build count.")).
		WithProperty("pending", int32Schema("Pending build count.")).
		WithProperty("running", int32Schema("Running build count.")).
		WithProperty("succeeded", int32Schema("Succeeded build count.")).
		WithProperty("total", int32Schema("Total build count."))
}

func buildRecordSchema() *openapi3.Schema {
	return objectSchema(
		"id",
		"number",
		"applicationName",
		"moduleId",
		"moduleName",
		"environment",
		"environmentName",
		"branch",
		"status",
		"triggerType",
		"repositoryProvider",
		"repositoryId",
		"repositoryFullName",
		"codeSourceConnectionId",
		"commitSha",
		"runtimeVersion",
		"platforms",
		"storageConfigurationId",
		"storageConfigurationName",
		"queuedAt",
		"createdAt",
		"createdBy",
		"dependencyBuilds",
		"updatedAt",
	).
		WithProperty("applicationName", stringSchema("Application display name used in artifact object keys.")).
		WithPropertyRef("artifactTree", schemaRef("BuildArtifactTree")).
		WithProperty("createdBy", nullableSchemaRef("UserSummary", "Build creator. Manual builds record the console user; webhook builds record the code source connection creator. Null for historical builds or deleted users.")).
		WithProperty("artifactKey", stringSchema("S3 object key root produced by the worker.")).
		WithProperty("artifactUrl", stringSchema("Public artifact base URL when storage configuration provides one.")).
		WithProperty("branch", stringSchema("Git branch selected for this build record.")).
		WithProperty("codeSourceConnectionId", stringSchema("Code source connection id of the module repository.")).
		WithProperty("commitSha", stringSchema("Commit built by this record. Webhook builds record the pushed commit; manual builds fill it after checkout.")).
		WithProperty("createdAt", dateTimeSchema("Build record creation time.")).
		WithProperty("defaultBranch", stringSchema("Repository branch used by the worker when configured on the module.")).
		WithPropertyRef("dependencyBuilds", arraySchemaRef("BuildDependency")).
		WithProperty("environment", stringSchema("Environment code.")).
		WithProperty("environmentName", stringSchema("Environment display name used in artifact object keys.")).
		WithProperty("errorMessage", stringSchema("Build failure message.")).
		WithProperty("finishedAt", dateTimeSchema("Build finish time.")).
		WithProperty("id", stringSchema("Build record UUID.")).
		WithProperty("number", int64Schema("Build number, increasing within the module.")).
		WithProperty("buildAdapterId", stringSchema("Build adapter used by an adapter build. Empty for Dockerfile builds or before rendering.")).
		WithProperty("buildAdapterVersion", int32Schema("Build adapter version used by an adapter build.").WithNullable()).
		WithProperty("renderedDockerfileSha256", stringSchema("SHA-256 of the rendered Dockerfile for adapter builds. Empty for Dockerfile builds.")).
		WithProperty("manifestKey", stringSchema("S3 object key of the artifact manifest.")).
		WithProperty("fileCount", int32Schema("Uploaded artifact file count.")).
		WithProperty("totalSizeBytes", int64Schema("Total uploaded artifact file size in bytes.")).
		WithProperty("moduleId", stringSchema("Module id.")).
		WithProperty("moduleName", stringSchema("Module display name.")).
		WithProperty("queuedAt", dateTimeSchema("Build queue time.")).
		WithProperty("repositoryFullName", stringSchema("Repository full name.")).
		WithProperty("platforms", stringArraySchema("Platform directories produced by the build: android, ios, harmonyos.")).
		WithProperty("repositoryId", stringSchema("Provider repository id (external repository id).")).
		WithProperty("repositoryProvider", enumStringSchema("Repository provider.", "github", "gitlab")).
		WithProperty("repositoryWebUrl", stringSchema("Repository web URL.")).
		WithProperty("runtimeVersion", stringSchema("runtimeVersion from forge.json. Empty until the build succeeds.")).
		WithProperty("startedAt", dateTimeSchema("Build start time.")).
		WithProperty("status", enumStringSchema("Build status.", "pending", "running", "succeeded", "failed")).
		WithProperty("storageConfigurationId", stringSchema("Storage configuration id bound to the environment at queue time.")).
		WithProperty("storageConfigurationName", stringSchema("Storage configuration display name.")).
		WithProperty("triggerId", stringSchema("Trigger source id, such as the webhook delivery id. Empty for manual builds.")).
		WithProperty("triggerType", enumStringSchema("Build trigger type.", "manual", "webhook")).
		WithProperty("updatedAt", dateTimeSchema("Build record update time.")).
		WithProperty("workerId", stringSchema("Worker id that claimed the build."))
}

func buildDependencySchema() *openapi3.Schema {
	schema := objectSchema("moduleId", "moduleName", "runtimeVersion", "buildRecordId", "buildNumber").
		WithProperty("moduleId", stringSchema("Dependency module id.")).
		WithProperty("moduleName", stringSchema("Dependency module display name. Empty when the module no longer exists.")).
		WithProperty("runtimeVersion", stringSchema("runtimeVersion of the dependency build.")).
		WithProperty("buildRecordId", stringSchema("Dependency build record UUID.")).
		WithProperty("buildNumber", int64Schema("Dependency build number. 0 when the build record no longer exists."))
	schema.Description = "Dependency build injected into the build context under .forge/deps/. Feature modules inject the active default route builds of their Common module in the same environment."
	return schema
}

func userSummarySchema() *openapi3.Schema {
	return objectSchema("id", "name", "avatarUrl").
		WithProperty("id", stringSchema("User id.")).
		WithProperty("name", stringSchema("User name.")).
		WithProperty("avatarUrl", stringSchema("Avatar URL. Empty when the user has no avatar."))
}

func buildArtifactTreeSchema() *openapi3.Schema {
	return objectSchema("fileCount", "totalSizeBytes", "nodes").
		WithProperty("fileCount", int32Schema("Uploaded artifact file count.")).
		WithProperty("totalSizeBytes", int64Schema("Total uploaded artifact file size in bytes.")).
		WithPropertyRef("nodes", arraySchemaRef("BuildArtifactTreeNode"))
}

func buildArtifactTreeNodeSchema() *openapi3.Schema {
	children := openapi3.NewArraySchema()
	children.Items = schemaRef("BuildArtifactTreeNode")
	return objectSchema("name", "path", "type", "sizeBytes").
		WithProperty("children", children).
		WithProperty("contentType", stringSchema("File content type.")).
		WithProperty("name", stringSchema("File or directory name.")).
		WithProperty("objectKey", stringSchema("S3 object key for file nodes.")).
		WithProperty("path", stringSchema("Relative artifact path.")).
		WithProperty("sha256", stringSchema("File SHA-256 digest.")).
		WithProperty("sizeBytes", int64Schema("File size or aggregated directory size in bytes.")).
		WithProperty("type", enumStringSchema("Artifact tree node type.", "directory", "file"))
}

func artifactManifestResponseSchema() *openapi3.Schema {
	return objectSchema("application", "environment", "platform", "runtimeVersion", "identifier", "modules", "missingModules").
		WithPropertyRef("application", schemaRef("ArtifactManifestApplication")).
		WithPropertyRef("environment", schemaRef("ArtifactManifestEnvironment")).
		WithProperty("platform", enumStringSchema("Requested platform.", domain.BuildPlatforms...)).
		WithProperty("runtimeVersion", stringSchema("Requested runtimeVersion.")).
		WithProperty("identifier", stringSchema("Requested targeting identifier.")).
		WithPropertyRef("modules", arraySchemaRef("ArtifactManifestModule")).
		WithPropertyRef("missingModules", arraySchemaRef("ArtifactMissingModule"))
}

func artifactManifestApplicationSchema() *openapi3.Schema {
	return objectSchema("id", "name", "slug").
		WithProperty("id", stringSchema("Application id.")).
		WithProperty("name", stringSchema("Application display name.")).
		WithProperty("slug", stringSchema("Application slug."))
}

func artifactManifestEnvironmentSchema() *openapi3.Schema {
	return objectSchema("id", "name").
		WithProperty("id", stringSchema("Environment code.")).
		WithProperty("name", stringSchema("Environment display name."))
}

func artifactManifestModuleSchema() *openapi3.Schema {
	return objectSchema("id", "name", "kind", "releaseId", "releaseType", "buildRecordId", "branch", "commitSha", "artifactKey", "manifestKey", "files").
		WithProperty("id", stringSchema("Module id.")).
		WithProperty("name", stringSchema("Module display name.")).
		WithProperty("kind", enumStringSchema("Module kind.", "Common", "Feature")).
		WithProperty("releaseId", stringSchema("Matched release UUID.")).
		WithProperty("releaseType", enumStringSchema("Matched release type.", "full", "gray")).
		WithProperty("grayStrategy", enumStringSchema("Matched gray strategy.", "", "targeted", "percentage")).
		WithProperty("grayPercentage", int32Schema("Matched percentage gray value.")).
		WithProperty("identifier", stringSchema("Matched targeting identifier for targeted gray releases.")).
		WithProperty("buildRecordId", stringSchema("Matched build record UUID.")).
		WithProperty("branch", stringSchema("Branch of the matched build.")).
		WithProperty("commitSha", stringSchema("Commit SHA of the matched build.")).
		WithProperty("artifactKey", stringSchema("Artifact object key root.")).
		WithProperty("manifestKey", stringSchema("Artifact manifest object key.")).
		WithPropertyRef("files", arraySchemaRef("ArtifactManifestFile"))
}

func artifactManifestFileSchema() *openapi3.Schema {
	digests := openapi3.NewObjectSchema().WithAdditionalProperties(stringSchema("Hex digest."))
	digests.Description = "File digests keyed by algorithm (sha256, md5), configured by FORGE_ARTIFACT_DIGESTS on the worker. Manifests generated before digests existed fall back to {sha256}."
	return objectSchema("path", "url", "contentType", "sizeBytes", "sha256", "digests").
		WithProperty("path", stringSchema("File path relative to the requested platform directory (platform prefix removed).")).
		WithProperty("url", stringSchema("Download URL. Uses public base URL when configured, otherwise the artifact download API with platform and path.")).
		WithProperty("contentType", stringSchema("File content type.")).
		WithProperty("sizeBytes", int64Schema("File size in bytes.")).
		WithProperty("sha256", stringSchema("File SHA-256 digest. Kept for compatibility; prefer digests.")).
		WithProperty("digests", digests)
}

func artifactMissingModuleSchema() *openapi3.Schema {
	return objectSchema("id", "name", "kind", "reason").
		WithProperty("id", stringSchema("Module id.")).
		WithProperty("name", stringSchema("Module display name.")).
		WithProperty("kind", enumStringSchema("Module kind.", "Common", "Feature")).
		WithProperty("reason", enumStringSchema(
			"Why this module is absent from the manifest modules list. no_release_for_runtime: no active route under the requested runtimeVersion; release not found: routes exist under the runtimeVersion but none matched; platform_unavailable: the matched build does not contain the requested platform.",
			domain.ArtifactMissingReasonNoReleaseForRuntime,
			domain.ArtifactMissingReasonReleaseNotFound,
			domain.ArtifactMissingReasonPlatformUnavailable,
			domain.ArtifactMissingReasonBuildNotFound,
			domain.ArtifactMissingReasonArtifactUnavailable,
		))
}

func releaseCreateRequestSchema() *openapi3.Schema {
	return objectSchema("buildRecordId", "releaseType").
		WithProperty("buildRecordId", stringSchema("Succeeded build record UUID to release.")).
		WithProperty("releaseType", enumStringSchema("Release type. full updates the default route, gray updates a targeted or percentage gray route.", "full", "gray")).
		WithProperty("grayStrategy", enumStringSchema("Gray strategy. Required for gray releases.", "", "targeted", "percentage")).
		WithProperty("grayPercentage", int32Schema("Initial percentage for percentage gray releases. Create supports 0 to 99.")).
		WithProperty("expiresAt", dateTimeSchema("Expiration time. Required for targeted gray releases and forbidden otherwise.")).
		WithProperty("identifier", stringSchema("Targeting identifier. Required for targeted gray releases and forbidden otherwise.")).
		WithProperty("description", stringSchema("Release description."))
}

func releaseGrayPercentageUpdateRequestSchema() *openapi3.Schema {
	return objectSchema("grayPercentage").
		WithProperty("grayPercentage", int32Schema("Updated percentage gray value. 100 promotes this release to full."))
}

func releaseDetailResponseSchema() *openapi3.Schema {
	return objectSchema("release", "route").
		WithPropertyRef("release", schemaRef("ReleaseRecord")).
		WithPropertyRef("route", schemaRef("ReleaseRoute"))
}

func releasesResponseSchema() *openapi3.Schema {
	return objectSchema("releases", "pagination", "summary").
		WithPropertyRef("releases", arraySchemaRef("ReleaseRecord")).
		WithPropertyRef("pagination", schemaRef("Pagination")).
		WithPropertyRef("summary", schemaRef("ReleaseRecordSummary"))
}

func releaseRecordSummarySchema() *openapi3.Schema {
	return objectSchema("active", "inactive", "total").
		WithProperty("active", int32Schema("Active release count.")).
		WithProperty("inactive", int32Schema("Inactive release count.")).
		WithProperty("total", int32Schema("Total release count."))
}

func releaseResolveResponseSchema() *openapi3.Schema {
	return objectSchema("route").
		WithPropertyRef("route", schemaRef("ReleaseRoute"))
}

func releaseRecordSchema() *openapi3.Schema {
	return objectSchema(
		"id",
		"moduleId",
		"moduleName",
		"environment",
		"environmentName",
		"buildRecordId",
		"releaseType",
		"runtimeVersion",
		"status",
		"artifactKey",
		"createdByUserId",
		"createdAt",
		"updatedAt",
	).
		WithProperty("applicationName", stringSchema("Application display name.")).
		WithProperty("artifactKey", stringSchema("Released artifact object key root.")).
		WithProperty("artifactUrl", stringSchema("Released artifact public URL.")).
		WithProperty("buildRecordId", stringSchema("Build record UUID used by this release.")).
		WithProperty("createdAt", dateTimeSchema("Release creation time.")).
		WithProperty("createdBy", stringSchema("Release creator display name.")).
		WithProperty("createdByUserId", stringSchema("Release creator user id.")).
		WithProperty("description", stringSchema("Release description.")).
		WithProperty("environment", stringSchema("Environment code.")).
		WithProperty("environmentName", stringSchema("Environment display name.")).
		WithProperty("id", stringSchema("Release UUID.")).
		WithProperty("identifier", stringSchema("Targeting identifier for targeted gray releases.")).
		WithProperty("moduleId", stringSchema("Module id.")).
		WithProperty("moduleName", stringSchema("Module display name.")).
		WithProperty("releaseType", enumStringSchema("Release type.", "full", "gray")).
		WithProperty("runtimeVersion", stringSchema("runtimeVersion of the release route. A release only replaces releases of the same runtimeVersion.")).
		WithProperty("grayStrategy", enumStringSchema("Gray strategy.", "", "targeted", "percentage")).
		WithProperty("grayPercentage", int32Schema("Percentage gray value.")).
		WithProperty("expiresAt", stringSchema("Expiration time for targeted gray releases, or empty otherwise.")).
		WithProperty("status", enumStringSchema("Release status.", "active", "inactive")).
		WithProperty("updatedAt", dateTimeSchema("Release update time."))
}

func releaseRouteSchema() *openapi3.Schema {
	return objectSchema(
		"id",
		"moduleId",
		"moduleName",
		"environment",
		"environmentName",
		"releaseId",
		"buildRecordId",
		"runtimeVersion",
		"routeType",
		"status",
		"artifactKey",
		"createdByUserId",
		"createdAt",
		"updatedAt",
	).
		WithProperty("applicationName", stringSchema("Application display name.")).
		WithProperty("artifactKey", stringSchema("Resolved artifact object key root.")).
		WithProperty("artifactUrl", stringSchema("Resolved artifact public URL.")).
		WithProperty("buildRecordId", stringSchema("Build record UUID used by this route.")).
		WithProperty("createdAt", dateTimeSchema("Route creation time.")).
		WithProperty("createdBy", stringSchema("Route creator display name.")).
		WithProperty("createdByUserId", stringSchema("Route creator user id.")).
		WithProperty("environment", stringSchema("Environment code.")).
		WithProperty("environmentName", stringSchema("Environment display name.")).
		WithProperty("id", stringSchema("Release route id.")).
		WithProperty("identifier", stringSchema("Targeting identifier for identifier routes.")).
		WithProperty("moduleId", stringSchema("Module id.")).
		WithProperty("moduleName", stringSchema("Module display name.")).
		WithProperty("releaseId", stringSchema("Release UUID.")).
		WithProperty("routeType", enumStringSchema("Route type.", "default", "identifier", "percentage")).
		WithProperty("runtimeVersion", stringSchema("runtimeVersion copied from the build record. Routes are isolated per runtimeVersion.")).
		WithProperty("grayStrategy", enumStringSchema("Gray strategy.", "", "targeted", "percentage")).
		WithProperty("grayPercentage", int32Schema("Percentage gray value.")).
		WithProperty("status", enumStringSchema("Release route status.", "active", "inactive")).
		WithProperty("updatedAt", dateTimeSchema("Route update time."))
}

func codeSourcesResponseSchema() *openapi3.Schema {
	return objectSchema("apps").
		WithPropertyRef("apps", arraySchemaRef("CodeSourceApp"))
}

func codeSourceAppSchema() *openapi3.Schema {
	return objectSchema("configurationStatus", "connectionCount", "description", "id", "name", "provider", "status").
		WithProperty("configurationStatus", enumStringSchema("Code source app configuration status.", "configured", "notConfigured")).
		WithProperty("connectionCount", int32Schema("Active installation connection count.")).
		WithProperty("description", stringSchema("Code source app description.")).
		WithProperty("id", enumStringSchema("Code source app id.", "github")).
		WithProperty("installedAt", dateTimeSchema("Installation time.")).
		WithProperty("name", stringSchema("Code source app display name.")).
		WithProperty("provider", enumStringSchema("Code source provider.", "github")).
		WithProperty("status", enumStringSchema("Code source app installation status.", "installed", "notInstalled"))
}

func codeSourceAppManifestSchema() *openapi3.Schema {
	return objectSchema("actionUrl", "manifest").
		WithProperty("actionUrl", stringSchema("GitHub App manifest creation endpoint.")).
		WithProperty("manifest", stringSchema("Serialized GitHub App manifest payload."))
}

func codeSourceInstallURLSchema() *openapi3.Schema {
	return objectSchema("url").
		WithProperty("url", stringSchema("Code source provider installation URL."))
}

func codeSourceRepositorySearchResponseSchema() *openapi3.Schema {
	return objectSchema("repositories", "hasMore", "page", "pageSize").
		WithPropertyRef("repositories", arraySchemaRef("CodeSourceRepository")).
		WithProperty("hasMore", boolSchema("Whether more repositories are available.")).
		WithProperty("page", int32Schema("Effective page number.")).
		WithProperty("pageSize", int32Schema("Effective page size."))
}

func codeSourceRepositorySchema() *openapi3.Schema {
	return objectSchema("connectionId", "externalRepositoryId", "fullName", "private").
		WithProperty("cloneUrl", stringSchema("HTTPS clone URL.")).
		WithProperty("connectionId", stringSchema("Code source connection id.")).
		WithProperty("defaultBranch", stringSchema("Default branch name.")).
		WithProperty("description", stringSchema("Repository description.")).
		WithProperty("externalRepositoryId", stringSchema("Provider repository id (GitHub repository id, GitLab project id).")).
		WithProperty("fullName", stringSchema("Repository full name, such as owner/name or group/subgroup/project.")).
		WithProperty("lastSyncedAt", dateTimeSchema("Repository cache sync time.")).
		WithProperty("private", boolSchema("Whether repository is private.")).
		WithProperty("webUrl", stringSchema("Repository web URL."))
}

func codeSourceConnectionSchema() *openapi3.Schema {
	return objectSchema("id", "provider", "name", "webBaseUrl", "apiBaseUrl", "status", "createdByUserId", "createdAt", "updatedAt").
		WithProperty("apiBaseUrl", stringSchema("Provider API base URL.")).
		WithProperty("createdAt", dateTimeSchema("Connection creation time.")).
		WithProperty("createdBy", stringSchema("Creator display name.")).
		WithProperty("createdByUserId", stringSchema("Creator platform user id.")).
		WithProperty("credentialExpiresAt", stringSchema("Credential expiration time, or empty when unknown or never expires.")).
		WithProperty("credentialPreview", stringSchema("Masked credential preview. Credential itself is never returned.")).
		WithProperty("externalAccount", stringSchema("GitHub installation account or GitLab token user.")).
		WithProperty("externalId", stringSchema("Provider level id, such as GitHub installation id. Empty for GitLab.")).
		WithProperty("id", stringSchema("Connection id.")).
		WithProperty("lastVerifiedAt", stringSchema("Last successful verification time, or empty.")).
		WithProperty("name", stringSchema("Connection display name.")).
		WithProperty("provider", enumStringSchema("Code source provider.", "github", "gitlab")).
		WithProperty("status", enumStringSchema("Connection status.", "active", "disabled")).
		WithProperty("updatedAt", dateTimeSchema("Connection update time.")).
		WithProperty("webBaseUrl", stringSchema("Provider web base URL, such as https://github.com or the GitLab instance URL."))
}

func codeSourceConnectionsResponseSchema() *openapi3.Schema {
	return objectSchema("connections").
		WithPropertyRef("connections", arraySchemaRef("CodeSourceConnection"))
}

func codeSourceConnectionDetailResponseSchema() *openapi3.Schema {
	return objectSchema("connection").
		WithPropertyRef("connection", schemaRef("CodeSourceConnection"))
}

func codeSourceConnectionCreateRequestSchema() *openapi3.Schema {
	return objectSchema("provider", "name", "webBaseUrl", "credential").
		WithProperty("apiBaseUrl", stringSchema("Provider API base URL. Defaults to webBaseUrl.")).
		WithProperty("credential", stringSchema("Access token, such as a GitLab group or project access token with read_api and read_repository scopes.")).
		WithProperty("name", stringSchema("Connection display name.")).
		WithProperty("provider", enumStringSchema("Code source provider. GitHub connections are created by the App installation callback.", "github", "gitlab")).
		WithProperty("webBaseUrl", stringSchema("Provider web base URL, such as https://gitlab.example.com."))
}

func codeSourceConnectionUpdateRequestSchema() *openapi3.Schema {
	return objectSchema().
		WithProperty("credential", stringSchema("New access token. The connection is re-verified when provided.")).
		WithProperty("name", stringSchema("Connection display name.")).
		WithProperty("status", enumStringSchema("Connection status.", "active", "disabled"))
}

func codeSourceConnectionWebhookSchema() *openapi3.Schema {
	return objectSchema("provider", "path", "secret").
		WithProperty("path", stringSchema("Webhook path on forge-server, prefix it with the public API origin.")).
		WithProperty("provider", enumStringSchema("Code source provider.", "github", "gitlab")).
		WithProperty("secret", stringSchema("Webhook secret token. Empty when the provider receives webhooks at App level (GitHub)."))
}

func artifactAccessTokenSchema() *openapi3.Schema {
	return objectSchema("id", "name", "tokenPreview", "status", "createdBy", "createdAt", "lastUsedAt", "expiresAt").
		WithProperty("id", stringSchema("Artifact access token id.")).
		WithProperty("name", stringSchema("Artifact access token display name.")).
		WithProperty("tokenPreview", stringSchema("Masked token preview.")).
		WithProperty("status", enumStringSchema("Artifact access token status.", "active", "disabled")).
		WithProperty("createdBy", stringSchema("Creator display name.")).
		WithProperty("createdAt", dateTimeSchema("Creation time.")).
		WithProperty("lastUsedAt", stringSchema("Last usage time, or empty when never used.")).
		WithProperty("expiresAt", stringSchema("Expiration time, or empty when token does not expire."))
}

func artifactAccessTokensResponseSchema() *openapi3.Schema {
	return objectSchema("tokens").
		WithPropertyRef("tokens", arraySchemaRef("ArtifactAccessToken"))
}

func artifactAccessTokenDetailResponseSchema() *openapi3.Schema {
	return objectSchema("token").
		WithPropertyRef("token", schemaRef("ArtifactAccessToken"))
}

func artifactAccessTokenGenerateResponseSchema() *openapi3.Schema {
	return objectSchema("plainToken").
		WithProperty("plainToken", stringSchema("One-time generated plain token."))
}

func artifactAccessTokenCreateRequestSchema() *openapi3.Schema {
	return objectSchema("name", "token").
		WithProperty("name", stringSchema("Artifact access token display name.")).
		WithProperty("token", stringSchema("Generated artifact access token value.")).
		WithProperty("expiresAt", dateTimeSchema("Optional token expiration time."))
}

func artifactAccessTokenUpdateRequestSchema() *openapi3.Schema {
	return objectSchema("status").
		WithProperty("status", enumStringSchema("Artifact access token status.", "active", "disabled"))
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
		WithProperty("status", enumStringSchema("Storage status.", "healthy", "error", "disabled"))
}

func storageConfigurationsResponseSchema() *openapi3.Schema {
	return objectSchema("configurations", "pagination").
		WithPropertyRef("configurations", arraySchemaRef("StorageConfiguration")).
		WithPropertyRef("pagination", schemaRef("Pagination"))
}

func paginationSchema() *openapi3.Schema {
	return objectSchema("page", "pageSize", "total").
		WithProperty("page", int32Schema("One-based page number.")).
		WithProperty("pageSize", int32Schema("Rows per page.")).
		WithProperty("total", int32Schema("Total rows matching the filters."))
}

func storageConfigurationDetailResponseSchema() *openapi3.Schema {
	return objectSchema("configuration").
		WithPropertyRef("configuration", schemaRef("StorageConfiguration"))
}

func storageConfigurationRequestSchema(requireCredentials bool) *openapi3.Schema {
	required := []string{"name", "adapter", "bucket", "endpoint", "region", "ownerUserId", "owner"}
	credentialNote := " Leave both credentials empty to keep the stored pair."
	if requireCredentials {
		required = append(required, "accessKeyId", "secretAccessKey")
		credentialNote = ""
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
		WithProperty("status", enumStringSchema("Storage status.", "healthy", "error", "disabled")).
		WithProperty("accessKeyId", stringSchema("Access key id. Not returned by read APIs."+credentialNote)).
		WithProperty("secretAccessKey", stringSchema("Secret access key. Not returned by read APIs."+credentialNote))
}

func storageConfigurationTestRequestSchema() *openapi3.Schema {
	return objectSchema("name", "adapter", "bucket", "endpoint", "region", "ownerUserId", "owner").
		WithProperty("id", stringSchema("Existing storage configuration id. When set and both credentials are empty, the stored pair is used.")).
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
		WithProperty("status", enumStringSchema("Storage status.", "healthy", "error", "disabled")).
		WithProperty("accessKeyId", stringSchema("Access key id. Required unless id is set and both credentials are empty.")).
		WithProperty("secretAccessKey", stringSchema("Secret access key. Required unless id is set and both credentials are empty."))
}

func storageConfigurationTestResponseSchema() *openapi3.Schema {
	return objectSchema("ok", "testedAt").
		WithProperty("ok", boolSchema("Whether the connectivity test passed.")).
		WithProperty("testedAt", dateTimeSchema("Test completion time."))
}

func globalCapabilitiesSchema() *openapi3.Schema {
	return objectSchema(
		"canCreateApplication",
		"canManageApplications",
		"canManageModules",
		"canCreateBuild",
		"canCreateRelease",
		"canManageCodeSources",
		"canManageStorage",
		"canManageEnvironments",
		"canManageAccessTokens",
		"canManageBuildAdapters",
	).
		WithProperty("canCreateApplication", boolSchema("Whether current user can create applications.")).
		WithProperty("canManageApplications", boolSchema("Whether current user can manage applications globally.")).
		WithProperty("canManageModules", boolSchema("Whether current user can manage modules globally.")).
		WithProperty("canCreateBuild", boolSchema("Whether current user can create builds globally.")).
		WithProperty("canCreateRelease", boolSchema("Whether current user can create or rollback releases globally.")).
		WithProperty("canManageCodeSources", boolSchema("Whether current user can manage code source integrations.")).
		WithProperty("canManageStorage", boolSchema("Whether current user can manage storage configurations.")).
		WithProperty("canManageEnvironments", boolSchema("Whether current user can manage environments.")).
		WithProperty("canManageAccessTokens", boolSchema("Whether current user can manage artifact access tokens.")).
		WithProperty("canManageBuildAdapters", boolSchema("Whether current user can create, edit, disable and enable build adapters."))
}

func feishuAuthResponseSchema() *openapi3.Schema {
	return objectSchema("capabilities", "roles", "user", "token").
		WithPropertyRef("capabilities", schemaRef("GlobalCapabilities")).
		WithProperty("roles", stringArraySchema("Global roles assigned to the current user.")).
		WithPropertyRef("user", schemaRef("FeishuUser")).
		WithPropertyRef("token", schemaRef("FeishuToken"))
}

func feishuUserSchema() *openapi3.Schema {
	return objectSchema("id", "unionId").
		WithProperty("id", stringSchema("Console user id derived from open_id, user_id, or union_id.")).
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
	return objectSchema("name", "departmentIds").
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

func int64Schema(description string) *openapi3.Schema {
	schema := openapi3.NewInt64Schema()
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

func intArraySchema(description string) *openapi3.Schema {
	schema := openapi3.NewArraySchema()
	schema.Description = description
	schema.Items = schemaValue(int32Schema(""))
	return schema
}

func stringMapSchema(description string) *openapi3.Schema {
	schema := openapi3.NewObjectSchema().WithAdditionalProperties(stringSchema(""))
	schema.Description = description
	return schema
}

func arraySchemaRef(itemSchema string) *openapi3.SchemaRef {
	schema := openapi3.NewArraySchema()
	schema.Items = schemaRef(itemSchema)
	return schemaValue(schema)
}

func buildLogParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewQueryParameter("afterSeq").
				WithDescription("Return chunks with seq greater than this value. Omit to read from the beginning.").
				WithSchema(int32Schema("Last seq the client already has.")),
		},
	}
}

func buildVariableDeleteParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewQueryParameter("key").
				WithDescription("Build variable key.").
				WithRequired(true).
				WithSchema(stringSchema("Build variable key.")),
		},
		{
			Value: openapi3.NewQueryParameter("environmentCode").
				WithDescription("Environment code. Omit or leave empty for the variable shared by all environments.").
				WithSchema(stringSchema("Environment code.")),
		},
	}
}

func buildLogChunkSchema() *openapi3.Schema {
	return objectSchema("seq", "content").
		WithProperty("seq", int32Schema("Chunk sequence number, increasing within one build.")).
		WithProperty("content", stringSchema("Redacted git and docker output."))
}

func buildLogsResponseSchema() *openapi3.Schema {
	return objectSchema("chunks", "finished").
		WithPropertyRef("chunks", arraySchemaRef("BuildLogChunk")).
		WithProperty("finished", boolSchema("True when the build has finished and no more chunks remain after this page."))
}

func buildVariableSchema() *openapi3.Schema {
	return objectSchema("key", "environmentCode", "value", "isSecret", "updatedBy", "updatedByUserId", "updatedAt").
		WithProperty("key", stringSchema("Variable key, matching ^[A-Z][A-Z0-9_]{0,63}$.")).
		WithProperty("environmentCode", stringSchema("Environment code. Empty means the variable applies to all environments.")).
		WithProperty("value", stringSchema("Plain value for normal variables; ****** for secrets.")).
		WithProperty("isSecret", boolSchema("Secrets are passed to docker build via BuildKit --secret and never returned.")).
		WithProperty("updatedBy", stringSchema("Display name of the last updater.")).
		WithProperty("updatedByUserId", stringSchema("User id of the last updater.")).
		WithProperty("updatedAt", dateTimeSchema("Last update time."))
}

func buildVariablesResponseSchema() *openapi3.Schema {
	return objectSchema("variables").
		WithPropertyRef("variables", arraySchemaRef("BuildVariable"))
}

func buildVariableDetailResponseSchema() *openapi3.Schema {
	return objectSchema("variable").
		WithPropertyRef("variable", schemaRef("BuildVariable"))
}

func buildVariableUpsertRequestSchema() *openapi3.Schema {
	return objectSchema("key", "value").
		WithProperty("key", stringSchema("Variable key, matching ^[A-Z][A-Z0-9_]{0,63}$. The FORGE_ prefix is reserved.")).
		WithProperty("environmentCode", stringSchema("Environment code. Empty means the variable applies to all environments.")).
		WithProperty("value", stringSchema("Plain value. Required and non-empty for secrets.")).
		WithProperty("isSecret", boolSchema("Store as a secret build variable."))
}

func buildAdapterIDParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewPathParameter("id").
				WithDescription("Build adapter id.").
				WithSchema(stringSchema("Build adapter id.")),
		},
	}
}

func buildAdapterParameterSchema() *openapi3.Schema {
	return objectSchema("key", "label", "description", "required", "default").
		WithProperty("key", stringSchema("Parameter key, matching ^[a-zA-Z][a-zA-Z0-9_]{0,63}$. Referenced in templates as .Params.<key>.")).
		WithProperty("label", stringSchema("Display label. Defaults to the key.")).
		WithProperty("description", stringSchema("Parameter description.")).
		WithProperty("required", boolSchema("Whether modules must provide a non-empty value (or the default is non-empty).")).
		WithProperty("default", stringSchema("Default value used when the module leaves the parameter empty."))
}

func buildAdapterSchema() *openapi3.Schema {
	return objectSchema(
		"id",
		"name",
		"description",
		"dockerfileTemplate",
		"files",
		"parameters",
		"version",
		"status",
		"moduleCount",
		"createdByUserId",
		"createdBy",
		"updatedByUserId",
		"updatedBy",
		"createdAt",
		"updatedAt",
	).
		WithProperty("id", stringSchema("Build adapter id.")).
		WithProperty("name", stringSchema("Build adapter display name.")).
		WithProperty("description", stringSchema("Build adapter description.")).
		WithProperty("dockerfileTemplate", stringSchema("Dockerfile template using Go text/template syntax.")).
		WithProperty("files", stringMapSchema("Helper file templates keyed by path relative to the .forge/ directory of the build context.")).
		WithPropertyRef("parameters", arraySchemaRef("BuildAdapterParameter")).
		WithProperty("version", int32Schema("Version, incremented by every update.")).
		WithProperty("status", enumStringSchema("Build adapter status. Disabled adapters cannot be selected and referencing modules cannot start new builds.", "active", "disabled")).
		WithProperty("moduleCount", int32Schema("Number of modules referencing this adapter.")).
		WithProperty("createdByUserId", stringSchema("Creator user id.")).
		WithProperty("createdBy", nullableSchemaRef("UserSummary", "Creator.")).
		WithProperty("updatedByUserId", stringSchema("Last updater user id.")).
		WithProperty("updatedBy", nullableSchemaRef("UserSummary", "Last updater.")).
		WithProperty("createdAt", dateTimeSchema("Creation time.")).
		WithProperty("updatedAt", dateTimeSchema("Last update time."))
}

func buildAdaptersResponseSchema() *openapi3.Schema {
	return objectSchema("buildAdapters").
		WithPropertyRef("buildAdapters", arraySchemaRef("BuildAdapter"))
}

func buildAdapterDetailResponseSchema() *openapi3.Schema {
	return objectSchema("buildAdapter").
		WithPropertyRef("buildAdapter", schemaRef("BuildAdapter"))
}

func buildAdapterRequestSchema(create bool) *openapi3.Schema {
	required := []string{}
	if create {
		required = []string{"name", "dockerfileTemplate"}
	}
	return objectSchema(required...).
		WithProperty("name", stringSchema("Display name, at most 100 characters.")).
		WithProperty("description", stringSchema("Description, at most 2000 characters.")).
		WithProperty("dockerfileTemplate", stringSchema("Dockerfile template, at most 64 KiB. Only default, quote, join, lower, upper, trim, replace and the and/or/not/eq/ne/lt/le/gt/ge builtins are allowed; define/template/block are rejected and range only accepts context fields or variables.")).
		WithProperty("files", stringMapSchema("Helper file templates, at most 20 files of 64 KiB each. Keys are clean relative paths without .. and are written under .forge/ of the build context. Replaces all files when provided.")).
		WithPropertyRef("parameters", arraySchemaRef("BuildAdapterParameter"))
}

func buildAdapterErrorResponseSchema() *openapi3.Schema {
	return objectSchema("error").
		WithProperty("error", stringSchema("Error message.")).
		WithProperty("template", stringSchema("Template that failed: Dockerfile or a helper file path.")).
		WithProperty("line", int32Schema("1-based template line of the error."))
}

func buildAdapterRenderPreviewRequestSchema() *openapi3.Schema {
	return objectSchema().
		WithProperty("moduleId", stringSchema("Module used for the render context. Omit to use a sample context.")).
		WithProperty("params", stringMapSchema("Parameter values. Omit to use the parameters saved on the module (when it references this adapter)."))
}

func buildAdapterRenderErrorSchema() *openapi3.Schema {
	return objectSchema("message", "template", "line").
		WithProperty("message", stringSchema("Error message.")).
		WithProperty("template", stringSchema("Template that failed: Dockerfile or a helper file path. Empty for parameter errors.")).
		WithProperty("line", int32Schema("1-based template line. 0 when unknown."))
}

func buildAdapterRenderPreviewResponseSchema() *openapi3.Schema {
	return objectSchema("ok", "dockerfile", "dockerfileSha256", "files", "error").
		WithProperty("ok", boolSchema("Whether rendering succeeded.")).
		WithProperty("dockerfile", stringSchema("Rendered Dockerfile.")).
		WithProperty("dockerfileSha256", stringSchema("SHA-256 of the rendered Dockerfile.")).
		WithProperty("files", stringMapSchema("Rendered helper files keyed by path relative to .forge/.")).
		WithProperty("error", nullableSchemaRef("BuildAdapterRenderError", "Render or parameter error. Null on success."))
}

func buildAdapterDraftRenderPreviewRequestSchema() *openapi3.Schema {
	return objectSchema("dockerfileTemplate").
		WithProperty("dockerfileTemplate", stringSchema("Draft Dockerfile template. Validated with the same rules as saving.")).
		WithProperty("files", stringMapSchema("Draft helper file templates keyed by path relative to .forge/.")).
		WithPropertyRef("parameters", arraySchemaRef("BuildAdapterParameter")).
		WithProperty("moduleId", stringSchema("Module used for the render context. Omit to use a sample context.")).
		WithProperty("params", stringMapSchema("Parameter values, validated strictly against the draft parameters. Omit to use the parameters saved on the module; keys not declared in the draft are ignored."))
}
