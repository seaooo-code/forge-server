package httpserver

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
)

// addReleaseChannelsOpenAPI 注册应用发布渠道的管理接口，规则见 docs/channels.md。
func addReleaseChannelsOpenAPI(doc *openapi3.T) {
	schemas := doc.Components.Schemas

	schemas["ReleaseChannel"] = schemaValue(objectSchema("code", "name", "description", "activeReleases", "createdAt", "updatedAt").
		WithProperty("code", stringSchema("Channel code reported by clients. Immutable after creation.")).
		WithProperty("name", stringSchema("Display name.")).
		WithProperty("description", stringSchema("Description.")).
		WithProperty("activeReleases", int32Schema("Active releases of this channel across all environments. The channel can only be deleted when this is 0.")).
		WithProperty("createdAt", dateTimeSchema("Creation time.")).
		WithProperty("updatedAt", dateTimeSchema("Update time.")))
	schemas["ReleaseChannelsResponse"] = schemaValue(objectSchema("channels").
		WithPropertyRef("channels", arraySchemaRef("ReleaseChannel")))
	schemas["ReleaseChannelCreateRequest"] = schemaValue(objectSchema("code", "name").
		WithProperty("code", stringSchema("Channel code: a lowercase letter, then lowercase letters, digits and hyphens, at most 32 characters.")).
		WithProperty("name", stringSchema("Display name, at most 50 characters.")).
		WithProperty("description", stringSchema("Optional description, at most 200 characters.")))
	schemas["ReleaseChannelUpdateRequest"] = schemaValue(objectSchema("name").
		WithProperty("name", stringSchema("Display name, at most 50 characters.")).
		WithProperty("description", stringSchema("Optional description, at most 200 characters.")))

	common := func(items ...openapi3.NewResponsesOption) *openapi3.Responses {
		return responses(append(items,
			response(http.StatusBadRequest, "Invalid release channel request.", schemaRef("ErrorResponse")),
			response(http.StatusUnauthorized, "Console session is required.", schemaRef("ErrorResponse")),
			response(http.StatusNotFound, "Application or channel not found.", schemaRef("ErrorResponse")),
			response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
		)...)
	}
	forbidden := response(http.StatusForbidden, "Current user cannot release in this application (release:create).", schemaRef("ErrorResponse"))
	code := &openapi3.ParameterRef{Value: openapi3.NewPathParameter("code").WithDescription("Channel code.").WithRequired(true).WithSchema(stringSchema("Channel code."))}

	list := operation("listReleaseChannels", "List release channels", []string{"Releases"}, common(
		response(http.StatusOK, "Channels of the application, oldest first.", schemaRef("ReleaseChannelsResponse")),
	))
	list.Parameters = applicationSlugParameters()
	create := operation("createReleaseChannel", "Create release channel", []string{"Releases"}, common(
		response(http.StatusCreated, "Channel created.", schemaRef("ReleaseChannel")),
		forbidden,
		response(http.StatusConflict, "error is \"release channel already exists\".", schemaRef("ErrorResponse")),
	))
	create.Parameters = applicationSlugParameters()
	create.RequestBody = requestBody("Release channel payload.", schemaRef("ReleaseChannelCreateRequest"))
	doc.Paths.Set("/api/v1/applications/{slug}/channels", &openapi3.PathItem{Get: list, Post: create})

	update := operation("updateReleaseChannel", "Update release channel", []string{"Releases"}, common(
		response(http.StatusOK, "Channel updated.", schemaRef("ReleaseChannel")),
		forbidden,
	))
	update.Parameters = append(applicationSlugParameters(), code)
	update.RequestBody = requestBody("Release channel payload.", schemaRef("ReleaseChannelUpdateRequest"))
	remove := operation("deleteReleaseChannel", "Delete release channel", []string{"Releases"}, common(
		responseNoBody(http.StatusNoContent, "Channel deleted. Historical releases keep their channel code."),
		forbidden,
		response(http.StatusConflict, "error is \"release channel in use\": the channel still has active releases in some environment.", schemaRef("ErrorResponse")),
	))
	remove.Parameters = append(applicationSlugParameters(), code)
	doc.Paths.Set("/api/v1/applications/{slug}/channels/{code}", &openapi3.PathItem{Patch: update, Delete: remove})
}
