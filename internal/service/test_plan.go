package service

import (
	"context"
	"slices"
	"strings"
	"time"

	"forge-server/internal/domain"

	"github.com/google/uuid"
)

type TestPlanRepository interface {
	ListTestPlans(ctx context.Context, filters domain.TestPlanFilters) ([]domain.TestPlan, int, error)
	GetTestPlan(ctx context.Context, id string) (domain.TestPlan, error)
	GetTestPlanDetail(ctx context.Context, id string) (domain.TestPlanDetail, error)
	CreateTestPlan(ctx context.Context, input domain.TestPlanCreateInput) (domain.TestPlanMutationResult, error)
	UpdateTestPlan(ctx context.Context, id string, input domain.TestPlanUpdateInput) (domain.TestPlanDetail, error)
	CloseTestPlan(ctx context.Context, id string, actorUserID string) (domain.TestPlanDetail, error)
	ExtendTestPlan(ctx context.Context, id string, input domain.TestPlanExtendInput) (domain.TestPlanDetail, error)
	SetTestPlanItem(ctx context.Context, id string, item domain.TestPlanItemInput, resolveConflicts bool, actorUserID string) (domain.TestPlanMutationResult, error)
	DeleteTestPlanItem(ctx context.Context, id string, moduleID string, actorUserID string) (domain.TestPlanDetail, error)
	AddTestPlanMembers(ctx context.Context, id string, members []domain.TestPlanMemberInput, resolveConflicts bool, actorUserID string) (domain.TestPlanMutationResult, error)
	RemoveTestPlanMembers(ctx context.Context, id string, userIDs []string, actorUserID string) (domain.TestPlanDetail, error)
	ListTestAccounts(ctx context.Context, ownerUserID string, environment string) ([]domain.TestAccount, error)
	UpsertTestAccount(ctx context.Context, input domain.TestAccountInput) (domain.TestAccount, error)
	DeleteTestAccount(ctx context.Context, ownerUserID string, environment string, userID string) error
}

// TestPlanService 校验提测单的输入，冲突规则和事务由仓储保证，见 docs/test-plans.md。
// 权限（谁能管理、谁能加入）在 HTTP 层按 test_plan:manage 和当前登录用户判断。
type TestPlanService struct {
	repository TestPlanRepository
	now        func() time.Time
}

func NewTestPlanService(repository TestPlanRepository) *TestPlanService {
	return &TestPlanService{repository: repository, now: time.Now}
}

func (s *TestPlanService) ListTestPlans(ctx context.Context, filters domain.TestPlanFilters) ([]domain.TestPlan, int, error) {
	normalized := domain.TestPlanFilters{
		ApplicationID: strings.TrimSpace(filters.ApplicationID),
		Environment:   strings.TrimSpace(filters.Environment),
		Status:        domain.TestPlanStatus(strings.TrimSpace(string(filters.Status))),
		MemberUserID:  strings.TrimSpace(filters.MemberUserID),
		Pagination:    filters.Pagination,
	}
	if !normalized.Pagination.IsValid() ||
		normalized.Status != "" && !domain.IsValidTestPlanStatus(normalized.Status) ||
		normalized.MemberUserID != "" && !domain.IsValidTestPlanUserID(normalized.MemberUserID) {
		return nil, 0, domain.ErrInvalidTestPlanInput
	}
	return s.repository.ListTestPlans(ctx, normalized)
}

func (s *TestPlanService) GetTestPlan(ctx context.Context, id string) (domain.TestPlan, error) {
	normalizedID, err := normalizeTestPlanID(id)
	if err != nil {
		return domain.TestPlan{}, err
	}
	return s.repository.GetTestPlan(ctx, normalizedID)
}

func (s *TestPlanService) GetTestPlanDetail(ctx context.Context, id string) (domain.TestPlanDetail, error) {
	normalizedID, err := normalizeTestPlanID(id)
	if err != nil {
		return domain.TestPlanDetail{}, err
	}
	return s.repository.GetTestPlanDetail(ctx, normalizedID)
}

func (s *TestPlanService) CreateTestPlan(ctx context.Context, input domain.TestPlanCreateInput) (domain.TestPlanMutationResult, error) {
	now := s.now()
	expiresAt := now.Add(domain.DefaultTestPlanTTL)
	if input.ExpiresAt != nil {
		expiresAt = *input.ExpiresAt
	}
	if err := s.validateExpiresAt(expiresAt, now); err != nil {
		return domain.TestPlanMutationResult{}, err
	}
	normalized := domain.TestPlanCreateInput{
		ApplicationID:    strings.TrimSpace(input.ApplicationID),
		Environment:      strings.TrimSpace(input.Environment),
		Name:             strings.TrimSpace(input.Name),
		Link:             strings.TrimSpace(input.Link),
		ExpiresAt:        &expiresAt,
		ResolveConflicts: input.ResolveConflicts,
		ActorUserID:      strings.TrimSpace(input.ActorUserID),
	}
	if normalized.ApplicationID == "" ||
		normalized.Environment == "" ||
		normalized.ActorUserID == "" ||
		!domain.IsValidTestPlanName(normalized.Name) ||
		!domain.IsValidTestPlanLink(normalized.Link) {
		return domain.TestPlanMutationResult{}, domain.ErrInvalidTestPlanInput
	}

	moduleIDs := map[string]bool{}
	for _, item := range input.Items {
		normalizedItem, err := normalizeTestPlanItemInput(item)
		if err != nil {
			return domain.TestPlanMutationResult{}, err
		}
		if moduleIDs[normalizedItem.ModuleID] {
			return domain.TestPlanMutationResult{}, domain.ErrInvalidTestPlanInput
		}
		moduleIDs[normalizedItem.ModuleID] = true
		normalized.Items = append(normalized.Items, normalizedItem)
	}
	members, err := normalizeTestPlanMembers(input.Members)
	if err != nil {
		return domain.TestPlanMutationResult{}, err
	}
	normalized.Members = members
	return s.repository.CreateTestPlan(ctx, normalized)
}

func (s *TestPlanService) UpdateTestPlan(ctx context.Context, id string, input domain.TestPlanUpdateInput) (domain.TestPlanDetail, error) {
	normalizedID, err := normalizeTestPlanID(id)
	if err != nil {
		return domain.TestPlanDetail{}, err
	}
	normalized := domain.TestPlanUpdateInput{ActorUserID: strings.TrimSpace(input.ActorUserID)}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if !domain.IsValidTestPlanName(name) {
			return domain.TestPlanDetail{}, domain.ErrInvalidTestPlanInput
		}
		normalized.Name = &name
	}
	if input.Link != nil {
		link := strings.TrimSpace(*input.Link)
		if !domain.IsValidTestPlanLink(link) {
			return domain.TestPlanDetail{}, domain.ErrInvalidTestPlanInput
		}
		normalized.Link = &link
	}
	if normalized.ActorUserID == "" || normalized.Name == nil && normalized.Link == nil {
		return domain.TestPlanDetail{}, domain.ErrInvalidTestPlanInput
	}
	return s.repository.UpdateTestPlan(ctx, normalizedID, normalized)
}

func (s *TestPlanService) CloseTestPlan(ctx context.Context, id string, actorUserID string) (domain.TestPlanDetail, error) {
	normalizedID, err := normalizeTestPlanID(id)
	if err != nil {
		return domain.TestPlanDetail{}, err
	}
	actor := strings.TrimSpace(actorUserID)
	if actor == "" {
		return domain.TestPlanDetail{}, domain.ErrInvalidTestPlanInput
	}
	return s.repository.CloseTestPlan(ctx, normalizedID, actor)
}

func (s *TestPlanService) ExtendTestPlan(ctx context.Context, id string, input domain.TestPlanExtendInput) (domain.TestPlanDetail, error) {
	normalizedID, err := normalizeTestPlanID(id)
	if err != nil {
		return domain.TestPlanDetail{}, err
	}
	if err := s.validateExpiresAt(input.ExpiresAt, s.now()); err != nil {
		return domain.TestPlanDetail{}, err
	}
	actor := strings.TrimSpace(input.ActorUserID)
	if actor == "" {
		return domain.TestPlanDetail{}, domain.ErrInvalidTestPlanInput
	}
	return s.repository.ExtendTestPlan(ctx, normalizedID, domain.TestPlanExtendInput{ExpiresAt: input.ExpiresAt, ActorUserID: actor})
}

func (s *TestPlanService) SetTestPlanItem(ctx context.Context, id string, item domain.TestPlanItemInput, resolveConflicts bool, actorUserID string) (domain.TestPlanMutationResult, error) {
	normalizedID, err := normalizeTestPlanID(id)
	if err != nil {
		return domain.TestPlanMutationResult{}, err
	}
	normalizedItem, err := normalizeTestPlanItemInput(item)
	if err != nil {
		return domain.TestPlanMutationResult{}, err
	}
	actor := strings.TrimSpace(actorUserID)
	if actor == "" {
		return domain.TestPlanMutationResult{}, domain.ErrInvalidTestPlanInput
	}
	return s.repository.SetTestPlanItem(ctx, normalizedID, normalizedItem, resolveConflicts, actor)
}

func (s *TestPlanService) DeleteTestPlanItem(ctx context.Context, id string, moduleID string, actorUserID string) (domain.TestPlanDetail, error) {
	normalizedID, err := normalizeTestPlanID(id)
	if err != nil {
		return domain.TestPlanDetail{}, err
	}
	normalizedModuleID := strings.TrimSpace(moduleID)
	actor := strings.TrimSpace(actorUserID)
	if !domain.IsValidModuleID(normalizedModuleID) || actor == "" {
		return domain.TestPlanDetail{}, domain.ErrInvalidTestPlanInput
	}
	return s.repository.DeleteTestPlanItem(ctx, normalizedID, normalizedModuleID, actor)
}

func (s *TestPlanService) AddTestPlanMembers(ctx context.Context, id string, members []domain.TestPlanMemberInput, resolveConflicts bool, actorUserID string) (domain.TestPlanMutationResult, error) {
	normalizedID, err := normalizeTestPlanID(id)
	if err != nil {
		return domain.TestPlanMutationResult{}, err
	}
	normalized, err := normalizeTestPlanMembers(members)
	if err != nil {
		return domain.TestPlanMutationResult{}, err
	}
	actor := strings.TrimSpace(actorUserID)
	if len(normalized) == 0 || actor == "" {
		return domain.TestPlanMutationResult{}, domain.ErrInvalidTestPlanInput
	}
	return s.repository.AddTestPlanMembers(ctx, normalizedID, normalized, resolveConflicts, actor)
}

func (s *TestPlanService) RemoveTestPlanMember(ctx context.Context, id string, userID string, actorUserID string) (domain.TestPlanDetail, error) {
	normalizedID, err := normalizeTestPlanID(id)
	if err != nil {
		return domain.TestPlanDetail{}, err
	}
	normalizedUserID := strings.TrimSpace(userID)
	actor := strings.TrimSpace(actorUserID)
	if !domain.IsValidTestPlanUserID(normalizedUserID) || actor == "" {
		return domain.TestPlanDetail{}, domain.ErrInvalidTestPlanInput
	}
	return s.repository.RemoveTestPlanMembers(ctx, normalizedID, []string{normalizedUserID}, actor)
}

// JoinTestPlan 用当前用户保存在提测单环境下的测试账号加入提测单。userIDs 为空时加入全部测试账号；
// 指定了 userIDs 时，只能是自己的测试账号。
func (s *TestPlanService) JoinTestPlan(ctx context.Context, id string, ownerUserID string, userIDs []string, resolveConflicts bool) (domain.TestPlanMutationResult, error) {
	plan, accounts, err := s.ownedAccountsForPlan(ctx, id, ownerUserID, userIDs)
	if err != nil {
		return domain.TestPlanMutationResult{}, err
	}
	members := make([]domain.TestPlanMemberInput, 0, len(accounts))
	for _, account := range accounts {
		members = append(members, domain.TestPlanMemberInput{UserID: account.UserID, Note: account.Note})
	}
	return s.repository.AddTestPlanMembers(ctx, plan.ID, members, resolveConflicts, strings.TrimSpace(ownerUserID))
}

// LeaveTestPlan 把当前用户的测试账号从提测单移出。userIDs 为空时移出全部。
func (s *TestPlanService) LeaveTestPlan(ctx context.Context, id string, ownerUserID string, userIDs []string) (domain.TestPlanDetail, error) {
	plan, accounts, err := s.ownedAccountsForPlan(ctx, id, ownerUserID, userIDs)
	if err != nil {
		return domain.TestPlanDetail{}, err
	}
	ids := make([]string, 0, len(accounts))
	for _, account := range accounts {
		ids = append(ids, account.UserID)
	}
	return s.repository.RemoveTestPlanMembers(ctx, plan.ID, ids, strings.TrimSpace(ownerUserID))
}

func (s *TestPlanService) ownedAccountsForPlan(ctx context.Context, id string, ownerUserID string, userIDs []string) (domain.TestPlan, []domain.TestAccount, error) {
	owner := strings.TrimSpace(ownerUserID)
	if owner == "" {
		return domain.TestPlan{}, nil, domain.ErrInvalidTestPlanInput
	}
	requested := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		normalized := strings.TrimSpace(userID)
		if !domain.IsValidTestPlanUserID(normalized) {
			return domain.TestPlan{}, nil, domain.ErrInvalidTestPlanInput
		}
		requested = append(requested, normalized)
	}
	plan, err := s.GetTestPlan(ctx, id)
	if err != nil {
		return domain.TestPlan{}, nil, err
	}
	accounts, err := s.repository.ListTestAccounts(ctx, owner, plan.Environment)
	if err != nil {
		return domain.TestPlan{}, nil, err
	}
	selected := []domain.TestAccount{}
	for _, account := range accounts {
		if len(requested) == 0 || slices.Contains(requested, account.UserID) {
			selected = append(selected, account)
		}
	}
	if len(selected) == 0 || len(requested) > 0 && len(selected) != len(requested) {
		return domain.TestPlan{}, nil, domain.ErrTestAccountNotFound
	}
	return plan, selected, nil
}

func (s *TestPlanService) ListTestAccounts(ctx context.Context, ownerUserID string, environment string) ([]domain.TestAccount, error) {
	owner := strings.TrimSpace(ownerUserID)
	if owner == "" {
		return nil, domain.ErrInvalidTestPlanInput
	}
	return s.repository.ListTestAccounts(ctx, owner, strings.TrimSpace(environment))
}

func (s *TestPlanService) UpsertTestAccount(ctx context.Context, input domain.TestAccountInput) (domain.TestAccount, error) {
	normalized := domain.TestAccountInput{
		OwnerUserID: strings.TrimSpace(input.OwnerUserID),
		Environment: strings.TrimSpace(input.Environment),
		UserID:      strings.TrimSpace(input.UserID),
		Note:        strings.TrimSpace(input.Note),
	}
	if normalized.OwnerUserID == "" ||
		normalized.Environment == "" ||
		!domain.IsValidTestPlanUserID(normalized.UserID) ||
		!domain.IsValidTestPlanNote(normalized.Note) {
		return domain.TestAccount{}, domain.ErrInvalidTestPlanInput
	}
	return s.repository.UpsertTestAccount(ctx, normalized)
}

func (s *TestPlanService) DeleteTestAccount(ctx context.Context, ownerUserID string, environment string, userID string) error {
	owner := strings.TrimSpace(ownerUserID)
	env := strings.TrimSpace(environment)
	normalizedUserID := strings.TrimSpace(userID)
	if owner == "" || env == "" || !domain.IsValidTestPlanUserID(normalizedUserID) {
		return domain.ErrInvalidTestPlanInput
	}
	return s.repository.DeleteTestAccount(ctx, owner, env, normalizedUserID)
}

func (s *TestPlanService) validateExpiresAt(expiresAt time.Time, now time.Time) error {
	if !expiresAt.After(now) || expiresAt.After(now.Add(domain.MaxTestPlanTTL)) {
		return domain.ErrInvalidTestPlanInput
	}
	return nil
}

func normalizeTestPlanID(id string) (string, error) {
	normalized := strings.TrimSpace(id)
	if _, err := uuid.Parse(normalized); err != nil {
		return "", domain.ErrTestPlanNotFound
	}
	return normalized, nil
}

func normalizeTestPlanItemInput(item domain.TestPlanItemInput) (domain.TestPlanItemInput, error) {
	normalized := domain.TestPlanItemInput{
		ModuleID:      strings.TrimSpace(item.ModuleID),
		Branch:        strings.TrimSpace(item.Branch),
		Mode:          domain.TestPlanItemMode(strings.TrimSpace(string(item.Mode))),
		BuildRecordID: strings.TrimSpace(item.BuildRecordID),
	}
	if normalized.Mode == "" {
		normalized.Mode = domain.TestPlanItemModeFollow
	}
	if !domain.IsValidModuleID(normalized.ModuleID) ||
		!domain.IsValidTestPlanBranch(normalized.Branch) ||
		!domain.IsValidTestPlanItemMode(normalized.Mode) {
		return domain.TestPlanItemInput{}, domain.ErrInvalidTestPlanInput
	}
	if normalized.Mode == domain.TestPlanItemModePinned && normalized.BuildRecordID == "" {
		return domain.TestPlanItemInput{}, domain.ErrInvalidTestPlanInput
	}
	if normalized.BuildRecordID != "" {
		if _, err := uuid.Parse(normalized.BuildRecordID); err != nil {
			return domain.TestPlanItemInput{}, domain.ErrInvalidTestPlanInput
		}
	}
	return normalized, nil
}

func normalizeTestPlanMembers(members []domain.TestPlanMemberInput) ([]domain.TestPlanMemberInput, error) {
	if len(members) > domain.MaxTestPlanMembers {
		return nil, domain.ErrTestPlanMemberLimit
	}
	seen := map[string]bool{}
	normalized := make([]domain.TestPlanMemberInput, 0, len(members))
	for _, member := range members {
		userID := strings.TrimSpace(member.UserID)
		note := strings.TrimSpace(member.Note)
		if !domain.IsValidTestPlanUserID(userID) || !domain.IsValidTestPlanNote(note) {
			return nil, domain.ErrInvalidTestPlanInput
		}
		if seen[userID] {
			continue
		}
		seen[userID] = true
		normalized = append(normalized, domain.TestPlanMemberInput{UserID: userID, Note: note})
	}
	return normalized, nil
}
