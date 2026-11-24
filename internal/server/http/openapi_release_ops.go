package httpserver

import (
	"net/http"

	"forge-server/internal/domain"

	"github.com/getkin/kin-openapi/openapi3"
)

// addReleaseOperationsOpenAPI 注册发布运维相关的接口和组件：能力冲突、停用与回滚、发布操作记录、拉包统计。
func addReleaseOperationsOpenAPI(doc *openapi3.T) {
	schemas := doc.Components.Schemas

	schemas["CapabilityShortfall"] = schemaValue(objectSchema("name", "required", "provided").
		WithProperty("name", stringSchema("Capability name.")).
		WithProperty("required", int64Schema("Level required by the Feature build.")).
		WithProperty("provided", int64Schema("Level provided by the Common build; null when the Common build does not declare this capability.").WithNullable()))
	schemas["ReleaseCapabilityConflict"] = schemaValue(objectSchema("moduleId", "moduleName", "releaseId", "releaseType", "buildRecordId", "buildNumber", "reason", "shortfalls").
		WithProperty("moduleId", stringSchema("Module of the conflicting release. When releasing a Common build these are Feature modules; when releasing a Feature build this is its Common module.")).
		WithProperty("moduleName", stringSchema("Module name.")).
		WithProperty("releaseId", stringSchema("Conflicting active release; empty when reason is common_missing.")).
		WithProperty("releaseType", stringSchema("Release type of the conflicting release (full or gray); empty when reason is common_missing.")).
		WithProperty("buildRecordId", stringSchema("Build of the conflicting release; empty when reason is common_missing.")).
		WithProperty("buildNumber", int64Schema("Build number of the conflicting release.")).
		WithProperty("reason", enumStringSchema("unmet: the Common build provides lower levels than the Feature build requires. common_missing: the Feature build declares requires but its Common module has no active release for this environment and runtimeVersion.",
			domain.ReleaseCapabilityUnmet, domain.ReleaseCapabilityCommonMissing)).
		WithPropertyRef("shortfalls", arraySchemaRef("CapabilityShortfall")))
	schemas["ReleaseConflictErrorResponse"] = schemaValue(objectSchema("error").
		WithProperty("error", enumStringSchema("Conflict kind.", "release already active", "release build unsigned", "release capability conflict")).
		WithPropertyRef("conflicts", arraySchemaRef("ReleaseCapabilityConflict")))

	schemas["ReleaseDeactivateRequest"] = schemaValue(objectSchema().
		WithProperty("reason", stringSchema("Optional reason, at most 200 characters. Recorded in the release events.")))
	schemas["ReleaseRollbackRequest"] = schemaValue(objectSchema().
		WithProperty("description", stringSchema("Optional description of the new full release, at most 200 characters.")))
	schemas["UserRef"] = schemaValue(objectSchema("id", "name").
		WithProperty("id", stringSchema("Console user id.")).
		WithProperty("name", stringSchema("User name.")))
	eventDetail := openapi3.NewObjectSchema().WithAnyAdditionalProperties()
	eventDetail.Description = "Kind-specific detail. created: releaseType, buildRecordId, runtimeVersion, channel, grayPercentage (gray), rollbackFromReleaseId (rollback). " +
		"gray_percentage_updated: from, to. promoted_to_full: fromPercentage. replaced: byReleaseId. deactivated: reason, plus auto, failureRate, launchedUsers, failedUsers, maxFailureRate, minDelta, windowMinutes and baselineFailureRate when stopped by health judgment. " +
		"health_unhealthy: failureRate, launchedUsers, failedUsers, maxFailureRate, minDelta, windowMinutes, baselineFailureRate."
	schemas["ReleaseEvent"] = schemaValue(objectSchema("id", "kind", "detail", "actor", "createdAt").
		WithProperty("id", int64Schema("Event id.")).
		WithProperty("kind", enumStringSchema("Event kind.", string(domain.ReleaseEventCreated), string(domain.ReleaseEventGrayPercentageUpdated),
			string(domain.ReleaseEventPromotedToFull), string(domain.ReleaseEventReplaced), string(domain.ReleaseEventDeactivated), string(domain.ReleaseEventHealthUnhealthy))).
		WithProperty("detail", eventDetail).
		WithProperty("actor", nullableSchemaRef("UserRef", "Who did it; null for system actions or deleted users.")).
		WithProperty("createdAt", stringSchema("RFC3339 time.")))
	schemas["ReleaseEventsResponse"] = schemaValue(objectSchema("events").
		WithPropertyRef("events", arraySchemaRef("ReleaseEvent")))
	schemas["ArtifactPullStat"] = schemaValue(objectSchema("day", "environment", "moduleId", "moduleName", "runtimeVersion", "platform", "channel", "outcome", "buildRecordId", "buildNumber", "requests").
		WithProperty("day", stringSchema("Date (YYYY-MM-DD) in the server's local time zone.")).
		WithProperty("environment", stringSchema("Environment code.")).
		WithProperty("moduleId", stringSchema("Module id.")).
		WithProperty("moduleName", stringSchema("Module name; empty when the module was deleted.")).
		WithProperty("runtimeVersion", stringSchema("Client runtimeVersion.")).
		WithProperty("platform", stringSchema("Client platform.")).
		WithProperty("channel", stringSchema("Channel used for resolution; empty for the default channel and for unregistered channels.")).
		WithProperty("outcome", stringSchema("testPlan, percentage or full when the module got a build; missing:{reason} when it did not, for example missing:no_release_for_runtime.")).
		WithProperty("buildRecordId", stringSchema("Build served; empty for missing outcomes.")).
		WithProperty("buildNumber", int64Schema("Build number; 0 for missing outcomes or deleted builds.")).
		WithProperty("requests", int64Schema("Manifest requests (including 304) that resolved this way on that day.")))
	schemas["ArtifactPullStatsResponse"] = schemaValue(objectSchema("from", "to", "stats").
		WithProperty("from", stringSchema("First day included (YYYY-MM-DD).")).
		WithProperty("to", stringSchema("Last day included (YYYY-MM-DD), today.")).
		WithPropertyRef("stats", arraySchemaRef("ArtifactPullStat")))

	releaseID := &openapi3.ParameterRef{Value: openapi3.NewPathParameter("id").WithDescription("Release UUID.").WithRequired(true).WithSchema(stringSchema("Release UUID."))}
	common := func(items ...openapi3.NewResponsesOption) *openapi3.Responses {
		return responses(append(items,
			response(http.StatusUnauthorized, "Console session is required.", schemaRef("ErrorResponse")),
			response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
		)...)
	}
	mutation := func(id string, summary string, okStatus int, okDescription string, conflict string, body string) *openapi3.Operation {
		op := operation(id, summary, []string{"Releases"}, common(
			response(okStatus, okDescription, schemaRef("ReleaseDetailResponse")),
			response(http.StatusBadRequest, "Invalid release request.", schemaRef("ErrorResponse")),
			response(http.StatusForbidden, "Current user cannot roll back or deactivate releases of this application (release:rollback).", schemaRef("ErrorResponse")),
			response(http.StatusNotFound, "Release not found.", schemaRef("ErrorResponse")),
			response(http.StatusConflict, conflict, schemaRef("ReleaseConflictErrorResponse")),
		))
		op.Parameters = openapi3.Parameters{releaseID}
		op.RequestBody = requestBody(summary+" payload.", schemaRef(body))
		op.RequestBody.Value.Required = false
		return op
	}
	doc.Paths.Set("/api/v1/releases/{id}/deactivate", &openapi3.PathItem{Post: mutation("deactivateRelease", "Deactivate release", http.StatusOK,
		"Release and its route deactivated. Nothing else is activated: after deactivating a full release, clients of that runtimeVersion that miss the gray bucket get no build and fall back to the bundled package.",
		"error is \"release not active\".", "ReleaseDeactivateRequest")})
	doc.Paths.Set("/api/v1/releases/{id}/rollback", &openapi3.PathItem{Post: mutation("rollbackRelease", "Roll back full release", http.StatusCreated,
		"New full release of the build of the previous full release (same module, environment and runtimeVersion, different build). It ends the active percentage gray release and passes the same capability checks as createRelease.",
		"error is \"release not active\", \"release rollback unavailable\" (not a full release, or no earlier full release), or \"release capability conflict\" with conflicts.", "ReleaseRollbackRequest")})
	doc.Paths.Set("/api/v1/releases/{id}/events", &openapi3.PathItem{Get: func() *openapi3.Operation {
		op := operation("listReleaseEvents", "List release events", []string{"Releases"}, common(
			response(http.StatusOK, "Latest events first, at most 100.", schemaRef("ReleaseEventsResponse")),
			response(http.StatusBadRequest, "Release id is not a UUID.", schemaRef("ErrorResponse")),
			response(http.StatusNotFound, "Release not found.", schemaRef("ErrorResponse")),
		))
		op.Parameters = openapi3.Parameters{releaseID}
		return op
	}()})
	doc.Paths.Set("/api/v1/artifact-pull-stats", &openapi3.PathItem{Get: func() *openapi3.Operation {
		op := operation("listArtifactPullStats", "List daily client pull statistics", []string{"Artifacts"}, common(
			response(http.StatusOK, "Daily counts of manifest requests per module outcome, latest day first, at most 5000 rows. Counts are flushed every 30 seconds.", schemaRef("ArtifactPullStatsResponse")),
			response(http.StatusBadRequest, "Invalid filters.", schemaRef("ErrorResponse")),
		))
		op.Parameters = openapi3.Parameters{
			{Value: openapi3.NewQueryParameter("applicationId").WithDescription("Filter by application id. At least one of applicationId, moduleId and buildRecordId is required.").WithSchema(stringSchema("Application id."))},
			{Value: openapi3.NewQueryParameter("environment").WithDescription("Filter by environment code.").WithSchema(stringSchema("Environment code."))},
			{Value: openapi3.NewQueryParameter("moduleId").WithDescription("Filter by module id.").WithSchema(stringSchema("Module id."))},
			{Value: openapi3.NewQueryParameter("buildRecordId").WithDescription("Filter by build UUID.").WithSchema(stringSchema("Build UUID."))},
			{Value: openapi3.NewQueryParameter("days").WithDescription("Days including today, 1-90, default 7. Data older than 90 days is deleted.").WithSchema(int32Schema("Days."))},
		}
		return op
	}()})
}
