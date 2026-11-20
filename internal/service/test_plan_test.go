package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"forge-server/internal/domain"
)

const testPlanTestID = "018f4d7a-0a6c-789b-9c12-36a4dfd9e001"

func TestTestPlanServiceCreateNormalizesAndDefaults(t *testing.T) {
	repository := &fakeTestPlanRepository{}
	service := NewTestPlanService(repository)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	_, err := service.CreateTestPlan(context.Background(), domain.TestPlanCreateInput{
		ApplicationID: " app-1 ",
		Environment:   " staging ",
		Name:          " 登录改版 ",
		Items:         []domain.TestPlanItemInput{{ModuleID: "claw", Branch: " feature/login "}},
		Members:       []domain.TestPlanMemberInput{{UserID: " 1001 ", Note: " 张三 "}, {UserID: "1001"}},
		ActorUserID:   "user-1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	input := repository.lastCreate
	if input.ApplicationID != "app-1" || input.Environment != "staging" || input.Name != "登录改版" {
		t.Fatalf("unexpected normalized plan: %+v", input)
	}
	if input.ExpiresAt == nil || !input.ExpiresAt.Equal(now.Add(domain.DefaultTestPlanTTL)) {
		t.Fatalf("expected default 7 day expiry, got %v", input.ExpiresAt)
	}
	if len(input.Items) != 1 || input.Items[0].Branch != "feature/login" || input.Items[0].Mode != domain.TestPlanItemModeFollow {
		t.Fatalf("expected follow mode by default, got %+v", input.Items)
	}
	if len(input.Members) != 1 || input.Members[0].UserID != "1001" || input.Members[0].Note != "张三" {
		t.Fatalf("expected deduplicated members, got %+v", input.Members)
	}
}

func TestTestPlanServiceRejectsInvalidInput(t *testing.T) {
	now := time.Now()
	tooLate := now.Add(domain.MaxTestPlanTTL + time.Hour)
	past := now.Add(-time.Minute)
	base := func() domain.TestPlanCreateInput {
		return domain.TestPlanCreateInput{ApplicationID: "app-1", Environment: "staging", Name: "A", ActorUserID: "user-1"}
	}
	cases := map[string]func(*domain.TestPlanCreateInput){
		"empty name":         func(in *domain.TestPlanCreateInput) { in.Name = " " },
		"bad link":           func(in *domain.TestPlanCreateInput) { in.Link = "javascript:alert(1)" },
		"expiry too late":    func(in *domain.TestPlanCreateInput) { in.ExpiresAt = &tooLate },
		"expiry in the past": func(in *domain.TestPlanCreateInput) { in.ExpiresAt = &past },
		"pinned without build": func(in *domain.TestPlanCreateInput) {
			in.Items = []domain.TestPlanItemInput{{ModuleID: "claw", Branch: "a", Mode: domain.TestPlanItemModePinned}}
		},
		"missing branch": func(in *domain.TestPlanCreateInput) { in.Items = []domain.TestPlanItemInput{{ModuleID: "claw"}} },
		"duplicate module": func(in *domain.TestPlanCreateInput) {
			in.Items = []domain.TestPlanItemInput{{ModuleID: "claw", Branch: "a"}, {ModuleID: "claw", Branch: "b"}}
		},
		"invalid user id": func(in *domain.TestPlanCreateInput) { in.Members = []domain.TestPlanMemberInput{{UserID: "has space"}} },
		"unknown mode": func(in *domain.TestPlanCreateInput) {
			in.Items = []domain.TestPlanItemInput{{ModuleID: "claw", Branch: "a", Mode: "latest"}}
		},
		"missing actor":       func(in *domain.TestPlanCreateInput) { in.ActorUserID = "" },
		"missing application": func(in *domain.TestPlanCreateInput) { in.ApplicationID = "" },
		"build id is not a uuid": func(in *domain.TestPlanCreateInput) {
			in.Items = []domain.TestPlanItemInput{{ModuleID: "claw", Branch: "a", Mode: domain.TestPlanItemModePinned, BuildRecordID: "build-1"}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			repository := &fakeTestPlanRepository{}
			input := base()
			mutate(&input)
			if _, err := NewTestPlanService(repository).CreateTestPlan(context.Background(), input); !errors.Is(err, domain.ErrInvalidTestPlanInput) {
				t.Fatalf("expected invalid input, got %v", err)
			}
			if repository.createCalls != 0 {
				t.Fatal("expected repository not to be called")
			}
		})
	}
}

func TestTestPlanServiceRejectsTooManyMembers(t *testing.T) {
	members := make([]domain.TestPlanMemberInput, domain.MaxTestPlanMembers+1)
	for index := range members {
		members[index] = domain.TestPlanMemberInput{UserID: "u" + time.Duration(index).String()}
	}
	_, err := NewTestPlanService(&fakeTestPlanRepository{}).AddTestPlanMembers(context.Background(), testPlanTestID, members, false, "user-1")
	if !errors.Is(err, domain.ErrTestPlanMemberLimit) {
		t.Fatalf("expected member limit error, got %v", err)
	}
}

// 加入只能用自己保存在提测单环境下的测试账号。
func TestTestPlanServiceJoinUsesOwnTestAccounts(t *testing.T) {
	repository := &fakeTestPlanRepository{
		plan: domain.TestPlan{ID: testPlanTestID, Environment: "staging"},
		accounts: []domain.TestAccount{
			{OwnerUserID: "user-qa", Environment: "staging", UserID: "1001", Note: "测试号 1"},
			{OwnerUserID: "user-qa", Environment: "staging", UserID: "1002"},
		},
	}
	service := NewTestPlanService(repository)

	if _, err := service.JoinTestPlan(context.Background(), testPlanTestID, "user-qa", nil, true); err != nil {
		t.Fatalf("join all: %v", err)
	}
	if len(repository.lastMembers) != 2 || repository.lastMembers[0].Note != "测试号 1" || !repository.lastResolve {
		t.Fatalf("expected all own accounts with notes and resolve flag, got %+v resolve=%v", repository.lastMembers, repository.lastResolve)
	}
	if repository.lastAccountsEnvironment != "staging" {
		t.Fatalf("expected accounts of the plan environment, got %q", repository.lastAccountsEnvironment)
	}

	if _, err := service.JoinTestPlan(context.Background(), testPlanTestID, "user-qa", []string{"1002"}, false); err != nil {
		t.Fatalf("join one: %v", err)
	}
	if len(repository.lastMembers) != 1 || repository.lastMembers[0].UserID != "1002" {
		t.Fatalf("expected only the selected account, got %+v", repository.lastMembers)
	}

	if _, err := service.JoinTestPlan(context.Background(), testPlanTestID, "user-qa", []string{"9999"}, false); !errors.Is(err, domain.ErrTestAccountNotFound) {
		t.Fatalf("expected someone else's user id to be rejected, got %v", err)
	}
}

func TestTestPlanServiceExtendValidatesExpiry(t *testing.T) {
	repository := &fakeTestPlanRepository{}
	service := NewTestPlanService(repository)
	if _, err := service.ExtendTestPlan(context.Background(), testPlanTestID, domain.TestPlanExtendInput{
		ExpiresAt:   time.Now().Add(domain.MaxTestPlanTTL + time.Hour),
		ActorUserID: "user-1",
	}); !errors.Is(err, domain.ErrInvalidTestPlanInput) {
		t.Fatalf("expected expiry beyond 30 days to be rejected, got %v", err)
	}
	if _, err := service.ExtendTestPlan(context.Background(), testPlanTestID, domain.TestPlanExtendInput{
		ExpiresAt:   time.Now().Add(48 * time.Hour),
		ActorUserID: "user-1",
	}); err != nil {
		t.Fatalf("extend: %v", err)
	}
}

func TestTestPlanServiceRejectsMalformedID(t *testing.T) {
	if _, err := NewTestPlanService(&fakeTestPlanRepository{}).GetTestPlanDetail(context.Background(), "not-a-uuid"); !errors.Is(err, domain.ErrTestPlanNotFound) {
		t.Fatalf("expected not found for a malformed id, got %v", err)
	}
}

type fakeTestPlanRepository struct {
	plan                    domain.TestPlan
	accounts                []domain.TestAccount
	createCalls             int
	lastCreate              domain.TestPlanCreateInput
	lastMembers             []domain.TestPlanMemberInput
	lastResolve             bool
	lastAccountsEnvironment string
}

func (r *fakeTestPlanRepository) ListTestPlans(context.Context, domain.TestPlanFilters) ([]domain.TestPlan, int, error) {
	return nil, 0, nil
}

func (r *fakeTestPlanRepository) GetTestPlan(context.Context, string) (domain.TestPlan, error) {
	return r.plan, nil
}

func (r *fakeTestPlanRepository) GetTestPlanDetail(context.Context, string) (domain.TestPlanDetail, error) {
	return domain.TestPlanDetail{Plan: r.plan}, nil
}

func (r *fakeTestPlanRepository) CreateTestPlan(_ context.Context, input domain.TestPlanCreateInput) (domain.TestPlanMutationResult, error) {
	r.createCalls++
	r.lastCreate = input
	return domain.TestPlanMutationResult{}, nil
}

func (r *fakeTestPlanRepository) UpdateTestPlan(context.Context, string, domain.TestPlanUpdateInput) (domain.TestPlanDetail, error) {
	return domain.TestPlanDetail{}, nil
}

func (r *fakeTestPlanRepository) CloseTestPlan(context.Context, string, string) (domain.TestPlanDetail, error) {
	return domain.TestPlanDetail{}, nil
}

func (r *fakeTestPlanRepository) ExtendTestPlan(context.Context, string, domain.TestPlanExtendInput) (domain.TestPlanDetail, error) {
	return domain.TestPlanDetail{}, nil
}

func (r *fakeTestPlanRepository) SetTestPlanItem(context.Context, string, domain.TestPlanItemInput, bool, string) (domain.TestPlanMutationResult, error) {
	return domain.TestPlanMutationResult{}, nil
}

func (r *fakeTestPlanRepository) DeleteTestPlanItem(context.Context, string, string, string) (domain.TestPlanDetail, error) {
	return domain.TestPlanDetail{}, nil
}

func (r *fakeTestPlanRepository) AddTestPlanMembers(_ context.Context, _ string, members []domain.TestPlanMemberInput, resolve bool, _ string) (domain.TestPlanMutationResult, error) {
	r.lastMembers = members
	r.lastResolve = resolve
	return domain.TestPlanMutationResult{}, nil
}

func (r *fakeTestPlanRepository) RemoveTestPlanMembers(context.Context, string, []string, string) (domain.TestPlanDetail, error) {
	return domain.TestPlanDetail{}, nil
}

func (r *fakeTestPlanRepository) ListTestAccounts(_ context.Context, _ string, environment string) ([]domain.TestAccount, error) {
	r.lastAccountsEnvironment = environment
	return r.accounts, nil
}

func (r *fakeTestPlanRepository) UpsertTestAccount(_ context.Context, input domain.TestAccountInput) (domain.TestAccount, error) {
	return domain.TestAccount{OwnerUserID: input.OwnerUserID, Environment: input.Environment, UserID: input.UserID, Note: input.Note}, nil
}

func (r *fakeTestPlanRepository) DeleteTestAccount(context.Context, string, string, string) error {
	return nil
}
