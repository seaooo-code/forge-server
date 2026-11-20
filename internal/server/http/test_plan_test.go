package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forge-server/internal/domain"
)

const httpTestPlanID = "018f4d7a-0a6c-789b-9c12-36a4dfd9e001"

func TestCreateTestPlanRequiresManagePermission(t *testing.T) {
	service := &fakeTestPlanService{}
	e := newAuthorizedTestServer(Dependencies{
		AuthorizationService: fakeAuthorizationService{err: domain.ErrForbidden},
		TestPlanService:      service,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/test-plans", jsonBody(map[string]any{
		"applicationId": "app-1", "environment": "staging", "name": "A",
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden || service.createCalls != 0 {
		t.Fatalf("expected 403 without calling the service, got %d calls=%d", rec.Code, service.createCalls)
	}
}

func TestCreateTestPlanBindsRequest(t *testing.T) {
	service := &fakeTestPlanService{}
	e := newAuthorizedTestServer(Dependencies{TestPlanService: service})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/test-plans", jsonBody(map[string]any{
		"applicationId":    "app-1",
		"environment":      "staging",
		"name":             "登录改版",
		"expiresAt":        "2026-11-27T10:00:00Z",
		"items":            []map[string]any{{"moduleId": "claw", "branch": "feature/login", "mode": "pinned", "buildRecordId": "b-1"}},
		"members":          []map[string]any{{"userId": "1001", "note": "张三"}},
		"resolveConflicts": true,
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	input := service.lastCreate
	if input.ApplicationID != "app-1" || input.ActorUserID == "" || !input.ResolveConflicts ||
		input.ExpiresAt == nil || !input.ExpiresAt.Equal(time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)) ||
		len(input.Items) != 1 || input.Items[0].Mode != domain.TestPlanItemModePinned || input.Items[0].BuildRecordID != "b-1" ||
		len(input.Members) != 1 || input.Members[0].Note != "张三" {
		t.Fatalf("unexpected create input: %+v", input)
	}
}

func TestTestPlanMemberConflictResponse(t *testing.T) {
	service := &fakeTestPlanService{
		plan: domain.TestPlan{ID: httpTestPlanID, ApplicationID: "app-1"},
		err: &domain.TestPlanMemberConflictError{Conflicts: []domain.TestPlanMemberConflict{{
			UserID:    "1001",
			TestPlan:  domain.TestPlanRef{ID: "plan-b", Name: "支付改版"},
			ModuleIDs: []string{"claw"},
		}}},
	}
	e := newAuthorizedTestServer(Dependencies{TestPlanService: service})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/test-plans/"+httpTestPlanID+"/members", jsonBody(map[string]any{
		"members": []map[string]any{{"userId": "1001"}},
	}))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var response testPlanMemberConflictErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.Error != "test plan member conflict" || len(response.Conflicts) != 1 ||
		response.Conflicts[0].TestPlan.Name != "支付改版" || response.Conflicts[0].ModuleIDs[0] != "claw" {
		t.Fatalf("unexpected conflict response: %+v", response)
	}
}

func TestTestPlanBranchInUseResponse(t *testing.T) {
	service := &fakeTestPlanService{
		plan: domain.TestPlan{ID: httpTestPlanID, ApplicationID: "app-1"},
		err:  &domain.TestPlanBranchInUseError{ModuleID: "claw", Branch: "feature/a", TestPlan: domain.TestPlanRef{ID: "plan-b", Name: "B"}},
	}
	e := newAuthorizedTestServer(Dependencies{TestPlanService: service})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/test-plans/"+httpTestPlanID+"/items/claw", jsonBody(map[string]any{"branch": "feature/a"}))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var response testPlanBranchInUseErrorResponse
	_ = json.NewDecoder(rec.Body).Decode(&response)
	if rec.Code != http.StatusConflict || response.Error != "test plan branch in use" || response.TestPlan.ID != "plan-b" || response.Branch != "feature/a" {
		t.Fatalf("unexpected branch in use response: %d %+v", rec.Code, response)
	}
	if service.lastItem.ModuleID != "claw" || service.lastItem.Branch != "feature/a" {
		t.Fatalf("expected path module and body branch to be bound, got %+v", service.lastItem)
	}
}

// 加入、退出不需要 test_plan:manage，只要求登录，且用当前登录用户作为测试账号的所有者。
func TestJoinTestPlanWithoutManagePermission(t *testing.T) {
	service := &fakeTestPlanService{}
	e := newAuthorizedTestServer(Dependencies{
		AuthorizationService: fakeAuthorizationService{err: domain.ErrForbidden},
		TestPlanService:      service,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/test-plans/"+httpTestPlanID+"/join", jsonBody(map[string]any{"userIds": []string{"1001"}}))
	req.Header.Set("Content-Type", "application/json")
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected join to succeed, got %d: %s", rec.Code, rec.Body.String())
	}
	if service.lastJoinOwner == "" || len(service.lastJoinUserIDs) != 1 {
		t.Fatalf("expected owner and user ids to be passed, got %q %v", service.lastJoinOwner, service.lastJoinUserIDs)
	}
}

func TestGetTestPlanDetailMarksCommonDependency(t *testing.T) {
	service := &fakeTestPlanService{detail: domain.TestPlanDetail{
		Plan: domain.TestPlan{ID: httpTestPlanID, Status: domain.TestPlanStatusOpen, ExpiresAt: time.Now().Add(-time.Minute)},
		Items: []domain.TestPlanItem{
			{ModuleID: "common", ModuleKind: domain.ModuleKindCommon},
			{ModuleID: "claw", ModuleKind: domain.ModuleKindFeature},
		},
	}}
	e := newAuthorizedTestServer(Dependencies{TestPlanService: service})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test-plans/"+httpTestPlanID, nil)
	addTestConsoleSession(req)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var response testPlanDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !response.CommonDependencyWarning || response.TestPlan.Status != "expired" {
		t.Fatalf("expected dependency warning and expired status, got %+v", response)
	}
}

type fakeTestPlanService struct {
	plan            domain.TestPlan
	detail          domain.TestPlanDetail
	err             error
	createCalls     int
	lastCreate      domain.TestPlanCreateInput
	lastItem        domain.TestPlanItemInput
	lastJoinOwner   string
	lastJoinUserIDs []string
}

func (s *fakeTestPlanService) ListTestPlans(context.Context, domain.TestPlanFilters) ([]domain.TestPlan, int, error) {
	return []domain.TestPlan{s.plan}, 1, s.err
}

func (s *fakeTestPlanService) GetTestPlan(context.Context, string) (domain.TestPlan, error) {
	return s.plan, nil
}

func (s *fakeTestPlanService) GetTestPlanDetail(context.Context, string) (domain.TestPlanDetail, error) {
	return s.detail, s.err
}

func (s *fakeTestPlanService) CreateTestPlan(_ context.Context, input domain.TestPlanCreateInput) (domain.TestPlanMutationResult, error) {
	s.createCalls++
	s.lastCreate = input
	return domain.TestPlanMutationResult{Detail: s.detail}, s.err
}

func (s *fakeTestPlanService) UpdateTestPlan(context.Context, string, domain.TestPlanUpdateInput) (domain.TestPlanDetail, error) {
	return s.detail, s.err
}

func (s *fakeTestPlanService) CloseTestPlan(context.Context, string, string) (domain.TestPlanDetail, error) {
	return s.detail, s.err
}

func (s *fakeTestPlanService) ExtendTestPlan(context.Context, string, domain.TestPlanExtendInput) (domain.TestPlanDetail, error) {
	return s.detail, s.err
}

func (s *fakeTestPlanService) SetTestPlanItem(_ context.Context, _ string, item domain.TestPlanItemInput, _ bool, _ string) (domain.TestPlanMutationResult, error) {
	s.lastItem = item
	return domain.TestPlanMutationResult{Detail: s.detail}, s.err
}

func (s *fakeTestPlanService) DeleteTestPlanItem(context.Context, string, string, string) (domain.TestPlanDetail, error) {
	return s.detail, s.err
}

func (s *fakeTestPlanService) AddTestPlanMembers(context.Context, string, []domain.TestPlanMemberInput, bool, string) (domain.TestPlanMutationResult, error) {
	return domain.TestPlanMutationResult{Detail: s.detail}, s.err
}

func (s *fakeTestPlanService) RemoveTestPlanMember(context.Context, string, string, string) (domain.TestPlanDetail, error) {
	return s.detail, s.err
}

func (s *fakeTestPlanService) JoinTestPlan(_ context.Context, _ string, owner string, userIDs []string, _ bool) (domain.TestPlanMutationResult, error) {
	s.lastJoinOwner = owner
	s.lastJoinUserIDs = userIDs
	return domain.TestPlanMutationResult{Detail: s.detail}, s.err
}

func (s *fakeTestPlanService) LeaveTestPlan(context.Context, string, string, []string) (domain.TestPlanDetail, error) {
	return s.detail, s.err
}

func (s *fakeTestPlanService) ListTestAccounts(context.Context, string, string) ([]domain.TestAccount, error) {
	return nil, s.err
}

func (s *fakeTestPlanService) UpsertTestAccount(_ context.Context, input domain.TestAccountInput) (domain.TestAccount, error) {
	return domain.TestAccount{Environment: input.Environment, UserID: input.UserID}, s.err
}

func (s *fakeTestPlanService) DeleteTestAccount(context.Context, string, string, string) error {
	return s.err
}
