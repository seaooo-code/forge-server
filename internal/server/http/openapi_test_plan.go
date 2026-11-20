package httpserver

import (
	"net/http"

	"forge-server/internal/domain"

	"github.com/getkin/kin-openapi/openapi3"
)

// addTestPlanOpenAPI 把提测单和测试账号接口加入 OpenAPI 文档，规则见 docs/test-plans.md。
func addTestPlanOpenAPI(doc *openapi3.T) {
	doc.Tags = append(doc.Tags, &openapi3.Tag{
		Name:        "TestPlans",
		Description: "Test plans: per-requirement sets of module builds delivered to a list of client user ids.",
	})

	idParameter := &openapi3.ParameterRef{Value: openapi3.NewPathParameter("id").
		WithDescription("Test plan UUID.").
		WithRequired(true).
		WithSchema(stringSchema("Test plan UUID."))}
	moduleParameter := &openapi3.ParameterRef{Value: openapi3.NewPathParameter("moduleId").
		WithDescription("Module id.").
		WithRequired(true).
		WithSchema(stringSchema("Module id."))}
	userParameter := &openapi3.ParameterRef{Value: openapi3.NewPathParameter("userId").
		WithDescription("Client user id.").
		WithRequired(true).
		WithSchema(stringSchema("Client user id."))}
	environmentParameter := &openapi3.ParameterRef{Value: openapi3.NewPathParameter("environment").
		WithDescription("Environment code.").
		WithRequired(true).
		WithSchema(stringSchema("Environment code."))}

	common := func(items ...openapi3.NewResponsesOption) *openapi3.Responses {
		items = append(items,
			response(http.StatusUnauthorized, "Authentication is required.", schemaRef("ErrorResponse")),
			response(http.StatusInternalServerError, "Server error.", schemaRef("ErrorResponse")),
		)
		return responses(items...)
	}
	mutationConflicts := response(http.StatusConflict, "Conflict. error is \"test plan member conflict\" with conflicts (retry with resolveConflicts=true to remove those user ids from the other test plans), \"test plan branch in use\" with the occupying test plan, \"test plan closed\", or \"test plans disabled for environment\".", schemaRef("TestPlanConflictErrorResponse"))
	withParams := func(op *openapi3.Operation, params ...*openapi3.ParameterRef) *openapi3.Operation {
		op.Parameters = append(op.Parameters, params...)
		return op
	}
	withBody := func(op *openapi3.Operation, description string, schema string) *openapi3.Operation {
		op.RequestBody = requestBody(description, schemaRef(schema))
		return op
	}

	listParameters := openapi3.Parameters{
		{Value: openapi3.NewQueryParameter("applicationId").WithDescription("Filter by application id.").WithSchema(stringSchema("Application id."))},
		{Value: openapi3.NewQueryParameter("environment").WithDescription("Filter by environment code.").WithSchema(stringSchema("Environment code."))},
		{Value: openapi3.NewQueryParameter("status").WithDescription("open: open and not expired. closed: closed or expired.").WithSchema(enumStringSchema("Status filter.", "open", "closed"))},
		{Value: openapi3.NewQueryParameter("memberUserId").WithDescription("Only test plans that contain this client user id.").WithSchema(stringSchema("Client user id."))},
		{Value: openapi3.NewQueryParameter("page").WithDescription("One-based page number.").WithSchema(int32Schema("Page."))},
		{Value: openapi3.NewQueryParameter("pageSize").WithDescription("Rows per page.").WithSchema(int32Schema("Page size."))},
	}

	doc.Paths.Set("/api/v1/test-plans", &openapi3.PathItem{
		Get: func() *openapi3.Operation {
			op := operation("listTestPlans", "List test plans", []string{"TestPlans"}, common(
				response(http.StatusOK, "Test plan list.", schemaRef("TestPlansResponse")),
				response(http.StatusBadRequest, "Invalid filters.", schemaRef("ErrorResponse")),
			))
			op.Parameters = listParameters
			return op
		}(),
		Post: withBody(operation("createTestPlan", "Create test plan", []string{"TestPlans"}, common(
			response(http.StatusCreated, "Test plan created.", schemaRef("TestPlanMutationResponse")),
			response(http.StatusBadRequest, "Invalid request, or a build is not a succeeded build of that module, branch and environment.", schemaRef("ErrorResponse")),
			response(http.StatusForbidden, "Current user cannot manage test plans of this application.", schemaRef("ErrorResponse")),
			response(http.StatusNotFound, "Module not found.", schemaRef("ErrorResponse")),
			mutationConflicts,
		)), "Test plan creation payload.", "TestPlanCreateRequest"),
	})
	doc.Paths.Set("/api/v1/test-plans/{id}", &openapi3.PathItem{
		Get: withParams(operation("getTestPlan", "Get test plan detail", []string{"TestPlans"}, common(
			response(http.StatusOK, "Test plan detail.", schemaRef("TestPlanDetailResponse")),
			response(http.StatusNotFound, "Test plan not found.", schemaRef("ErrorResponse")),
		)), idParameter),
		Patch: withBody(withParams(operation("updateTestPlan", "Update test plan name or link", []string{"TestPlans"}, common(
			response(http.StatusOK, "Test plan updated.", schemaRef("TestPlanDetailResponse")),
			response(http.StatusBadRequest, "Invalid request.", schemaRef("ErrorResponse")),
			response(http.StatusForbidden, "Current user cannot manage this test plan.", schemaRef("ErrorResponse")),
			response(http.StatusNotFound, "Test plan not found.", schemaRef("ErrorResponse")),
			response(http.StatusConflict, "Test plan is closed or expired.", schemaRef("ErrorResponse")),
		)), idParameter), "Fields to update.", "TestPlanUpdateRequest"),
	})
	doc.Paths.Set("/api/v1/test-plans/{id}/close", &openapi3.PathItem{
		Post: withParams(operation("closeTestPlan", "Close test plan", []string{"TestPlans"}, common(
			response(http.StatusOK, "Test plan closed.", schemaRef("TestPlanDetailResponse")),
			response(http.StatusForbidden, "Current user cannot manage this test plan.", schemaRef("ErrorResponse")),
			response(http.StatusNotFound, "Test plan not found.", schemaRef("ErrorResponse")),
			response(http.StatusConflict, "Test plan is already closed or expired.", schemaRef("ErrorResponse")),
		)), idParameter),
	})
	doc.Paths.Set("/api/v1/test-plans/{id}/extend", &openapi3.PathItem{
		Post: withBody(withParams(operation("extendTestPlan", "Change test plan expiration", []string{"TestPlans"}, common(
			response(http.StatusOK, "Expiration changed.", schemaRef("TestPlanDetailResponse")),
			response(http.StatusBadRequest, "expiresAt must be in the future and at most 30 days from now.", schemaRef("ErrorResponse")),
			response(http.StatusForbidden, "Current user cannot manage this test plan.", schemaRef("ErrorResponse")),
			response(http.StatusNotFound, "Test plan not found.", schemaRef("ErrorResponse")),
			response(http.StatusConflict, "Test plan is closed or expired; create a new one instead.", schemaRef("ErrorResponse")),
		)), idParameter), "New expiration.", "TestPlanExtendRequest"),
	})
	doc.Paths.Set("/api/v1/test-plans/{id}/items/{moduleId}", &openapi3.PathItem{
		Put: withBody(withParams(operation("setTestPlanItem", "Set the build of a module in a test plan", []string{"TestPlans"}, common(
			response(http.StatusOK, "Item set.", schemaRef("TestPlanMutationResponse")),
			response(http.StatusBadRequest, "Invalid request, or no usable build for the branch.", schemaRef("ErrorResponse")),
			response(http.StatusForbidden, "Current user cannot manage this test plan.", schemaRef("ErrorResponse")),
			response(http.StatusNotFound, "Test plan or module not found.", schemaRef("ErrorResponse")),
			mutationConflicts,
		)), idParameter, moduleParameter), "Item payload.", "TestPlanItemSetRequest"),
		Delete: withParams(operation("deleteTestPlanItem", "Remove a module from a test plan", []string{"TestPlans"}, common(
			response(http.StatusOK, "Item removed.", schemaRef("TestPlanDetailResponse")),
			response(http.StatusForbidden, "Current user cannot manage this test plan.", schemaRef("ErrorResponse")),
			response(http.StatusNotFound, "Test plan or item not found.", schemaRef("ErrorResponse")),
			response(http.StatusConflict, "Test plan is closed or expired.", schemaRef("ErrorResponse")),
		)), idParameter, moduleParameter),
	})
	doc.Paths.Set("/api/v1/test-plans/{id}/members", &openapi3.PathItem{
		Post: withBody(withParams(operation("addTestPlanMembers", "Add client user ids to a test plan", []string{"TestPlans"}, common(
			response(http.StatusOK, "Members added.", schemaRef("TestPlanMutationResponse")),
			response(http.StatusBadRequest, "Invalid user ids or member limit exceeded.", schemaRef("ErrorResponse")),
			response(http.StatusForbidden, "Current user cannot manage this test plan.", schemaRef("ErrorResponse")),
			response(http.StatusNotFound, "Test plan not found.", schemaRef("ErrorResponse")),
			mutationConflicts,
		)), idParameter), "Members to add.", "TestPlanMembersAddRequest"),
	})
	doc.Paths.Set("/api/v1/test-plans/{id}/members/{userId}", &openapi3.PathItem{
		Delete: withParams(operation("removeTestPlanMember", "Remove a client user id from a test plan", []string{"TestPlans"}, common(
			response(http.StatusOK, "Member removed.", schemaRef("TestPlanDetailResponse")),
			response(http.StatusForbidden, "Current user cannot manage this test plan.", schemaRef("ErrorResponse")),
			response(http.StatusNotFound, "Test plan or member not found.", schemaRef("ErrorResponse")),
			response(http.StatusConflict, "Test plan is closed or expired.", schemaRef("ErrorResponse")),
		)), idParameter, userParameter),
	})
	doc.Paths.Set("/api/v1/test-plans/{id}/join", &openapi3.PathItem{
		Post: withBody(withParams(operation("joinTestPlan", "Join a test plan with my test accounts", []string{"TestPlans"}, common(
			response(http.StatusOK, "Joined.", schemaRef("TestPlanMutationResponse")),
			response(http.StatusBadRequest, "Invalid user ids or member limit exceeded.", schemaRef("ErrorResponse")),
			response(http.StatusNotFound, "Test plan not found, or the user ids are not my test accounts in this environment.", schemaRef("ErrorResponse")),
			mutationConflicts,
		)), idParameter), "Which of my test accounts to add. Empty userIds adds all of my test accounts in the test plan environment.", "TestPlanJoinRequest"),
	})
	doc.Paths.Set("/api/v1/test-plans/{id}/leave", &openapi3.PathItem{
		Post: withBody(withParams(operation("leaveTestPlan", "Remove my test accounts from a test plan", []string{"TestPlans"}, common(
			response(http.StatusOK, "Left.", schemaRef("TestPlanDetailResponse")),
			response(http.StatusNotFound, "Test plan not found, or none of my test accounts is a member.", schemaRef("ErrorResponse")),
			response(http.StatusConflict, "Test plan is closed or expired.", schemaRef("ErrorResponse")),
		)), idParameter), "Which of my test accounts to remove. Empty userIds removes all.", "TestPlanLeaveRequest"),
	})
	doc.Paths.Set("/api/v1/me/test-accounts", &openapi3.PathItem{
		Get: func() *openapi3.Operation {
			op := operation("listMyTestAccounts", "List my test accounts", []string{"TestPlans"}, common(
				response(http.StatusOK, "My test accounts.", schemaRef("TestAccountsResponse")),
			))
			op.Parameters = openapi3.Parameters{
				{Value: openapi3.NewQueryParameter("environment").WithDescription("Only accounts of this environment.").WithSchema(stringSchema("Environment code."))},
			}
			return op
		}(),
		Post: withBody(operation("upsertMyTestAccount", "Save a test account", []string{"TestPlans"}, common(
			response(http.StatusOK, "Test account saved; an existing one only updates its note.", schemaRef("TestAccount")),
			response(http.StatusBadRequest, "Invalid request.", schemaRef("ErrorResponse")),
		)), "Test account payload.", "TestAccountRequest"),
	})
	doc.Paths.Set("/api/v1/me/test-accounts/{environment}/{userId}", &openapi3.PathItem{
		Delete: withParams(operation("deleteMyTestAccount", "Delete a test account", []string{"TestPlans"}, common(
			responseNoBody(http.StatusNoContent, "Deleted."),
			response(http.StatusNotFound, "Test account not found.", schemaRef("ErrorResponse")),
		)), environmentParameter, userParameter),
	})

	schemas := doc.Components.Schemas
	schemas["TestPlanRef"] = schemaValue(objectSchema("id", "name").
		WithProperty("id", stringSchema("Test plan UUID.")).
		WithProperty("name", stringSchema("Test plan name.")))
	schemas["TestPlan"] = schemaValue(objectSchema("id", "applicationId", "applicationName", "applicationSlug", "environment", "environmentName", "name", "link", "status", "expiresAt", "createdByUserId", "createdBy", "createdAt", "updatedAt", "closedAt", "itemCount", "memberCount").
		WithProperty("id", stringSchema("Test plan UUID.")).
		WithProperty("applicationId", stringSchema("Application id.")).
		WithProperty("applicationName", stringSchema("Application display name.")).
		WithProperty("applicationSlug", stringSchema("Application slug.")).
		WithProperty("environment", stringSchema("Environment code.")).
		WithProperty("environmentName", stringSchema("Environment display name.")).
		WithProperty("name", stringSchema("Test plan name, for example the requirement title.")).
		WithProperty("link", stringSchema("Related link, for example the requirement ticket.")).
		WithProperty("status", enumStringSchema("Derived status. Only open test plans take effect and take part in conflict checks.", "open", "closed", "expired")).
		WithProperty("expiresAt", dateTimeSchema("Expiration time.")).
		WithProperty("createdByUserId", stringSchema("Creator user id.")).
		WithProperty("createdBy", stringSchema("Creator display name.")).
		WithProperty("createdAt", dateTimeSchema("Creation time.")).
		WithProperty("updatedAt", dateTimeSchema("Last change of the plan, its items or members.")).
		WithProperty("closedAt", stringSchema("Close time, or empty.")).
		WithProperty("itemCount", int32Schema("Item count.")).
		WithProperty("memberCount", int32Schema("Member count.")))
	schemas["TestPlanItem"] = schemaValue(objectSchema("moduleId", "moduleName", "moduleKind", "branch", "mode", "buildRecordId", "buildNumber", "commitSha", "runtimeVersion", "platforms", "updatedBy", "updatedAt").
		WithProperty("moduleId", stringSchema("Module id.")).
		WithProperty("moduleName", stringSchema("Module display name.")).
		WithProperty("moduleKind", enumStringSchema("Module kind.", "Common", "Feature")).
		WithProperty("branch", stringSchema("Bound branch. The same branch of a module belongs to at most one open test plan per environment.")).
		WithProperty("mode", enumStringSchema("follow: replaced by every newer succeeded build of the branch. pinned: fixed build.", string(domain.TestPlanItemModeFollow), string(domain.TestPlanItemModePinned))).
		WithProperty("buildRecordId", stringSchema("Current build record UUID.")).
		WithProperty("buildNumber", int64Schema("Build number within the module.")).
		WithProperty("commitSha", stringSchema("Commit SHA of the build.")).
		WithProperty("runtimeVersion", stringSchema("runtimeVersion of the build. Clients with another runtimeVersion skip this item.")).
		WithProperty("platforms", stringArraySchema("Platforms in the build.")).
		WithProperty("updatedBy", stringSchema("Who last set the item; empty when updated by a followed build.")).
		WithProperty("updatedAt", dateTimeSchema("Last update time.")))
	schemas["TestPlanMember"] = schemaValue(objectSchema("userId", "note", "addedBy", "createdAt").
		WithProperty("userId", stringSchema("Client user id, matched against the manifest userId.")).
		WithProperty("note", stringSchema("Note, for example who the account belongs to.")).
		WithProperty("addedBy", stringSchema("Who added the member.")).
		WithProperty("createdAt", dateTimeSchema("Added time.")))
	eventDetail := openapi3.NewObjectSchema()
	eventDetail.Description = "Event specific fields, for example userId, moduleIds and causedByTestPlanId for member_removed_by_conflict."
	schemas["TestPlanEvent"] = schemaValue(objectSchema("id", "kind", "detail", "actor", "createdAt").
		WithProperty("id", int64Schema("Event id.")).
		WithProperty("kind", enumStringSchema("Event kind.",
			string(domain.TestPlanEventCreated),
			string(domain.TestPlanEventUpdated),
			string(domain.TestPlanEventClosed),
			string(domain.TestPlanEventExtended),
			string(domain.TestPlanEventItemSet),
			string(domain.TestPlanEventItemDeleted),
			string(domain.TestPlanEventItemFollowed),
			string(domain.TestPlanEventMemberAdded),
			string(domain.TestPlanEventMemberRemoved),
			string(domain.TestPlanEventMemberRemovedByConflict),
		)).
		WithProperty("detail", eventDetail).
		WithProperty("actor", stringSchema("Who did it; empty for automatic updates.")).
		WithProperty("createdAt", dateTimeSchema("Event time.")))
	schemas["TestPlanMemberConflict"] = schemaValue(objectSchema("userId", "testPlan", "moduleIds").
		WithProperty("userId", stringSchema("Client user id that is also in another open test plan.")).
		WithPropertyRef("testPlan", schemaRef("TestPlanRef")).
		WithProperty("moduleIds", stringArraySchema("Modules contained in both test plans.")))
	schemas["TestPlansResponse"] = schemaValue(objectSchema("testPlans", "pagination").
		WithPropertyRef("testPlans", arraySchemaRef("TestPlan")).
		WithPropertyRef("pagination", schemaRef("Pagination")))
	detail := objectSchema("testPlan", "items", "members", "events", "commonDependencyWarning").
		WithPropertyRef("testPlan", schemaRef("TestPlan")).
		WithPropertyRef("items", arraySchemaRef("TestPlanItem")).
		WithPropertyRef("members", arraySchemaRef("TestPlanMember")).
		WithPropertyRef("events", arraySchemaRef("TestPlanEvent")).
		WithProperty("commonDependencyWarning", boolSchema("Items contain both a Common module and Feature modules. Feature builds were built against the released common, not the common build in this test plan, so they may be incompatible."))
	schemas["TestPlanDetailResponse"] = schemaValue(detail)
	mutation := objectSchema("testPlan", "items", "members", "events", "commonDependencyWarning", "removed").
		WithPropertyRef("testPlan", schemaRef("TestPlan")).
		WithPropertyRef("items", arraySchemaRef("TestPlanItem")).
		WithPropertyRef("members", arraySchemaRef("TestPlanMember")).
		WithPropertyRef("events", arraySchemaRef("TestPlanEvent")).
		WithProperty("commonDependencyWarning", boolSchema("Same as TestPlanDetailResponse.")).
		WithPropertyRef("removed", arraySchemaRef("TestPlanMemberConflict"))
	mutation.Description = "Test plan detail after the change. removed lists user ids that were removed from other test plans because resolveConflicts was true."
	schemas["TestPlanMutationResponse"] = schemaValue(mutation)
	schemas["TestPlanConflictErrorResponse"] = schemaValue(objectSchema("error").
		WithProperty("error", enumStringSchema("Conflict kind.", "test plan member conflict", "test plan branch in use", "test plan closed", "test plans disabled for environment")).
		WithPropertyRef("conflicts", arraySchemaRef("TestPlanMemberConflict")).
		WithProperty("moduleId", stringSchema("Module whose branch is in use.")).
		WithProperty("branch", stringSchema("Branch in use.")).
		WithProperty("testPlan", nullableSchemaRef("TestPlanRef", "Test plan occupying the branch.")))

	itemRequest := objectSchema("moduleId", "branch").
		WithProperty("moduleId", stringSchema("Module id of this application.")).
		WithProperty("branch", stringSchema("Branch to bind.")).
		WithProperty("mode", enumStringSchema("Defaults to follow.", string(domain.TestPlanItemModeFollow), string(domain.TestPlanItemModePinned))).
		WithProperty("buildRecordId", stringSchema("Build to use. Required for pinned; for follow, empty means the latest succeeded build of the branch."))
	schemas["TestPlanItemRequest"] = schemaValue(itemRequest)
	schemas["TestPlanMemberRequest"] = schemaValue(objectSchema("userId").
		WithProperty("userId", stringSchema("Client user id, 1-64 characters of [A-Za-z0-9._-].")).
		WithProperty("note", stringSchema("Optional note, at most 64 characters.")))
	resolve := boolSchema("Remove conflicting user ids from the other test plans instead of returning 409 test plan member conflict.")
	schemas["TestPlanCreateRequest"] = schemaValue(objectSchema("applicationId", "environment", "name").
		WithProperty("applicationId", stringSchema("Application id.")).
		WithProperty("environment", stringSchema("Environment code. The environment must be bound to the application and allow test plans.")).
		WithProperty("name", stringSchema("Name, 1-100 characters.")).
		WithProperty("link", stringSchema("Optional http(s) link.")).
		WithProperty("expiresAt", dateTimeSchema("Optional expiration, default 7 days, at most 30 days from now.")).
		WithPropertyRef("items", arraySchemaRef("TestPlanItemRequest")).
		WithPropertyRef("members", arraySchemaRef("TestPlanMemberRequest")).
		WithProperty("resolveConflicts", resolve))
	schemas["TestPlanUpdateRequest"] = schemaValue(objectSchema().
		WithProperty("name", stringSchema("New name.")).
		WithProperty("link", stringSchema("New link; empty string clears it.")))
	schemas["TestPlanExtendRequest"] = schemaValue(objectSchema("expiresAt").
		WithProperty("expiresAt", dateTimeSchema("New expiration, in the future and at most 30 days from now.")))
	schemas["TestPlanItemSetRequest"] = schemaValue(objectSchema("branch").
		WithProperty("branch", stringSchema("Branch to bind.")).
		WithProperty("mode", enumStringSchema("Defaults to follow.", string(domain.TestPlanItemModeFollow), string(domain.TestPlanItemModePinned))).
		WithProperty("buildRecordId", stringSchema("Build to use. Required for pinned; for follow, empty means the latest succeeded build of the branch.")).
		WithProperty("resolveConflicts", resolve))
	schemas["TestPlanMembersAddRequest"] = schemaValue(objectSchema("members").
		WithPropertyRef("members", arraySchemaRef("TestPlanMemberRequest")).
		WithProperty("resolveConflicts", resolve))
	schemas["TestPlanJoinRequest"] = schemaValue(objectSchema().
		WithProperty("userIds", stringArraySchema("My test account user ids to add; empty adds all of them in the test plan environment.")).
		WithProperty("resolveConflicts", resolve))
	schemas["TestPlanLeaveRequest"] = schemaValue(objectSchema().
		WithProperty("userIds", stringArraySchema("My test account user ids to remove; empty removes all.")))
	schemas["TestAccount"] = schemaValue(objectSchema("environment", "userId", "note", "createdAt").
		WithProperty("environment", stringSchema("Environment code. Account systems may differ between environments.")).
		WithProperty("userId", stringSchema("Client user id.")).
		WithProperty("note", stringSchema("Note.")).
		WithProperty("createdAt", dateTimeSchema("Saved time.")))
	schemas["TestAccountsResponse"] = schemaValue(objectSchema("testAccounts").
		WithPropertyRef("testAccounts", arraySchemaRef("TestAccount")))
	schemas["TestAccountRequest"] = schemaValue(objectSchema("environment", "userId").
		WithProperty("environment", stringSchema("Environment code.")).
		WithProperty("userId", stringSchema("Client user id, 1-64 characters of [A-Za-z0-9._-].")).
		WithProperty("note", stringSchema("Optional note, at most 64 characters.")))
}
