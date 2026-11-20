package httpserver

import (
	"errors"
	"net/http"
	"time"

	"forge-server/internal/domain"

	"github.com/labstack/echo/v4"
)

// 提测单接口，规则见 docs/test-plans.md。查看列表和详情只要求登录；
// 管理提测单需要应用的 test_plan:manage；用自己的测试账号加入、退出只要求登录。
type testPlanHandler struct {
	authService AuthorizationService
	service     TestPlanService
}

type testPlanItemRequest struct {
	ModuleID      string `json:"moduleId"`
	Branch        string `json:"branch"`
	Mode          string `json:"mode"`
	BuildRecordID string `json:"buildRecordId"`
}

type testPlanMemberRequest struct {
	UserID string `json:"userId"`
	Note   string `json:"note"`
}

type testPlanCreateRequest struct {
	ApplicationID    string                  `json:"applicationId"`
	Environment      string                  `json:"environment"`
	Name             string                  `json:"name"`
	Link             string                  `json:"link"`
	ExpiresAt        string                  `json:"expiresAt"`
	Items            []testPlanItemRequest   `json:"items"`
	Members          []testPlanMemberRequest `json:"members"`
	ResolveConflicts bool                    `json:"resolveConflicts"`
}

type testPlanUpdateRequest struct {
	Name *string `json:"name"`
	Link *string `json:"link"`
}

type testPlanExtendRequest struct {
	ExpiresAt string `json:"expiresAt"`
}

type testPlanItemSetRequest struct {
	Branch           string `json:"branch"`
	Mode             string `json:"mode"`
	BuildRecordID    string `json:"buildRecordId"`
	ResolveConflicts bool   `json:"resolveConflicts"`
}

type testPlanMembersAddRequest struct {
	Members          []testPlanMemberRequest `json:"members"`
	ResolveConflicts bool                    `json:"resolveConflicts"`
}

type testPlanJoinRequest struct {
	UserIDs          []string `json:"userIds"`
	ResolveConflicts bool     `json:"resolveConflicts"`
}

type testPlanLeaveRequest struct {
	UserIDs []string `json:"userIds"`
}

type testAccountRequest struct {
	Environment string `json:"environment"`
	UserID      string `json:"userId"`
	Note        string `json:"note"`
}

type testPlanResponse struct {
	ID              string `json:"id"`
	ApplicationID   string `json:"applicationId"`
	ApplicationName string `json:"applicationName"`
	ApplicationSlug string `json:"applicationSlug"`
	Environment     string `json:"environment"`
	EnvironmentName string `json:"environmentName"`
	Name            string `json:"name"`
	Link            string `json:"link"`
	// Status 是派生状态：open（进行中）、closed（已关闭）、expired（已过期）。
	Status          string `json:"status"`
	ExpiresAt       string `json:"expiresAt"`
	CreatedByUserID string `json:"createdByUserId"`
	CreatedBy       string `json:"createdBy"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
	ClosedAt        string `json:"closedAt"`
	ItemCount       int    `json:"itemCount"`
	MemberCount     int    `json:"memberCount"`
}

type testPlansResponse struct {
	Pagination paginationResponse `json:"pagination"`
	TestPlans  []testPlanResponse `json:"testPlans"`
}

type testPlanItemResponse struct {
	ModuleID       string   `json:"moduleId"`
	ModuleName     string   `json:"moduleName"`
	ModuleKind     string   `json:"moduleKind"`
	Branch         string   `json:"branch"`
	Mode           string   `json:"mode"`
	BuildRecordID  string   `json:"buildRecordId"`
	BuildNumber    int64    `json:"buildNumber"`
	CommitSHA      string   `json:"commitSha"`
	RuntimeVersion string   `json:"runtimeVersion"`
	Platforms      []string `json:"platforms"`
	UpdatedBy      string   `json:"updatedBy"`
	UpdatedAt      string   `json:"updatedAt"`
}

type testPlanMemberResponse struct {
	UserID    string `json:"userId"`
	Note      string `json:"note"`
	AddedBy   string `json:"addedBy"`
	CreatedAt string `json:"createdAt"`
}

type testPlanEventResponse struct {
	ID        int64          `json:"id"`
	Kind      string         `json:"kind"`
	Detail    map[string]any `json:"detail"`
	Actor     string         `json:"actor"`
	CreatedAt string         `json:"createdAt"`
}

type testPlanDetailResponse struct {
	TestPlan testPlanResponse         `json:"testPlan"`
	Items    []testPlanItemResponse   `json:"items"`
	Members  []testPlanMemberResponse `json:"members"`
	Events   []testPlanEventResponse  `json:"events"`
	// CommonDependencyWarning 表示条目里同时有 Common 模块和 Feature 模块：Feature 构建注入的是已发布的 common，
	// 不是本提测单里的 common 构建，可能不兼容。
	CommonDependencyWarning bool `json:"commonDependencyWarning"`
}

type testPlanRefResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type testPlanMemberConflictResponse struct {
	UserID    string              `json:"userId"`
	TestPlan  testPlanRefResponse `json:"testPlan"`
	ModuleIDs []string            `json:"moduleIds"`
}

type testPlanMutationResponse struct {
	testPlanDetailResponse
	Removed []testPlanMemberConflictResponse `json:"removed"`
}

type testPlanMemberConflictErrorResponse struct {
	Error     string                           `json:"error"`
	Conflicts []testPlanMemberConflictResponse `json:"conflicts"`
}

type testPlanBranchInUseErrorResponse struct {
	Error    string              `json:"error"`
	ModuleID string              `json:"moduleId"`
	Branch   string              `json:"branch"`
	TestPlan testPlanRefResponse `json:"testPlan"`
}

type testAccountResponse struct {
	Environment string `json:"environment"`
	UserID      string `json:"userId"`
	Note        string `json:"note"`
	CreatedAt   string `json:"createdAt"`
}

type testAccountsResponse struct {
	TestAccounts []testAccountResponse `json:"testAccounts"`
}

func registerTestPlanRoutes(group *echo.Group, service TestPlanService, authService AuthorizationService) {
	handler := testPlanHandler{authService: authService, service: service}

	group.GET("/test-plans", handler.list)
	group.POST("/test-plans", handler.create)
	group.GET("/test-plans/:id", handler.get)
	group.PATCH("/test-plans/:id", handler.update)
	group.POST("/test-plans/:id/close", handler.close)
	group.POST("/test-plans/:id/extend", handler.extend)
	group.PUT("/test-plans/:id/items/:moduleId", handler.setItem)
	group.DELETE("/test-plans/:id/items/:moduleId", handler.deleteItem)
	group.POST("/test-plans/:id/members", handler.addMembers)
	group.DELETE("/test-plans/:id/members/:userId", handler.removeMember)
	group.POST("/test-plans/:id/join", handler.join)
	group.POST("/test-plans/:id/leave", handler.leave)

	group.GET("/me/test-accounts", handler.listTestAccounts)
	group.POST("/me/test-accounts", handler.upsertTestAccount)
	group.DELETE("/me/test-accounts/:environment/:userId", handler.deleteTestAccount)
}

func (h testPlanHandler) list(c echo.Context) error {
	if h.service == nil {
		return writeErrorResponse(c, http.StatusServiceUnavailable, "test plan service unavailable")
	}
	pagination, ok := requestPagination(c)
	if !ok {
		return testPlanErrorResponse(c, domain.ErrInvalidTestPlanInput)
	}
	plans, total, err := h.service.ListTestPlans(c.Request().Context(), domain.TestPlanFilters{
		ApplicationID: c.QueryParam("applicationId"),
		Environment:   c.QueryParam("environment"),
		Status:        domain.TestPlanStatus(c.QueryParam("status")),
		MemberUserID:  c.QueryParam("memberUserId"),
		Pagination:    pagination,
	})
	if err != nil {
		return testPlanErrorResponse(c, err)
	}
	now := time.Now()
	responses := make([]testPlanResponse, 0, len(plans))
	for _, plan := range plans {
		responses = append(responses, newTestPlanResponse(plan, now))
	}
	return c.JSON(http.StatusOK, testPlansResponse{
		Pagination: newPaginationResponse(pagination, total),
		TestPlans:  responses,
	})
}

func (h testPlanHandler) create(c echo.Context) error {
	if h.service == nil {
		return writeErrorResponse(c, http.StatusServiceUnavailable, "test plan service unavailable")
	}
	var request testPlanCreateRequest
	if err := c.Bind(&request); err != nil {
		return testPlanErrorResponse(c, domain.ErrInvalidTestPlanInput)
	}
	if request.ApplicationID == "" {
		return testPlanErrorResponse(c, domain.ErrInvalidTestPlanInput)
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionTestPlanManage, domain.AuthorizationResource{ApplicationID: request.ApplicationID}); err != nil {
		return err
	}
	actor, ok := currentConsoleUserID(c)
	if !ok {
		return writeErrorResponse(c, http.StatusUnauthorized, "unauthorized")
	}
	expiresAt, ok := parseOptionalRFC3339(request.ExpiresAt)
	if !ok {
		return testPlanErrorResponse(c, domain.ErrInvalidTestPlanInput)
	}
	items := make([]domain.TestPlanItemInput, 0, len(request.Items))
	for _, item := range request.Items {
		items = append(items, domain.TestPlanItemInput{
			ModuleID:      item.ModuleID,
			Branch:        item.Branch,
			Mode:          domain.TestPlanItemMode(item.Mode),
			BuildRecordID: item.BuildRecordID,
		})
	}
	result, err := h.service.CreateTestPlan(c.Request().Context(), domain.TestPlanCreateInput{
		ApplicationID:    request.ApplicationID,
		Environment:      request.Environment,
		Name:             request.Name,
		Link:             request.Link,
		ExpiresAt:        expiresAt,
		Items:            items,
		Members:          testPlanMemberInputs(request.Members),
		ResolveConflicts: request.ResolveConflicts,
		ActorUserID:      actor,
	})
	if err != nil {
		return testPlanErrorResponse(c, err)
	}
	return c.JSON(http.StatusCreated, newTestPlanMutationResponse(result))
}

func (h testPlanHandler) get(c echo.Context) error {
	if h.service == nil {
		return writeErrorResponse(c, http.StatusServiceUnavailable, "test plan service unavailable")
	}
	detail, err := h.service.GetTestPlanDetail(c.Request().Context(), c.Param("id"))
	if err != nil {
		return testPlanErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newTestPlanDetailResponse(detail))
}

func (h testPlanHandler) update(c echo.Context) error {
	var request testPlanUpdateRequest
	if err := c.Bind(&request); err != nil {
		return testPlanErrorResponse(c, domain.ErrInvalidTestPlanInput)
	}
	actor, err := h.authorizeManage(c)
	if err != nil {
		return err
	}
	detail, err := h.service.UpdateTestPlan(c.Request().Context(), c.Param("id"), domain.TestPlanUpdateInput{
		Name:        request.Name,
		Link:        request.Link,
		ActorUserID: actor,
	})
	if err != nil {
		return testPlanErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newTestPlanDetailResponse(detail))
}

func (h testPlanHandler) close(c echo.Context) error {
	actor, err := h.authorizeManage(c)
	if err != nil {
		return err
	}
	detail, err := h.service.CloseTestPlan(c.Request().Context(), c.Param("id"), actor)
	if err != nil {
		return testPlanErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newTestPlanDetailResponse(detail))
}

func (h testPlanHandler) extend(c echo.Context) error {
	var request testPlanExtendRequest
	if err := c.Bind(&request); err != nil {
		return testPlanErrorResponse(c, domain.ErrInvalidTestPlanInput)
	}
	expiresAt, ok := parseOptionalRFC3339(request.ExpiresAt)
	if !ok || expiresAt == nil {
		return testPlanErrorResponse(c, domain.ErrInvalidTestPlanInput)
	}
	actor, err := h.authorizeManage(c)
	if err != nil {
		return err
	}
	detail, err := h.service.ExtendTestPlan(c.Request().Context(), c.Param("id"), domain.TestPlanExtendInput{
		ExpiresAt:   *expiresAt,
		ActorUserID: actor,
	})
	if err != nil {
		return testPlanErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newTestPlanDetailResponse(detail))
}

func (h testPlanHandler) setItem(c echo.Context) error {
	var request testPlanItemSetRequest
	if err := c.Bind(&request); err != nil {
		return testPlanErrorResponse(c, domain.ErrInvalidTestPlanInput)
	}
	actor, err := h.authorizeManage(c)
	if err != nil {
		return err
	}
	result, err := h.service.SetTestPlanItem(c.Request().Context(), c.Param("id"), domain.TestPlanItemInput{
		ModuleID:      c.Param("moduleId"),
		Branch:        request.Branch,
		Mode:          domain.TestPlanItemMode(request.Mode),
		BuildRecordID: request.BuildRecordID,
	}, request.ResolveConflicts, actor)
	if err != nil {
		return testPlanErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newTestPlanMutationResponse(result))
}

func (h testPlanHandler) deleteItem(c echo.Context) error {
	actor, err := h.authorizeManage(c)
	if err != nil {
		return err
	}
	detail, err := h.service.DeleteTestPlanItem(c.Request().Context(), c.Param("id"), c.Param("moduleId"), actor)
	if err != nil {
		return testPlanErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newTestPlanDetailResponse(detail))
}

func (h testPlanHandler) addMembers(c echo.Context) error {
	var request testPlanMembersAddRequest
	if err := c.Bind(&request); err != nil {
		return testPlanErrorResponse(c, domain.ErrInvalidTestPlanInput)
	}
	actor, err := h.authorizeManage(c)
	if err != nil {
		return err
	}
	result, err := h.service.AddTestPlanMembers(c.Request().Context(), c.Param("id"), testPlanMemberInputs(request.Members), request.ResolveConflicts, actor)
	if err != nil {
		return testPlanErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newTestPlanMutationResponse(result))
}

func (h testPlanHandler) removeMember(c echo.Context) error {
	actor, err := h.authorizeManage(c)
	if err != nil {
		return err
	}
	detail, err := h.service.RemoveTestPlanMember(c.Request().Context(), c.Param("id"), c.Param("userId"), actor)
	if err != nil {
		return testPlanErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newTestPlanDetailResponse(detail))
}

func (h testPlanHandler) join(c echo.Context) error {
	if h.service == nil {
		return writeErrorResponse(c, http.StatusServiceUnavailable, "test plan service unavailable")
	}
	var request testPlanJoinRequest
	if err := c.Bind(&request); err != nil {
		return testPlanErrorResponse(c, domain.ErrInvalidTestPlanInput)
	}
	owner, ok := currentConsoleUserID(c)
	if !ok {
		return writeErrorResponse(c, http.StatusUnauthorized, "unauthorized")
	}
	result, err := h.service.JoinTestPlan(c.Request().Context(), c.Param("id"), owner, request.UserIDs, request.ResolveConflicts)
	if err != nil {
		return testPlanErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newTestPlanMutationResponse(result))
}

func (h testPlanHandler) leave(c echo.Context) error {
	if h.service == nil {
		return writeErrorResponse(c, http.StatusServiceUnavailable, "test plan service unavailable")
	}
	var request testPlanLeaveRequest
	if err := c.Bind(&request); err != nil {
		return testPlanErrorResponse(c, domain.ErrInvalidTestPlanInput)
	}
	owner, ok := currentConsoleUserID(c)
	if !ok {
		return writeErrorResponse(c, http.StatusUnauthorized, "unauthorized")
	}
	detail, err := h.service.LeaveTestPlan(c.Request().Context(), c.Param("id"), owner, request.UserIDs)
	if err != nil {
		return testPlanErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newTestPlanDetailResponse(detail))
}

func (h testPlanHandler) listTestAccounts(c echo.Context) error {
	if h.service == nil {
		return writeErrorResponse(c, http.StatusServiceUnavailable, "test plan service unavailable")
	}
	owner, ok := currentConsoleUserID(c)
	if !ok {
		return writeErrorResponse(c, http.StatusUnauthorized, "unauthorized")
	}
	accounts, err := h.service.ListTestAccounts(c.Request().Context(), owner, c.QueryParam("environment"))
	if err != nil {
		return testPlanErrorResponse(c, err)
	}
	responses := make([]testAccountResponse, 0, len(accounts))
	for _, account := range accounts {
		responses = append(responses, newTestAccountResponse(account))
	}
	return c.JSON(http.StatusOK, testAccountsResponse{TestAccounts: responses})
}

func (h testPlanHandler) upsertTestAccount(c echo.Context) error {
	if h.service == nil {
		return writeErrorResponse(c, http.StatusServiceUnavailable, "test plan service unavailable")
	}
	var request testAccountRequest
	if err := c.Bind(&request); err != nil {
		return testPlanErrorResponse(c, domain.ErrInvalidTestPlanInput)
	}
	owner, ok := currentConsoleUserID(c)
	if !ok {
		return writeErrorResponse(c, http.StatusUnauthorized, "unauthorized")
	}
	account, err := h.service.UpsertTestAccount(c.Request().Context(), domain.TestAccountInput{
		OwnerUserID: owner,
		Environment: request.Environment,
		UserID:      request.UserID,
		Note:        request.Note,
	})
	if err != nil {
		return testPlanErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, newTestAccountResponse(account))
}

func (h testPlanHandler) deleteTestAccount(c echo.Context) error {
	if h.service == nil {
		return writeErrorResponse(c, http.StatusServiceUnavailable, "test plan service unavailable")
	}
	owner, ok := currentConsoleUserID(c)
	if !ok {
		return writeErrorResponse(c, http.StatusUnauthorized, "unauthorized")
	}
	if err := h.service.DeleteTestAccount(c.Request().Context(), owner, c.Param("environment"), c.Param("userId")); err != nil {
		return testPlanErrorResponse(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// authorizeManage 先读取提测单得到所属应用，再校验 test_plan:manage，返回当前用户 ID。
// 先读后鉴权，是为了让不存在的提测单返回 404，而不是在鉴权时出错。
func (h testPlanHandler) authorizeManage(c echo.Context) (string, error) {
	if h.service == nil {
		return "", writeErrorResponse(c, http.StatusServiceUnavailable, "test plan service unavailable")
	}
	plan, err := h.service.GetTestPlan(c.Request().Context(), c.Param("id"))
	if err != nil {
		return "", testPlanErrorResponse(c, err)
	}
	if err := authorizeCurrentUser(c, h.authService, domain.ActionTestPlanManage, domain.AuthorizationResource{ApplicationID: plan.ApplicationID}); err != nil {
		return "", err
	}
	actor, ok := currentConsoleUserID(c)
	if !ok {
		return "", writeErrorResponse(c, http.StatusUnauthorized, "unauthorized")
	}
	return actor, nil
}

func currentConsoleUserID(c echo.Context) (string, bool) {
	session, ok := consoleSession(c)
	if !ok {
		return "", false
	}
	userID := domain.ConsoleUserIDFromFeishu(session.User)
	return userID, userID != ""
}

func testPlanMemberInputs(requests []testPlanMemberRequest) []domain.TestPlanMemberInput {
	members := make([]domain.TestPlanMemberInput, 0, len(requests))
	for _, member := range requests {
		members = append(members, domain.TestPlanMemberInput{UserID: member.UserID, Note: member.Note})
	}
	return members
}

func testPlanErrorResponse(c echo.Context, err error) error {
	var memberConflict *domain.TestPlanMemberConflictError
	if errors.As(err, &memberConflict) {
		return c.JSON(http.StatusConflict, testPlanMemberConflictErrorResponse{
			Error:     "test plan member conflict",
			Conflicts: newTestPlanMemberConflictResponses(memberConflict.Conflicts),
		})
	}
	var branchInUse *domain.TestPlanBranchInUseError
	if errors.As(err, &branchInUse) {
		return c.JSON(http.StatusConflict, testPlanBranchInUseErrorResponse{
			Error:    "test plan branch in use",
			ModuleID: branchInUse.ModuleID,
			Branch:   branchInUse.Branch,
			TestPlan: testPlanRefResponse{ID: branchInUse.TestPlan.ID, Name: branchInUse.TestPlan.Name},
		})
	}
	switch {
	case errors.Is(err, errResponseWritten):
		return err
	case errors.Is(err, domain.ErrInvalidTestPlanInput):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid test plan request"})
	case errors.Is(err, domain.ErrTestPlanBuildNotFound):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "test plan build not found"})
	case errors.Is(err, domain.ErrTestPlanMemberLimit):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "test plan member limit exceeded"})
	case errors.Is(err, domain.ErrTestPlanClosed):
		return c.JSON(http.StatusConflict, map[string]string{"error": "test plan closed"})
	case errors.Is(err, domain.ErrTestPlansDisabled):
		return c.JSON(http.StatusConflict, map[string]string{"error": "test plans disabled for environment"})
	case errors.Is(err, domain.ErrTestPlanNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "test plan not found"})
	case errors.Is(err, domain.ErrTestPlanItemNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "test plan item not found"})
	case errors.Is(err, domain.ErrTestPlanMemberNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "test plan member not found"})
	case errors.Is(err, domain.ErrTestAccountNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "test account not found"})
	case errors.Is(err, domain.ErrModuleNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "module not found"})
	case errors.Is(err, domain.ErrForbidden):
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func testPlanState(plan domain.TestPlan, now time.Time) string {
	switch {
	case plan.Status == domain.TestPlanStatusClosed:
		return "closed"
	case !plan.ExpiresAt.After(now):
		return "expired"
	default:
		return "open"
	}
}

func newTestPlanResponse(plan domain.TestPlan, now time.Time) testPlanResponse {
	return testPlanResponse{
		ID:              plan.ID,
		ApplicationID:   plan.ApplicationID,
		ApplicationName: plan.ApplicationName,
		ApplicationSlug: plan.ApplicationSlug,
		Environment:     plan.Environment,
		EnvironmentName: plan.EnvironmentName,
		Name:            plan.Name,
		Link:            plan.Link,
		Status:          testPlanState(plan, now),
		ExpiresAt:       formatTime(plan.ExpiresAt),
		CreatedByUserID: plan.CreatedByUserID,
		CreatedBy:       plan.CreatedBy,
		CreatedAt:       formatTime(plan.CreatedAt),
		UpdatedAt:       formatTime(plan.UpdatedAt),
		ClosedAt:        formatOptionalTime(plan.ClosedAt),
		ItemCount:       plan.ItemCount,
		MemberCount:     plan.MemberCount,
	}
}

func newTestPlanDetailResponse(detail domain.TestPlanDetail) testPlanDetailResponse {
	response := testPlanDetailResponse{
		TestPlan: newTestPlanResponse(detail.Plan, time.Now()),
		Items:    make([]testPlanItemResponse, 0, len(detail.Items)),
		Members:  make([]testPlanMemberResponse, 0, len(detail.Members)),
		Events:   make([]testPlanEventResponse, 0, len(detail.Events)),
	}
	hasCommon, hasFeature := false, false
	for _, item := range detail.Items {
		hasCommon = hasCommon || item.ModuleKind == domain.ModuleKindCommon
		hasFeature = hasFeature || item.ModuleKind == domain.ModuleKindFeature
		platforms := item.Platforms
		if platforms == nil {
			platforms = []string{}
		}
		response.Items = append(response.Items, testPlanItemResponse{
			ModuleID:       item.ModuleID,
			ModuleName:     item.ModuleName,
			ModuleKind:     string(item.ModuleKind),
			Branch:         item.Branch,
			Mode:           string(item.Mode),
			BuildRecordID:  item.BuildRecordID,
			BuildNumber:    item.BuildNumber,
			CommitSHA:      item.CommitSHA,
			RuntimeVersion: item.RuntimeVersion,
			Platforms:      platforms,
			UpdatedBy:      item.UpdatedBy,
			UpdatedAt:      formatTime(item.UpdatedAt),
		})
	}
	response.CommonDependencyWarning = hasCommon && hasFeature
	for _, member := range detail.Members {
		response.Members = append(response.Members, testPlanMemberResponse{
			UserID:    member.UserID,
			Note:      member.Note,
			AddedBy:   member.AddedBy,
			CreatedAt: formatTime(member.CreatedAt),
		})
	}
	for _, event := range detail.Events {
		eventDetail := event.Detail
		if eventDetail == nil {
			eventDetail = map[string]any{}
		}
		response.Events = append(response.Events, testPlanEventResponse{
			ID:        event.ID,
			Kind:      string(event.Kind),
			Detail:    eventDetail,
			Actor:     event.Actor,
			CreatedAt: formatTime(event.CreatedAt),
		})
	}
	return response
}

func newTestPlanMutationResponse(result domain.TestPlanMutationResult) testPlanMutationResponse {
	return testPlanMutationResponse{
		testPlanDetailResponse: newTestPlanDetailResponse(result.Detail),
		Removed:                newTestPlanMemberConflictResponses(result.Removed),
	}
}

func newTestPlanMemberConflictResponses(conflicts []domain.TestPlanMemberConflict) []testPlanMemberConflictResponse {
	responses := make([]testPlanMemberConflictResponse, 0, len(conflicts))
	for _, conflict := range conflicts {
		moduleIDs := conflict.ModuleIDs
		if moduleIDs == nil {
			moduleIDs = []string{}
		}
		responses = append(responses, testPlanMemberConflictResponse{
			UserID:    conflict.UserID,
			TestPlan:  testPlanRefResponse{ID: conflict.TestPlan.ID, Name: conflict.TestPlan.Name},
			ModuleIDs: moduleIDs,
		})
	}
	return responses
}

func newTestAccountResponse(account domain.TestAccount) testAccountResponse {
	return testAccountResponse{
		Environment: account.Environment,
		UserID:      account.UserID,
		Note:        account.Note,
		CreatedAt:   formatTime(account.CreatedAt),
	}
}
