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
				Name:        "Modules",
				Description: "RN module repository binding management.",
			},
			{
				Name:        "Builds",
				Description: "RN JS bundle build record management.",
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
				Description: "Code source provider installation and repository discovery.",
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
					op := operation("deleteApplication", "Delete application", []string{"Applications"}, responses(
						responseNoBody(http.StatusNoContent, "Application was deleted."),
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
						response(http.StatusNotFound, "Module not found.", schemaRef("ErrorResponse")),
						response(http.StatusConflict, "Module is referenced.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = moduleIDParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/builds", &openapi3.PathItem{
				Get: operation("listBuildRecords", "List RN JS bundle build records", []string{"Builds"}, responses(
					response(http.StatusOK, "Build record list.", schemaRef("BuildRecordsResponse")),
					response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
				)),
				Post: func() *openapi3.Operation {
					op := operation("createBuildRecord", "Create RN JS bundle build record", []string{"Builds"}, responses(
						response(http.StatusCreated, "Build record created.", schemaRef("BuildRecordDetailResponse")),
						response(http.StatusBadRequest, "Invalid build request.", schemaRef("ErrorResponse")),
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
						response(http.StatusBadRequest, "Invalid artifact manifest request.", schemaRef("ErrorResponse")),
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
				Get: operation("listCodeSources", "List code source apps", []string{"CodeSources"}, responses(
					response(http.StatusOK, "Code source app list.", schemaRef("CodeSourcesResponse")),
					response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
					response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
				)),
			}),
			openapi3.WithPath("/api/v1/code-sources/{provider}/manifest", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("getCodeSourceAppManifest", "Get code source app manifest creation payload", []string{"CodeSources"}, responses(
						response(http.StatusOK, "Code source app manifest creation payload.", schemaRef("CodeSourceAppManifest")),
						response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Code source provider not found.", schemaRef("ErrorResponse")),
						response(http.StatusServiceUnavailable, "Code source auth is not configured.", schemaRef("ErrorResponse")),
						response(http.StatusBadGateway, "Code source provider request failed.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = codeSourceProviderParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/code-sources/{provider}/manifest/callback", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("completeCodeSourceAppManifest", "Complete code source app manifest callback", []string{"CodeSources"}, responses(
						response(http.StatusFound, "Redirects to GitHub App installation URL.", nil),
						response(http.StatusBadRequest, "Invalid code source manifest callback.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Code source provider not found.", schemaRef("ErrorResponse")),
						response(http.StatusServiceUnavailable, "Code source auth is not configured.", schemaRef("ErrorResponse")),
						response(http.StatusBadGateway, "Code source provider request failed.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = codeSourceManifestCallbackParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/code-sources/{provider}/install-url", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("getCodeSourceInstallURL", "Get code source app installation URL", []string{"CodeSources"}, responses(
						response(http.StatusOK, "Code source installation URL.", schemaRef("CodeSourceInstallURL")),
						response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Code source provider not found.", schemaRef("ErrorResponse")),
						response(http.StatusServiceUnavailable, "Code source auth is not configured.", schemaRef("ErrorResponse")),
						response(http.StatusBadGateway, "Code source provider request failed.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = codeSourceProviderParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/code-sources/{provider}/install/callback", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("completeCodeSourceInstallation", "Complete code source app installation callback", []string{"CodeSources"}, responses(
						response(http.StatusFound, "Code source app installation completed and redirects to console.", nil),
						response(http.StatusBadRequest, "Invalid code source callback.", schemaRef("ErrorResponse")),
						response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Code source provider or installation not found.", schemaRef("ErrorResponse")),
						response(http.StatusServiceUnavailable, "Code source auth is not configured.", schemaRef("ErrorResponse")),
						response(http.StatusBadGateway, "Code source provider request failed.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = codeSourceInstallCallbackParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/code-sources/{provider}/installations", &openapi3.PathItem{
				Delete: func() *openapi3.Operation {
					op := operation("deleteCodeSourceInstallations", "Delete current user's code source app authorizations and repository cache", []string{"CodeSources"}, responses(
						responseNoBody(http.StatusNoContent, "Current user's code source app authorizations and repository cache were deleted."),
						response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Code source provider or installation not found.", schemaRef("ErrorResponse")),
						response(http.StatusServiceUnavailable, "Code source auth is not configured.", schemaRef("ErrorResponse")),
						response(http.StatusBadGateway, "Code source provider request failed.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = codeSourceProviderParameters()
					return op
				}(),
			}),
			openapi3.WithPath("/api/v1/code-sources/{provider}/repositories/search", &openapi3.PathItem{
				Get: func() *openapi3.Operation {
					op := operation("searchCodeSourceRepositories", "Search authorized code source repositories", []string{"CodeSources"}, responses(
						response(http.StatusOK, "Authorized repository search result.", schemaRef("CodeSourceRepositorySearchResponse")),
						response(http.StatusBadRequest, "Invalid repository search request.", schemaRef("ErrorResponse")),
						response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
						response(http.StatusNotFound, "Code source provider or installation not found.", schemaRef("ErrorResponse")),
						response(http.StatusServiceUnavailable, "Code source auth is not configured.", schemaRef("ErrorResponse")),
						response(http.StatusBadGateway, "Code source provider request failed.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = codeSourceRepositorySearchParameters()
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
					response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
				)),
			}),
			openapi3.WithPath("/api/v1/artifact-access-tokens/{id}", &openapi3.PathItem{
				Patch: func() *openapi3.Operation {
					op := operation("updateArtifactAccessToken", "Update artifact access token", []string{"AccessTokens"}, responses(
						response(http.StatusOK, "Artifact access token updated.", schemaRef("ArtifactAccessTokenDetailResponse")),
						response(http.StatusBadRequest, "Invalid artifact access token request.", schemaRef("ErrorResponse")),
						response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
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
						response(http.StatusNotFound, "Artifact access token not found.", schemaRef("ErrorResponse")),
						response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
					))
					op.Parameters = artifactAccessTokenIDParameters()
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
						response(http.StatusBadGateway, "Storage backend could not be reached.", schemaRef("ErrorResponse")),
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
				"ErrorResponse":                       schemaValue(errorResponseSchema()),
				"HealthResponse":                      schemaValue(healthResponseSchema()),
				"ReadinessResponse":                   schemaValue(readinessResponseSchema()),
				"PingResponse":                        schemaValue(pingResponseSchema()),
				"EnumOption":                          schemaValue(enumOptionSchema()),
				"EnumsResponse":                       schemaValue(enumsResponseSchema()),
				"Environment":                         schemaValue(environmentSchema()),
				"EnvironmentCreateRequest":            schemaValue(environmentCreateRequestSchema()),
				"EnvironmentUpdateRequest":            schemaValue(environmentCreateRequestSchema()),
				"EnvironmentsResponse":                schemaValue(environmentsResponseSchema()),
				"EnvironmentDetailResponse":           schemaValue(environmentDetailResponseSchema()),
				"Application":                         schemaValue(applicationSchema()),
				"ApplicationOption":                   schemaValue(applicationOptionSchema()),
				"ApplicationOptions":                  schemaValue(applicationOptionsSchema()),
				"ApplicationSummary":                  schemaValue(applicationSummarySchema()),
				"ApplicationMetric":                   schemaValue(applicationMetricSchema()),
				"ApplicationEnvironment":              schemaValue(applicationEnvironmentSchema()),
				"ApplicationModule":                   schemaValue(applicationModuleSchema()),
				"ApplicationMember":                   schemaValue(applicationMemberSchema()),
				"ApplicationPipeline":                 schemaValue(applicationPipelineSchema()),
				"ApplicationDetail":                   schemaValue(applicationDetailSchema()),
				"ApplicationsResponse":                schemaValue(applicationsResponseSchema()),
				"ApplicationDetailResponse":           schemaValue(applicationDetailResponseSchema()),
				"ApplicationCreateRequest":            schemaValue(applicationRequestSchema(true)),
				"ApplicationUpdateRequest":            schemaValue(applicationRequestSchema(false)),
				"ApplicationStatusUpdateRequest":      schemaValue(applicationStatusUpdateRequestSchema()),
				"ApplicationEnvironmentRequest":       schemaValue(applicationEnvironmentRequestSchema()),
				"ApplicationMemberRequest":            schemaValue(applicationMemberRequestSchema()),
				"ApplicationDeleteCheck":              schemaValue(applicationDeleteCheckSchema()),
				"ApplicationDeleteDependency":         schemaValue(applicationDeleteDependencySchema()),
				"Module":                              schemaValue(moduleSchema()),
				"ModuleSummary":                       schemaValue(moduleSummarySchema()),
				"ModulesResponse":                     schemaValue(modulesResponseSchema()),
				"ModuleDetailResponse":                schemaValue(moduleDetailResponseSchema()),
				"ModuleRepositoryRequest":             schemaValue(moduleRepositoryRequestSchema()),
				"ModuleCreateRequest":                 schemaValue(moduleRequestSchema(true)),
				"ModuleUpdateRequest":                 schemaValue(moduleRequestSchema(false)),
				"BuildRecord":                         schemaValue(buildRecordSchema()),
				"BuildArtifactTree":                   schemaValue(buildArtifactTreeSchema()),
				"BuildArtifactTreeNode":               schemaValue(buildArtifactTreeNodeSchema()),
				"BuildRecordCreateRequest":            schemaValue(buildRecordCreateRequestSchema()),
				"BuildRecordDetailResponse":           schemaValue(buildRecordDetailResponseSchema()),
				"BuildRecordSummary":                  schemaValue(buildRecordSummarySchema()),
				"BuildRecordsResponse":                schemaValue(buildRecordsResponseSchema()),
				"ArtifactManifestResponse":            schemaValue(artifactManifestResponseSchema()),
				"ArtifactManifestApplication":         schemaValue(artifactManifestApplicationSchema()),
				"ArtifactManifestEnvironment":         schemaValue(artifactManifestEnvironmentSchema()),
				"ArtifactManifestModule":              schemaValue(artifactManifestModuleSchema()),
				"ArtifactManifestFile":                schemaValue(artifactManifestFileSchema()),
				"ArtifactMissingModule":               schemaValue(artifactMissingModuleSchema()),
				"ReleaseRecord":                       schemaValue(releaseRecordSchema()),
				"ReleaseRoute":                        schemaValue(releaseRouteSchema()),
				"ReleaseCreateRequest":                schemaValue(releaseCreateRequestSchema()),
				"ReleaseGrayPercentageUpdateRequest":  schemaValue(releaseGrayPercentageUpdateRequestSchema()),
				"ReleaseDetailResponse":               schemaValue(releaseDetailResponseSchema()),
				"ReleaseResolveResponse":              schemaValue(releaseResolveResponseSchema()),
				"ReleasesResponse":                    schemaValue(releasesResponseSchema()),
				"CodeSourceApp":                       schemaValue(codeSourceAppSchema()),
				"CodeSourceAppDetailResponse":         schemaValue(codeSourceAppDetailResponseSchema()),
				"CodeSourceAppManifest":               schemaValue(codeSourceAppManifestSchema()),
				"CodeSourceInstallURL":                schemaValue(codeSourceInstallURLSchema()),
				"CodeSourceRepository":                schemaValue(codeSourceRepositorySchema()),
				"CodeSourceRepositorySearchResponse":  schemaValue(codeSourceRepositorySearchResponseSchema()),
				"CodeSourcesResponse":                 schemaValue(codeSourcesResponseSchema()),
				"ArtifactAccessToken":                 schemaValue(artifactAccessTokenSchema()),
				"ArtifactAccessTokensResponse":        schemaValue(artifactAccessTokensResponseSchema()),
				"ArtifactAccessTokenDetailResponse":   schemaValue(artifactAccessTokenDetailResponseSchema()),
				"ArtifactAccessTokenGenerateResponse": schemaValue(artifactAccessTokenGenerateResponseSchema()),
				"ArtifactAccessTokenCreateRequest":    schemaValue(artifactAccessTokenCreateRequestSchema()),
				"ArtifactAccessTokenUpdateRequest":    schemaValue(artifactAccessTokenUpdateRequestSchema()),
				"StorageConfiguration":                schemaValue(storageConfigurationSchema()),
				"StorageConfigurationsResponse":       schemaValue(storageConfigurationsResponseSchema()),
				"StorageConfigurationDetailResponse":  schemaValue(storageConfigurationDetailResponseSchema()),
				"StorageConfigurationCreateRequest":   schemaValue(storageConfigurationRequestSchema()),
				"StorageConfigurationUpdateRequest":   schemaValue(storageConfigurationRequestSchema()),
				"StorageConfigurationTestRequest":     schemaValue(storageConfigurationTestRequestSchema()),
				"StorageConfigurationTestResponse":    schemaValue(storageConfigurationTestResponseSchema()),
				"FeishuAuthResponse":                  schemaValue(feishuAuthResponseSchema()),
				"FeishuToken":                         schemaValue(feishuTokenSchema()),
				"FeishuUser":                          schemaValue(feishuUserSchema()),
				"FeishuUserSearchItem":                schemaValue(feishuUserSearchItemSchema()),
				"FeishuUserSearchResponse":            schemaValue(feishuUserSearchResponseSchema()),
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
			Value: openapi3.NewQueryParameter("path").
				WithDescription("Artifact file relative path from the build artifact manifest.").
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

func codeSourceProviderParameters() openapi3.Parameters {
	return openapi3.Parameters{
		{
			Value: openapi3.NewPathParameter("provider").
				WithDescription("Code source provider.").
				WithSchema(enumStringSchema("Code source provider.", "github")),
		},
	}
}

func codeSourceManifestCallbackParameters() openapi3.Parameters {
	parameters := codeSourceProviderParameters()
	return append(parameters,
		&openapi3.ParameterRef{
			Value: openapi3.NewQueryParameter("code").
				WithDescription("Temporary GitHub App manifest code returned by GitHub.").
				WithSchema(stringSchema("GitHub App manifest code.")),
		},
	)
}

func codeSourceInstallCallbackParameters() openapi3.Parameters {
	parameters := codeSourceProviderParameters()
	return append(parameters,
		&openapi3.ParameterRef{
			Value: openapi3.NewQueryParameter("installation_id").
				WithDescription("GitHub App installation id returned by setup URL.").
				WithSchema(int64Schema("GitHub App installation id.")),
		},
		&openapi3.ParameterRef{
			Value: openapi3.NewQueryParameter("setup_action").
				WithDescription("GitHub setup action, such as install or update.").
				WithSchema(enumStringSchema("GitHub setup action.", "install", "update")),
		},
		&openapi3.ParameterRef{
			Value: openapi3.NewQueryParameter("state").
				WithDescription("Opaque setup state returned by GitHub when present.").
				WithSchema(stringSchema("Setup state.")),
		},
	)
}

func codeSourceRepositorySearchParameters() openapi3.Parameters {
	parameters := codeSourceProviderParameters()
	return append(parameters,
		&openapi3.ParameterRef{
			Value: openapi3.NewQueryParameter("query").
				WithDescription("Repository search keyword. Matches owner, name, full name or description.").
				WithSchema(stringSchema("Repository search keyword.")),
		},
		&openapi3.ParameterRef{
			Value: openapi3.NewQueryParameter("pageSize").
				WithDescription("Result size, min 1, max 100, default 20.").
				WithSchema(int32Schema("Page size.")),
		},
	)
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
				WithDescription("Owner user id filter.").
				WithSchema(stringSchema("Owner user id filter.")),
		},
		{
			Value: openapi3.NewQueryParameter("keyword").
				WithDescription("Fuzzy application name keyword.").
				WithSchema(stringSchema("Application name keyword.")),
		},
	}
}

func moduleListParameters() openapi3.Parameters {
	return openapi3.Parameters{
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
}

func releaseListParameters() openapi3.Parameters {
	return openapi3.Parameters{
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
	return objectSchema("name", "role", "team").
		WithProperty("name", stringSchema("Member account or name.")).
		WithProperty("role", enumStringSchema("Member role.", "Owner", "Developer", "QA", "Release")).
		WithProperty("team", stringSchema("Member team."))
}

func modulesResponseSchema() *openapi3.Schema {
	return objectSchema("modules", "summary").
		WithPropertyRef("modules", arraySchemaRef("Module")).
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
		"repositoryProvider",
		"repositoryId",
		"repositoryFullName",
		"repository",
	).
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
		WithProperty("repositoryHtmlUrl", stringSchema("Repository HTML URL.")).
		WithProperty("repositoryId", stringSchema("Provider repository id.")).
		WithProperty("repositoryInstallationId", stringSchema("Provider installation id that currently grants repository access.")).
		WithProperty("repositoryProvider", enumStringSchema("Repository provider.", "github")).
		WithProperty("repositorySshUrl", stringSchema("Repository SSH clone URL.")).
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
		WithPropertyRef("repository", schemaRef("ModuleRepositoryRequest"))
	if includeID {
		schema.WithProperty("id", stringSchema("Immutable module id."))
	}
	return schema
}

func moduleRepositoryRequestSchema() *openapi3.Schema {
	return objectSchema("provider", "id", "installationId", "fullName").
		WithProperty("cloneUrl", stringSchema("Repository HTTPS clone URL.")).
		WithProperty("defaultBranch", stringSchema("Repository default branch.")).
		WithProperty("fullName", stringSchema("Repository full name.")).
		WithProperty("htmlUrl", stringSchema("Repository HTML URL.")).
		WithProperty("id", stringSchema("Provider repository id.")).
		WithProperty("installationId", stringSchema("Provider installation id that currently grants repository access.")).
		WithProperty("provider", enumStringSchema("Code source provider.", "github")).
		WithProperty("sshUrl", stringSchema("Repository SSH clone URL."))
}

func buildRecordCreateRequestSchema() *openapi3.Schema {
	return objectSchema("moduleId", "environment").
		WithProperty("branch", stringSchema("Git branch to build. Defaults to the module default branch when omitted.")).
		WithProperty("moduleId", stringSchema("Module id to build.")).
		WithProperty("environment", stringSchema("Environment code whose storage configuration receives artifacts.")).
		WithProperty("triggerId", stringSchema("Trigger source id. Required when triggerType is pipeline.")).
		WithProperty("triggerType", enumStringSchema("Build trigger type. Defaults to manual when omitted.", "manual", "pipeline"))
}

func buildRecordDetailResponseSchema() *openapi3.Schema {
	return objectSchema("build").
		WithPropertyRef("build", schemaRef("BuildRecord"))
}

func buildRecordsResponseSchema() *openapi3.Schema {
	return objectSchema("builds", "summary").
		WithPropertyRef("builds", arraySchemaRef("BuildRecord")).
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
		"repositoryInstallationId",
		"repositoryFullName",
		"storageConfigurationId",
		"storageConfigurationName",
		"queuedAt",
		"createdAt",
		"updatedAt",
	).
		WithProperty("applicationName", stringSchema("Application display name used in artifact object keys.")).
		WithPropertyRef("artifactTree", schemaRef("BuildArtifactTree")).
		WithProperty("artifactKey", stringSchema("S3 object key root produced by the worker.")).
		WithProperty("artifactUrl", stringSchema("Public artifact base URL when storage configuration provides one.")).
		WithProperty("branch", stringSchema("Git branch selected for this build record.")).
		WithProperty("createdAt", dateTimeSchema("Build record creation time.")).
		WithProperty("defaultBranch", stringSchema("Repository branch used by the worker when configured on the module.")).
		WithProperty("environment", stringSchema("Environment code.")).
		WithProperty("environmentName", stringSchema("Environment display name used in artifact object keys.")).
		WithProperty("errorMessage", stringSchema("Build failure message.")).
		WithProperty("finishedAt", dateTimeSchema("Build finish time.")).
		WithProperty("id", stringSchema("Build record UUID.")).
		WithProperty("manifestKey", stringSchema("S3 object key of the artifact manifest.")).
		WithProperty("fileCount", int32Schema("Uploaded artifact file count.")).
		WithProperty("totalSizeBytes", int64Schema("Total uploaded artifact file size in bytes.")).
		WithProperty("moduleId", stringSchema("Module id.")).
		WithProperty("moduleName", stringSchema("Module display name.")).
		WithProperty("queuedAt", dateTimeSchema("Build queue time.")).
		WithProperty("repositoryFullName", stringSchema("Repository full name.")).
		WithProperty("repositoryId", stringSchema("Provider repository id.")).
		WithProperty("repositoryInstallationId", stringSchema("Provider installation id.")).
		WithProperty("repositoryProvider", enumStringSchema("Repository provider.", "github")).
		WithProperty("startedAt", dateTimeSchema("Build start time.")).
		WithProperty("status", enumStringSchema("Build status.", "pending", "running", "succeeded", "failed")).
		WithProperty("storageConfigurationId", stringSchema("Storage configuration id bound to the environment at queue time.")).
		WithProperty("storageConfigurationName", stringSchema("Storage configuration display name.")).
		WithProperty("triggerId", stringSchema("Trigger source id. Empty for manual builds.")).
		WithProperty("triggerType", enumStringSchema("Build trigger type.", "manual", "pipeline")).
		WithProperty("updatedAt", dateTimeSchema("Build record update time.")).
		WithProperty("workerId", stringSchema("Worker id that claimed the build."))
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
	return objectSchema("application", "environment", "identifier", "modules", "missingModules").
		WithPropertyRef("application", schemaRef("ArtifactManifestApplication")).
		WithPropertyRef("environment", schemaRef("ArtifactManifestEnvironment")).
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
	return objectSchema("id", "name", "kind", "releaseId", "releaseType", "buildRecordId", "artifactKey", "manifestKey", "files").
		WithProperty("id", stringSchema("Module id.")).
		WithProperty("name", stringSchema("Module display name.")).
		WithProperty("kind", enumStringSchema("Module kind.", "Common", "Feature")).
		WithProperty("releaseId", stringSchema("Matched release UUID.")).
		WithProperty("releaseType", enumStringSchema("Matched release type.", "full", "gray")).
		WithProperty("grayStrategy", enumStringSchema("Matched gray strategy.", "", "targeted", "percentage")).
		WithProperty("grayPercentage", int32Schema("Matched percentage gray value.")).
		WithProperty("identifier", stringSchema("Matched targeting identifier for targeted gray releases.")).
		WithProperty("buildRecordId", stringSchema("Matched build record UUID.")).
		WithProperty("artifactKey", stringSchema("Artifact object key root.")).
		WithProperty("manifestKey", stringSchema("Artifact manifest object key.")).
		WithPropertyRef("files", arraySchemaRef("ArtifactManifestFile"))
}

func artifactManifestFileSchema() *openapi3.Schema {
	return objectSchema("path", "url", "contentType", "sizeBytes", "sha256").
		WithProperty("path", stringSchema("Artifact file relative path.")).
		WithProperty("url", stringSchema("Download URL. Uses public base URL when configured, otherwise the artifact download API.")).
		WithProperty("contentType", stringSchema("File content type.")).
		WithProperty("sizeBytes", int64Schema("File size in bytes.")).
		WithProperty("sha256", stringSchema("File SHA-256 digest."))
}

func artifactMissingModuleSchema() *openapi3.Schema {
	return objectSchema("id", "name", "kind", "reason").
		WithProperty("id", stringSchema("Module id.")).
		WithProperty("name", stringSchema("Module display name.")).
		WithProperty("kind", enumStringSchema("Module kind.", "Common", "Feature")).
		WithProperty("reason", stringSchema("Why this module is absent from the manifest modules list."))
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
	return objectSchema("releases").
		WithPropertyRef("releases", arraySchemaRef("ReleaseRecord"))
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
		WithProperty("grayStrategy", enumStringSchema("Gray strategy.", "", "targeted", "percentage")).
		WithProperty("grayPercentage", int32Schema("Percentage gray value.")).
		WithProperty("status", enumStringSchema("Release route status.", "active", "inactive")).
		WithProperty("updatedAt", dateTimeSchema("Route update time."))
}

func codeSourcesResponseSchema() *openapi3.Schema {
	return objectSchema("apps").
		WithPropertyRef("apps", arraySchemaRef("CodeSourceApp"))
}

func codeSourceAppDetailResponseSchema() *openapi3.Schema {
	return objectSchema("app").
		WithPropertyRef("app", schemaRef("CodeSourceApp"))
}

func codeSourceAppSchema() *openapi3.Schema {
	return objectSchema("configurationStatus", "description", "id", "name", "provider", "status").
		WithProperty("configurationStatus", enumStringSchema("Code source app configuration status.", "configured", "notConfigured")).
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
	return objectSchema("repositories").
		WithPropertyRef("repositories", arraySchemaRef("CodeSourceRepository"))
}

func codeSourceRepositorySchema() *openapi3.Schema {
	return objectSchema("id", "installationId", "provider", "owner", "name", "fullName", "private").
		WithProperty("cloneUrl", stringSchema("HTTPS clone URL.")).
		WithProperty("defaultBranch", stringSchema("Default branch name.")).
		WithProperty("description", stringSchema("Repository description.")).
		WithProperty("fullName", stringSchema("Repository full name, owner/name.")).
		WithProperty("htmlUrl", stringSchema("Repository HTML URL.")).
		WithProperty("id", stringSchema("Provider repository id.")).
		WithProperty("installationId", stringSchema("Provider installation id that currently grants repository access.")).
		WithProperty("name", stringSchema("Repository name.")).
		WithProperty("owner", stringSchema("Repository owner.")).
		WithProperty("private", boolSchema("Whether repository is private.")).
		WithProperty("provider", enumStringSchema("Code source provider.", "github")).
		WithProperty("sshUrl", stringSchema("SSH clone URL.")).
		WithProperty("updatedAt", dateTimeSchema("Repository update time."))
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
	return objectSchema("configurations").
		WithPropertyRef("configurations", arraySchemaRef("StorageConfiguration"))
}

func storageConfigurationDetailResponseSchema() *openapi3.Schema {
	return objectSchema("configuration").
		WithPropertyRef("configuration", schemaRef("StorageConfiguration"))
}

func storageConfigurationRequestSchema() *openapi3.Schema {
	required := []string{"name", "adapter", "bucket", "endpoint", "region", "ownerUserId", "owner", "accessKeyId", "secretAccessKey"}

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
		WithProperty("accessKeyId", stringSchema("Access key id. Not returned by read APIs.")).
		WithProperty("secretAccessKey", stringSchema("Secret access key. Not returned by read APIs."))
}

func storageConfigurationTestRequestSchema() *openapi3.Schema {
	return objectSchema("name", "adapter", "bucket", "endpoint", "region", "ownerUserId", "owner", "accessKeyId", "secretAccessKey").
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
		WithProperty("accessKeyId", stringSchema("Access key id.")).
		WithProperty("secretAccessKey", stringSchema("Secret access key."))
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

func arraySchemaRef(itemSchema string) *openapi3.SchemaRef {
	schema := openapi3.NewArraySchema()
	schema.Items = schemaRef(itemSchema)
	return schemaValue(schema)
}
