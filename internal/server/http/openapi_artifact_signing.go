package httpserver

import (
	"net/http"

	"forge-server/internal/domain"

	"github.com/getkin/kin-openapi/openapi3"
)

// addArtifactSigningOpenAPI 注册包签名的组件和密钥管理接口，规则见 docs/artifact-signing.md。
func addArtifactSigningOpenAPI(doc *openapi3.T) {
	schemas := doc.Components.Schemas

	schemas["ArtifactSignature"] = schemaValue(objectSchema("algorithm", "keyId", "payload", "signature").
		WithProperty("algorithm", enumStringSchema("Signature algorithm.", domain.ArtifactSigningAlgorithm)).
		WithProperty("keyId", stringSchema("Key id of the signing key; pick the embedded public key with this id.")).
		WithProperty("payload", stringSchema("Base64 of the signed JSON bytes: application, moduleId, environment, buildRecordId, buildNumber, runtimeVersion, commitSha, signedAt and files (path with platform prefix, sizeBytes, sha256). Verify the signature over these exact bytes before parsing.")).
		WithProperty("signature", stringSchema("Base64 Ed25519 signature of the payload bytes.")))
	schemas["SigningKey"] = schemaValue(objectSchema("keyId", "algorithm", "publicKey", "status", "createdBy", "createdAt", "retiredAt", "signedBuilds").
		WithProperty("keyId", stringSchema("First 16 hex characters of the public key's SHA-256.")).
		WithProperty("algorithm", enumStringSchema("Signature algorithm.", domain.ArtifactSigningAlgorithm)).
		WithProperty("publicKey", stringSchema("Base64 of the raw 32-byte Ed25519 public key, to embed in clients.")).
		WithProperty("status", enumStringSchema("active signs new builds; retired no longer signs but its signed builds stay valid.", string(domain.SigningKeyStatusActive), string(domain.SigningKeyStatusRetired))).
		WithProperty("createdBy", nullableSchemaRef("UserRef", "Who generated the key; null when the user was deleted.")).
		WithProperty("createdAt", dateTimeSchema("Creation time.")).
		WithProperty("retiredAt", stringSchema("Time the key was retired; empty for the active key.")).
		WithProperty("signedBuilds", int32Schema("Builds signed with this key.")))
	schemas["ApplicationSigningResponse"] = schemaValue(objectSchema("requireSignedReleases", "keys").
		WithProperty("requireSignedReleases", boolSchema("When true, creating a release or rolling back rejects unsigned builds.")).
		WithPropertyRef("keys", arraySchemaRef("SigningKey")))
	schemas["ApplicationSigningUpdateRequest"] = schemaValue(objectSchema("requireSignedReleases").
		WithProperty("requireSignedReleases", boolSchema("Only allow releasing signed builds. Turning it on requires an active key.")))

	common := func(items ...openapi3.NewResponsesOption) *openapi3.Responses {
		return responses(append(items,
			response(http.StatusBadRequest, "Invalid signing request.", schemaRef("ErrorResponse")),
			response(http.StatusUnauthorized, "Console session is required.", schemaRef("ErrorResponse")),
			response(http.StatusNotFound, "Application or signing key not found.", schemaRef("ErrorResponse")),
			response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
		)...)
	}
	forbidden := response(http.StatusForbidden, "Current user cannot update this application (application:update).", schemaRef("ErrorResponse"))

	get := operation("getApplicationSigning", "Get application signing settings", []string{"Applications"}, common(
		response(http.StatusOK, "Signing settings and keys, active key first. Private keys are never returned.", schemaRef("ApplicationSigningResponse")),
	))
	get.Parameters = applicationSlugParameters()
	update := operation("updateApplicationSigning", "Update application signing settings", []string{"Applications"}, common(
		response(http.StatusOK, "Updated signing settings.", schemaRef("ApplicationSigningResponse")),
		forbidden,
		response(http.StatusConflict, "error is \"signing key required\": turning on requireSignedReleases needs an active key.", schemaRef("ErrorResponse")),
	))
	update.Parameters = applicationSlugParameters()
	update.RequestBody = requestBody("Signing settings payload.", schemaRef("ApplicationSigningUpdateRequest"))
	doc.Paths.Set("/api/v1/applications/{slug}/signing", &openapi3.PathItem{Get: get, Patch: update})

	create := operation("createSigningKey", "Generate signing key", []string{"Applications"}, common(
		response(http.StatusCreated, "New active Ed25519 key. The previous active key becomes retired. Ship clients with the new public key before generating it, otherwise they reject newly signed builds.", schemaRef("SigningKey")),
		forbidden,
	))
	create.Parameters = applicationSlugParameters()
	doc.Paths.Set("/api/v1/applications/{slug}/signing/keys", &openapi3.PathItem{Post: create})

	keyID := &openapi3.ParameterRef{Value: openapi3.NewPathParameter("keyId").WithDescription("Signing key id.").WithRequired(true).WithSchema(stringSchema("Signing key id."))}
	remove := operation("deleteSigningKey", "Delete retired signing key", []string{"Applications"}, common(
		responseNoBody(http.StatusNoContent, "Key deleted. Builds it signed keep their signatures."),
		forbidden,
		response(http.StatusConflict, "error is \"signing key active\": generate a new key first.", schemaRef("ErrorResponse")),
	))
	remove.Parameters = append(applicationSlugParameters(), keyID)
	doc.Paths.Set("/api/v1/applications/{slug}/signing/keys/{keyId}", &openapi3.PathItem{Delete: remove})
}
