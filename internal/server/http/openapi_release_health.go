package httpserver

import (
	"net/http"

	"forge-server/internal/domain"

	"github.com/getkin/kin-openapi/openapi3"
)

func numberSchema(description string) *openapi3.Schema {
	schema := openapi3.NewFloat64Schema()
	schema.Description = description
	return schema
}

// addReleaseHealthOpenAPI 注册发布健康的组件和接口：客户端启动结果上报、健康策略、发布健康查询，规则见 docs/release-health.md。
func addReleaseHealthOpenAPI(doc *openapi3.T) {
	schemas := doc.Components.Schemas
	outcomes := []string{string(domain.LaunchOutcomeLaunched), string(domain.LaunchOutcomeLaunchFailed), string(domain.LaunchOutcomeVerifyFailed), string(domain.LaunchOutcomeDownloadFailed)}
	statuses := []string{string(domain.ReleaseHealthNoReports), string(domain.ReleaseHealthInsufficientData), string(domain.ReleaseHealthHealthy), string(domain.ReleaseHealthUnhealthy)}

	schemas["LaunchReportEvent"] = schemaValue(objectSchema("moduleId", "buildRecordId", "outcome").
		WithProperty("moduleId", stringSchema("Module id from the manifest.")).
		WithProperty("buildRecordId", stringSchema("buildRecordId of the module in the manifest.")).
		WithProperty("releaseId", stringSchema("releaseId of the module in the manifest; empty when the module came from a test plan. Health is judged per release, so events without it only count for statistics.")).
		WithProperty("outcome", enumStringSchema("launched: reached the module's ready point. launch_failed: crashed before the ready point and the client rolled back. verify_failed: sha256 or signature check failed. download_failed: download failed. Only launched and launch_failed count toward the failure rate, and they must be reported together.", outcomes...)).
		WithProperty("reason", stringSchema("Optional short reason code defined by the client, at most 64 bytes, for example js_exception. Do not send stack traces.")).
		WithProperty("occurredAt", stringSchema("RFC3339 time. Events older than 24 hours or more than 5 minutes in the future are dropped. Defaults to the server time.")))
	schemas["LaunchReportRequest"] = schemaValue(objectSchema("platform", "runtimeVersion", "events").
		WithProperty("platform", enumStringSchema("Client platform.", domain.BuildPlatforms...)).
		WithProperty("runtimeVersion", stringSchema("Client runtimeVersion.")).
		WithProperty("channel", stringSchema("Release channel the client reported when pulling the manifest; empty for the default channel.")).
		WithProperty("userId", stringSchema("Client user id. Users are deduplicated per 5-minute bucket; events without userId are counted but do not take part in health judgment.")).
		WithPropertyRef("events", arraySchemaRef("LaunchReportEvent")))
	schemas["LaunchReportResponse"] = schemaValue(objectSchema("accepted").
		WithProperty("accepted", int32Schema("Events counted. Malformed or expired events are skipped without failing the batch.")))

	schemas["ReleaseHealthPolicy"] = schemaValue(objectSchema("environment", "environmentName", "configured", "enabled", "windowMinutes", "minUsers", "maxFailureRate", "minDelta", "autoStopGray", "updatedAt").
		WithProperty("environment", stringSchema("Environment code.")).
		WithProperty("environmentName", stringSchema("Environment display name.")).
		WithProperty("configured", boolSchema("False when the policy was never saved and the values are defaults.")).
		WithProperty("enabled", boolSchema("Judge active releases of this application and environment every minute.")).
		WithProperty("windowMinutes", int32Schema("Judgment window, 5-1440 minutes, starting no earlier than the release.")).
		WithProperty("minUsers", int32Schema("Minimum launched plus launch_failed users in the window before judging.")).
		WithProperty("maxFailureRate", numberSchema("Failure rate threshold, greater than 0 and at most 1.")).
		WithProperty("minDelta", numberSchema("For percentage gray releases with a baseline full release, the failure rate must also exceed the baseline by this much, 0-1.")).
		WithProperty("autoStopGray", boolSchema("Deactivate a percentage gray release when it becomes unhealthy. Full releases are only flagged.")).
		WithProperty("updatedAt", stringSchema("Last update time; empty when never saved.")))
	schemas["ReleaseHealthPoliciesResponse"] = schemaValue(objectSchema("policies").
		WithPropertyRef("policies", arraySchemaRef("ReleaseHealthPolicy")))
	schemas["ReleaseHealthPolicyRequest"] = schemaValue(objectSchema("enabled", "windowMinutes", "minUsers", "maxFailureRate", "minDelta", "autoStopGray").
		WithProperty("enabled", boolSchema("Enable judgment.")).
		WithProperty("windowMinutes", int32Schema("5-1440.")).
		WithProperty("minUsers", int32Schema("1-1000000.")).
		WithProperty("maxFailureRate", numberSchema("Greater than 0 and at most 1.")).
		WithProperty("minDelta", numberSchema("0-1.")).
		WithProperty("autoStopGray", boolSchema("Deactivate unhealthy percentage gray releases.")))
	schemas["ReleaseHealthResponse"] = schemaValue(objectSchema("status", "policyEnabled", "policy", "windowStart", "launchedUsers", "failedUsers", "verifyFailedUsers", "downloadFailedUsers", "failureRate", "baselineFailureRate", "evaluatedAt").
		WithProperty("status", enumStringSchema("no_reports: no launched or launch_failed report since the release was created, so the release is not protected. insufficient_data: fewer users than minUsers in the window. healthy or unhealthy: judged result.", statuses...)).
		WithProperty("policyEnabled", boolSchema("False when the policy is off; the status is then computed with the policy's current values for reference only and no action is taken.")).
		WithPropertyRef("policy", schemaRef("ReleaseHealthPolicy")).
		WithProperty("windowStart", stringSchema("Start of the judgment window.")).
		WithProperty("launchedUsers", int64Schema("launched users in the window.")).
		WithProperty("failedUsers", int64Schema("launch_failed users in the window.")).
		WithProperty("verifyFailedUsers", int64Schema("verify_failed users in the window; may indicate tampered packages or a wrong embedded public key.")).
		WithProperty("downloadFailedUsers", int64Schema("download_failed users in the window.")).
		WithProperty("failureRate", numberSchema("failedUsers / (launchedUsers + failedUsers); 0 without samples.")).
		WithProperty("baselineFailureRate", numberSchema("Failure rate of the active full release used as baseline; null without a usable baseline.").WithNullable()).
		WithProperty("evaluatedAt", stringSchema("Computation time.")))

	common := func(items ...openapi3.NewResponsesOption) *openapi3.Responses {
		return responses(append(items,
			response(http.StatusBadRequest, "Invalid request.", schemaRef("ErrorResponse")),
			response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
			response(http.StatusNotFound, "Not found.", schemaRef("ErrorResponse")),
			response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
		)...)
	}

	report := operation("createLaunchReport", "Report module launch outcomes", []string{"Artifacts"}, common(
		response(http.StatusAccepted, "Events accepted for aggregation; counts are flushed every 30 seconds.", schemaRef("LaunchReportResponse")),
	))
	report.Parameters = openapi3.Parameters{
		{Value: openapi3.NewPathParameter("applicationSlug").WithDescription("Application slug.").WithSchema(stringSchema("Application slug."))},
		{Value: openapi3.NewPathParameter("environment").WithDescription("Environment code.").WithSchema(stringSchema("Environment code."))},
	}
	report.Security = artifactTokenSecurityRequirements()
	report.RequestBody = requestBody("Launch report, at most 50 events.", schemaRef("LaunchReportRequest"))
	doc.Paths.Set("/api/v1/artifacts/applications/{applicationSlug}/environments/{environment}/launch-reports", &openapi3.PathItem{Post: report})

	health := operation("getReleaseHealth", "Get release health", []string{"Releases"}, common(
		response(http.StatusOK, "Live health computed from launch reports, without side effects.", schemaRef("ReleaseHealthResponse")),
	))
	health.Parameters = openapi3.Parameters{{Value: openapi3.NewPathParameter("id").WithDescription("Release UUID.").WithRequired(true).WithSchema(stringSchema("Release UUID."))}}
	doc.Paths.Set("/api/v1/releases/{id}/health", &openapi3.PathItem{Get: health})

	list := operation("listReleaseHealthPolicies", "List release health policies", []string{"Applications"}, common(
		response(http.StatusOK, "One policy per environment bound to the application; unsaved environments return defaults.", schemaRef("ReleaseHealthPoliciesResponse")),
	))
	list.Parameters = applicationSlugParameters()
	doc.Paths.Set("/api/v1/applications/{slug}/release-health-policies", &openapi3.PathItem{Get: list})

	update := operation("updateReleaseHealthPolicy", "Update release health policy", []string{"Applications"}, common(
		response(http.StatusOK, "Policies of all environments after the update.", schemaRef("ReleaseHealthPoliciesResponse")),
		response(http.StatusForbidden, "Current user cannot update this application (application:update).", schemaRef("ErrorResponse")),
	))
	update.Parameters = append(applicationSlugParameters(), &openapi3.ParameterRef{Value: openapi3.NewPathParameter("environment").WithDescription("Environment code bound to the application.").WithRequired(true).WithSchema(stringSchema("Environment code."))})
	update.RequestBody = requestBody("Release health policy payload.", schemaRef("ReleaseHealthPolicyRequest"))
	doc.Paths.Set("/api/v1/applications/{slug}/release-health-policies/{environment}", &openapi3.PathItem{Put: update})
}
