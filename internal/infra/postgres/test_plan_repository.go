package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"forge-server/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPlanRepository 读写提测单。会触发冲突校验的写操作（新建、设置条目、新增成员）在同一事务里完成：
// 先对「应用 + 环境」加 advisory lock，再写入，最后按 docs/test-plans.md 第 3.2、3.3 节检查冲突，
// 校验不通过就回滚，保证并发请求不能绕过规则。
type TestPlanRepository struct {
	pool *pgxpool.Pool
}

func NewTestPlanRepository(pool *pgxpool.Pool) *TestPlanRepository {
	return &TestPlanRepository{pool: pool}
}

// testPlanRef 是事务内锁定的提测单基本信息。
type testPlanRef struct {
	ID            string
	Name          string
	ApplicationID string
	Environment   string
}

func (r *TestPlanRepository) ListTestPlans(ctx context.Context, filters domain.TestPlanFilters) ([]domain.TestPlan, int, error) {
	where, args := testPlanListWhere(filters)

	var total int
	if err := r.pool.QueryRow(ctx, `select count(*) from test_plans tp `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count test plans: %w", err)
	}

	args = append(args, filters.Pagination.Limit(), filters.Pagination.Offset())
	rows, err := r.pool.Query(ctx, testPlanSelectSQL()+where+fmt.Sprintf(`
		order by tp.created_at desc
		limit nullif($%d, 0) offset $%d
	`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query test plans: %w", err)
	}
	defer rows.Close()

	plans := []domain.TestPlan{}
	for rows.Next() {
		plan, err := scanTestPlan(rows)
		if err != nil {
			return nil, 0, err
		}
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate test plans: %w", err)
	}
	return plans, total, nil
}

func testPlanListWhere(filters domain.TestPlanFilters) (string, []any) {
	conditions := []string{}
	args := []any{}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, strings.ReplaceAll(condition, "?", fmt.Sprintf("$%d", len(args))))
	}
	if filters.ApplicationID != "" {
		add("tp.application_id = ?", filters.ApplicationID)
	}
	if filters.Environment != "" {
		add("tp.environment_code = ?", filters.Environment)
	}
	switch filters.Status {
	case domain.TestPlanStatusOpen:
		conditions = append(conditions, "tp.status = 'open' and tp.expires_at > now()")
	case domain.TestPlanStatusClosed:
		conditions = append(conditions, "(tp.status = 'closed' or tp.expires_at <= now())")
	}
	if filters.MemberUserID != "" {
		add("exists(select 1 from test_plan_members m where m.test_plan_id = tp.id and m.user_id = ?)", filters.MemberUserID)
	}
	if len(conditions) == 0 {
		return "", args
	}
	return " where " + strings.Join(conditions, " and "), args
}

func (r *TestPlanRepository) GetTestPlan(ctx context.Context, id string) (domain.TestPlan, error) {
	plan, err := scanTestPlan(r.pool.QueryRow(ctx, testPlanSelectSQL()+` where tp.id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.TestPlan{}, domain.ErrTestPlanNotFound
		}
		return domain.TestPlan{}, err
	}
	return plan, nil
}

func (r *TestPlanRepository) GetTestPlanDetail(ctx context.Context, id string) (domain.TestPlanDetail, error) {
	plan, err := r.GetTestPlan(ctx, id)
	if err != nil {
		return domain.TestPlanDetail{}, err
	}
	detail := domain.TestPlanDetail{Plan: plan}

	itemRows, err := r.pool.Query(ctx, `
		select
			item.test_plan_id,
			item.module_id,
			m.name,
			m.kind,
			item.branch,
			item.mode,
			item.build_record_id::text,
			br.number,
			br.commit_sha,
			br.runtime_version,
			br.platforms,
			coalesce(item.updated_by_user_id, ''),
			coalesce(u.name, ''),
			item.updated_at
		from test_plan_items item
		join modules m on m.id = item.module_id
		join build_records br on br.id = item.build_record_id
		left join users u on u.id = item.updated_by_user_id
		where item.test_plan_id = $1
		order by m.sort_order asc, m.created_at asc
	`, id)
	if err != nil {
		return domain.TestPlanDetail{}, fmt.Errorf("query test plan items: %w", err)
	}
	defer itemRows.Close()
	detail.Items = []domain.TestPlanItem{}
	for itemRows.Next() {
		var item domain.TestPlanItem
		if err := itemRows.Scan(
			&item.TestPlanID,
			&item.ModuleID,
			&item.ModuleName,
			&item.ModuleKind,
			&item.Branch,
			&item.Mode,
			&item.BuildRecordID,
			&item.BuildNumber,
			&item.CommitSHA,
			&item.RuntimeVersion,
			&item.Platforms,
			&item.UpdatedByUserID,
			&item.UpdatedBy,
			&item.UpdatedAt,
		); err != nil {
			return domain.TestPlanDetail{}, fmt.Errorf("scan test plan item: %w", err)
		}
		detail.Items = append(detail.Items, item)
	}
	if err := itemRows.Err(); err != nil {
		return domain.TestPlanDetail{}, fmt.Errorf("iterate test plan items: %w", err)
	}

	memberRows, err := r.pool.Query(ctx, `
		select
			mem.test_plan_id,
			mem.user_id,
			mem.note,
			coalesce(mem.added_by_user_id, ''),
			coalesce(u.name, ''),
			mem.created_at
		from test_plan_members mem
		left join users u on u.id = mem.added_by_user_id
		where mem.test_plan_id = $1
		order by mem.created_at asc, mem.user_id asc
	`, id)
	if err != nil {
		return domain.TestPlanDetail{}, fmt.Errorf("query test plan members: %w", err)
	}
	defer memberRows.Close()
	detail.Members = []domain.TestPlanMember{}
	for memberRows.Next() {
		var member domain.TestPlanMember
		if err := memberRows.Scan(
			&member.TestPlanID,
			&member.UserID,
			&member.Note,
			&member.AddedByUserID,
			&member.AddedBy,
			&member.CreatedAt,
		); err != nil {
			return domain.TestPlanDetail{}, fmt.Errorf("scan test plan member: %w", err)
		}
		detail.Members = append(detail.Members, member)
	}
	if err := memberRows.Err(); err != nil {
		return domain.TestPlanDetail{}, fmt.Errorf("iterate test plan members: %w", err)
	}

	eventRows, err := r.pool.Query(ctx, `
		select
			ev.id,
			ev.test_plan_id,
			ev.kind,
			ev.detail,
			coalesce(ev.actor_user_id, ''),
			coalesce(u.name, ''),
			ev.created_at
		from test_plan_events ev
		left join users u on u.id = ev.actor_user_id
		where ev.test_plan_id = $1
		order by ev.created_at desc, ev.id desc
		limit $2
	`, id, domain.MaxTestPlanEvents)
	if err != nil {
		return domain.TestPlanDetail{}, fmt.Errorf("query test plan events: %w", err)
	}
	defer eventRows.Close()
	detail.Events = []domain.TestPlanEvent{}
	for eventRows.Next() {
		var event domain.TestPlanEvent
		var raw []byte
		if err := eventRows.Scan(
			&event.ID,
			&event.TestPlanID,
			&event.Kind,
			&raw,
			&event.ActorUserID,
			&event.Actor,
			&event.CreatedAt,
		); err != nil {
			return domain.TestPlanDetail{}, fmt.Errorf("scan test plan event: %w", err)
		}
		event.Detail = map[string]any{}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &event.Detail); err != nil {
				return domain.TestPlanDetail{}, fmt.Errorf("decode test plan event detail: %w", err)
			}
		}
		detail.Events = append(detail.Events, event)
	}
	if err := eventRows.Err(); err != nil {
		return domain.TestPlanDetail{}, fmt.Errorf("iterate test plan events: %w", err)
	}
	return detail, nil
}

// CreateTestPlan 新建提测单及其初始条目和成员，按冲突规则校验。
func (r *TestPlanRepository) CreateTestPlan(ctx context.Context, input domain.TestPlanCreateInput) (domain.TestPlanMutationResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.TestPlanMutationResult{}, fmt.Errorf("begin create test plan: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	if err := lockTestPlanScope(ctx, tx, input.ApplicationID, input.Environment); err != nil {
		return domain.TestPlanMutationResult{}, err
	}

	var bound bool
	var allowed bool
	if err := tx.QueryRow(ctx, `
		select
			exists(select 1 from application_environment_bindings where application_id = $1 and environment_code = $2),
			coalesce((select allow_test_plans from environments where code = $2), false)
	`, input.ApplicationID, input.Environment).Scan(&bound, &allowed); err != nil {
		return domain.TestPlanMutationResult{}, fmt.Errorf("query test plan environment: %w", err)
	}
	if !bound {
		return domain.TestPlanMutationResult{}, domain.ErrInvalidTestPlanInput
	}
	if !allowed {
		return domain.TestPlanMutationResult{}, domain.ErrTestPlansDisabled
	}

	plan := testPlanRef{
		ID:            uuid.NewString(),
		Name:          input.Name,
		ApplicationID: input.ApplicationID,
		Environment:   input.Environment,
	}
	if _, err := tx.Exec(ctx, `
		insert into test_plans (id, application_id, environment_code, name, link, status, expires_at, created_by_user_id)
		values ($1, $2, $3, $4, $5, 'open', $6, $7)
	`, plan.ID, plan.ApplicationID, plan.Environment, input.Name, input.Link, input.ExpiresAt, input.ActorUserID); err != nil {
		if isForeignKeyViolation(err) {
			return domain.TestPlanMutationResult{}, domain.ErrInvalidTestPlanInput
		}
		return domain.TestPlanMutationResult{}, fmt.Errorf("insert test plan: %w", err)
	}
	if err := insertTestPlanEvent(ctx, tx, plan.ID, domain.TestPlanEventCreated, map[string]any{"name": input.Name}, input.ActorUserID); err != nil {
		return domain.TestPlanMutationResult{}, err
	}
	for _, item := range input.Items {
		if err := upsertTestPlanItem(ctx, tx, plan, item, input.ActorUserID); err != nil {
			return domain.TestPlanMutationResult{}, err
		}
	}
	for _, member := range input.Members {
		if err := insertTestPlanMember(ctx, tx, plan.ID, member, input.ActorUserID); err != nil {
			return domain.TestPlanMutationResult{}, err
		}
	}

	removed, err := checkTestPlanConflicts(ctx, tx, plan, input.ResolveConflicts, input.ActorUserID)
	if err != nil {
		return domain.TestPlanMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.TestPlanMutationResult{}, fmt.Errorf("commit create test plan: %w", err)
	}
	return r.mutationResult(ctx, plan.ID, removed)
}

func (r *TestPlanRepository) UpdateTestPlan(ctx context.Context, id string, input domain.TestPlanUpdateInput) (domain.TestPlanDetail, error) {
	err := r.withActivePlan(ctx, id, func(tx pgx.Tx, plan testPlanRef) error {
		detail := map[string]any{}
		if input.Name != nil {
			detail["name"] = *input.Name
		}
		if input.Link != nil {
			detail["link"] = *input.Link
		}
		if _, err := tx.Exec(ctx, `
			update test_plans
			set name = coalesce($2, name),
				link = coalesce($3, link),
				updated_at = now()
			where id = $1
		`, id, input.Name, input.Link); err != nil {
			return fmt.Errorf("update test plan: %w", err)
		}
		return insertTestPlanEvent(ctx, tx, id, domain.TestPlanEventUpdated, detail, input.ActorUserID)
	})
	if err != nil {
		return domain.TestPlanDetail{}, err
	}
	return r.GetTestPlanDetail(ctx, id)
}

func (r *TestPlanRepository) CloseTestPlan(ctx context.Context, id string, actorUserID string) (domain.TestPlanDetail, error) {
	err := r.withActivePlan(ctx, id, func(tx pgx.Tx, plan testPlanRef) error {
		if _, err := tx.Exec(ctx, `
			update test_plans
			set status = 'closed', closed_at = now(), updated_at = now()
			where id = $1
		`, id); err != nil {
			return fmt.Errorf("close test plan: %w", err)
		}
		return insertTestPlanEvent(ctx, tx, id, domain.TestPlanEventClosed, map[string]any{}, actorUserID)
	})
	if err != nil {
		return domain.TestPlanDetail{}, err
	}
	return r.GetTestPlanDetail(ctx, id)
}

// ExtendTestPlan 修改进行中提测单的有效期。已过期的提测单不能延长：过期后它的分支和成员可能已被其他提测单使用，
// 重新生效会绕过冲突规则，需要新建提测单。
func (r *TestPlanRepository) ExtendTestPlan(ctx context.Context, id string, input domain.TestPlanExtendInput) (domain.TestPlanDetail, error) {
	err := r.withActivePlan(ctx, id, func(tx pgx.Tx, plan testPlanRef) error {
		if _, err := tx.Exec(ctx, `
			update test_plans set expires_at = $2, updated_at = now() where id = $1
		`, id, input.ExpiresAt); err != nil {
			return fmt.Errorf("extend test plan: %w", err)
		}
		return insertTestPlanEvent(ctx, tx, id, domain.TestPlanEventExtended, map[string]any{"expiresAt": input.ExpiresAt.UTC().Format(time.RFC3339)}, input.ActorUserID)
	})
	if err != nil {
		return domain.TestPlanDetail{}, err
	}
	return r.GetTestPlanDetail(ctx, id)
}

func (r *TestPlanRepository) SetTestPlanItem(ctx context.Context, id string, item domain.TestPlanItemInput, resolveConflicts bool, actorUserID string) (domain.TestPlanMutationResult, error) {
	var removed []domain.TestPlanMemberConflict
	err := r.withActivePlan(ctx, id, func(tx pgx.Tx, plan testPlanRef) error {
		if err := upsertTestPlanItem(ctx, tx, plan, item, actorUserID); err != nil {
			return err
		}
		var err error
		removed, err = checkTestPlanConflicts(ctx, tx, plan, resolveConflicts, actorUserID)
		return err
	})
	if err != nil {
		return domain.TestPlanMutationResult{}, err
	}
	return r.mutationResult(ctx, id, removed)
}

func (r *TestPlanRepository) DeleteTestPlanItem(ctx context.Context, id string, moduleID string, actorUserID string) (domain.TestPlanDetail, error) {
	err := r.withActivePlan(ctx, id, func(tx pgx.Tx, plan testPlanRef) error {
		tag, err := tx.Exec(ctx, `delete from test_plan_items where test_plan_id = $1 and module_id = $2`, id, moduleID)
		if err != nil {
			return fmt.Errorf("delete test plan item: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrTestPlanItemNotFound
		}
		return insertTestPlanEvent(ctx, tx, id, domain.TestPlanEventItemDeleted, map[string]any{"moduleId": moduleID}, actorUserID)
	})
	if err != nil {
		return domain.TestPlanDetail{}, err
	}
	return r.GetTestPlanDetail(ctx, id)
}

func (r *TestPlanRepository) AddTestPlanMembers(ctx context.Context, id string, members []domain.TestPlanMemberInput, resolveConflicts bool, actorUserID string) (domain.TestPlanMutationResult, error) {
	var removed []domain.TestPlanMemberConflict
	err := r.withActivePlan(ctx, id, func(tx pgx.Tx, plan testPlanRef) error {
		for _, member := range members {
			if err := insertTestPlanMember(ctx, tx, id, member, actorUserID); err != nil {
				return err
			}
		}
		var err error
		removed, err = checkTestPlanConflicts(ctx, tx, plan, resolveConflicts, actorUserID)
		return err
	})
	if err != nil {
		return domain.TestPlanMutationResult{}, err
	}
	return r.mutationResult(ctx, id, removed)
}

func (r *TestPlanRepository) RemoveTestPlanMembers(ctx context.Context, id string, userIDs []string, actorUserID string) (domain.TestPlanDetail, error) {
	err := r.withActivePlan(ctx, id, func(tx pgx.Tx, plan testPlanRef) error {
		removedAny := false
		for _, userID := range userIDs {
			tag, err := tx.Exec(ctx, `delete from test_plan_members where test_plan_id = $1 and user_id = $2`, id, userID)
			if err != nil {
				return fmt.Errorf("delete test plan member: %w", err)
			}
			if tag.RowsAffected() == 0 {
				continue
			}
			removedAny = true
			if err := insertTestPlanEvent(ctx, tx, id, domain.TestPlanEventMemberRemoved, map[string]any{"userId": userID}, actorUserID); err != nil {
				return err
			}
		}
		if !removedAny {
			return domain.ErrTestPlanMemberNotFound
		}
		_, err := tx.Exec(ctx, `update test_plans set updated_at = now() where id = $1`, id)
		return err
	})
	if err != nil {
		return domain.TestPlanDetail{}, err
	}
	return r.GetTestPlanDetail(ctx, id)
}

// FollowBuild 把跟随该构建分支的条目换成这次构建：同模块、同分支、同环境、跟随模式、提测单进行中，
// 且当前条目的构建号小于这次构建（旧构建的延迟回调不覆盖新构建）。返回被更新的提测单 ID。
func (r *TestPlanRepository) FollowBuild(ctx context.Context, record domain.BuildRecord) ([]string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin follow test plan build: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	rows, err := tx.Query(ctx, `
		with targets as (
			select item.test_plan_id, item.module_id
			from test_plan_items item
			join test_plans tp on tp.id = item.test_plan_id
			join build_records current_build on current_build.id = item.build_record_id
			where item.module_id = $1
				and item.branch = $2
				and item.mode = 'follow'
				and tp.environment_code = $3
				and tp.status = 'open'
				and tp.expires_at > now()
				and current_build.number < $4
			for update of item
		)
		update test_plan_items item
		set build_record_id = $5, updated_by_user_id = null, updated_at = now()
		from targets
		where item.test_plan_id = targets.test_plan_id
			and item.module_id = targets.module_id
		returning item.test_plan_id::text
	`, record.ModuleID, record.Branch, record.Environment, record.Number, record.ID)
	if err != nil {
		return nil, fmt.Errorf("follow test plan build: %w", err)
	}
	planIDs := []string{}
	for rows.Next() {
		var planID string
		if err := rows.Scan(&planID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan followed test plan: %w", err)
		}
		planIDs = append(planIDs, planID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate followed test plans: %w", err)
	}

	for _, planID := range planIDs {
		if err := insertTestPlanEvent(ctx, tx, planID, domain.TestPlanEventItemFollowed, map[string]any{
			"moduleId":      record.ModuleID,
			"branch":        record.Branch,
			"buildRecordId": record.ID,
			"buildNumber":   record.Number,
		}, ""); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit follow test plan build: %w", err)
	}
	return planIDs, nil
}

func (r *TestPlanRepository) ListTestAccounts(ctx context.Context, ownerUserID string, environment string) ([]domain.TestAccount, error) {
	rows, err := r.pool.Query(ctx, `
		select owner_user_id, environment_code, user_id, note, created_at
		from user_test_accounts
		where owner_user_id = $1
			and ($2 = '' or environment_code = $2)
		order by environment_code asc, created_at asc
	`, ownerUserID, environment)
	if err != nil {
		return nil, fmt.Errorf("query test accounts: %w", err)
	}
	defer rows.Close()
	accounts := []domain.TestAccount{}
	for rows.Next() {
		var account domain.TestAccount
		if err := rows.Scan(&account.OwnerUserID, &account.Environment, &account.UserID, &account.Note, &account.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan test account: %w", err)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate test accounts: %w", err)
	}
	return accounts, nil
}

// UpsertTestAccount 保存测试账号；已存在时只更新备注。
func (r *TestPlanRepository) UpsertTestAccount(ctx context.Context, input domain.TestAccountInput) (domain.TestAccount, error) {
	var account domain.TestAccount
	if err := r.pool.QueryRow(ctx, `
		insert into user_test_accounts (owner_user_id, environment_code, user_id, note)
		values ($1, $2, $3, $4)
		on conflict (owner_user_id, environment_code, user_id) do update set note = excluded.note
		returning owner_user_id, environment_code, user_id, note, created_at
	`, input.OwnerUserID, input.Environment, input.UserID, input.Note).Scan(
		&account.OwnerUserID, &account.Environment, &account.UserID, &account.Note, &account.CreatedAt,
	); err != nil {
		if isForeignKeyViolation(err) {
			return domain.TestAccount{}, domain.ErrInvalidTestPlanInput
		}
		return domain.TestAccount{}, fmt.Errorf("upsert test account: %w", err)
	}
	return account, nil
}

func (r *TestPlanRepository) DeleteTestAccount(ctx context.Context, ownerUserID string, environment string, userID string) error {
	tag, err := r.pool.Exec(ctx, `
		delete from user_test_accounts where owner_user_id = $1 and environment_code = $2 and user_id = $3
	`, ownerUserID, environment, userID)
	if err != nil {
		return fmt.Errorf("delete test account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrTestAccountNotFound
	}
	return nil
}

func (r *TestPlanRepository) mutationResult(ctx context.Context, id string, removed []domain.TestPlanMemberConflict) (domain.TestPlanMutationResult, error) {
	detail, err := r.GetTestPlanDetail(ctx, id)
	if err != nil {
		return domain.TestPlanMutationResult{}, err
	}
	if removed == nil {
		removed = []domain.TestPlanMemberConflict{}
	}
	return domain.TestPlanMutationResult{Detail: detail, Removed: removed}, nil
}

// withActivePlan 在事务里锁定进行中的提测单（先锁「应用 + 环境」，再锁提测单行）后执行 fn。
// 已关闭或已过期的提测单返回 ErrTestPlanClosed。
func (r *TestPlanRepository) withActivePlan(ctx context.Context, id string, fn func(tx pgx.Tx, plan testPlanRef) error) error {
	var plan testPlanRef
	if err := r.pool.QueryRow(ctx, `
		select id::text, name, application_id, environment_code from test_plans where id = $1
	`, id).Scan(&plan.ID, &plan.Name, &plan.ApplicationID, &plan.Environment); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrTestPlanNotFound
		}
		return fmt.Errorf("query test plan: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin test plan update: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	if err := lockTestPlanScope(ctx, tx, plan.ApplicationID, plan.Environment); err != nil {
		return err
	}
	var active bool
	if err := tx.QueryRow(ctx, `
		select status = 'open' and expires_at > now(), name from test_plans where id = $1 for update
	`, id).Scan(&active, &plan.Name); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrTestPlanNotFound
		}
		return fmt.Errorf("lock test plan: %w", err)
	}
	if !active {
		return domain.ErrTestPlanClosed
	}
	if err := fn(tx, plan); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit test plan update: %w", err)
	}
	return nil
}

// lockTestPlanScope 串行化同一「应用 + 环境」下所有会影响冲突规则的写操作，锁在事务结束时释放。
func lockTestPlanScope(ctx context.Context, tx pgx.Tx, applicationID string, environment string) error {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('forge:test_plan:' || $1 || '/' || $2, 0))`, applicationID, environment); err != nil {
		return fmt.Errorf("lock test plan scope: %w", err)
	}
	return nil
}

// upsertTestPlanItem 设置条目：模块必须属于提测单的应用，构建必须是该模块、该分支、同环境、构建成功且有产物。
// 跟随模式不指定构建时，取该分支在同环境下最新的成功构建。
func upsertTestPlanItem(ctx context.Context, tx pgx.Tx, plan testPlanRef, item domain.TestPlanItemInput, actorUserID string) error {
	var moduleApplicationID string
	if err := tx.QueryRow(ctx, `select application_id from modules where id = $1`, item.ModuleID).Scan(&moduleApplicationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrModuleNotFound
		}
		return fmt.Errorf("query test plan item module: %w", err)
	}
	if moduleApplicationID != plan.ApplicationID {
		return domain.ErrInvalidTestPlanInput
	}

	var buildRecordID string
	err := tx.QueryRow(ctx, `
		select id::text
		from build_records
		where module_id = $1
			and environment_code = $2
			and branch = $3
			and status = 'succeeded'
			and manifest_key <> ''
			and ($4 = '' or id::text = $4)
		order by number desc
		limit 1
	`, item.ModuleID, plan.Environment, item.Branch, item.BuildRecordID).Scan(&buildRecordID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrTestPlanBuildNotFound
		}
		return fmt.Errorf("query test plan item build: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		insert into test_plan_items (test_plan_id, module_id, branch, mode, build_record_id, updated_by_user_id)
		values ($1, $2, $3, $4, $5, nullif($6, ''))
		on conflict (test_plan_id, module_id) do update set
			branch = excluded.branch,
			mode = excluded.mode,
			build_record_id = excluded.build_record_id,
			updated_by_user_id = excluded.updated_by_user_id,
			updated_at = now()
	`, plan.ID, item.ModuleID, item.Branch, item.Mode, buildRecordID, actorUserID); err != nil {
		return fmt.Errorf("upsert test plan item: %w", err)
	}
	if _, err := tx.Exec(ctx, `update test_plans set updated_at = now() where id = $1`, plan.ID); err != nil {
		return fmt.Errorf("touch test plan: %w", err)
	}
	return insertTestPlanEvent(ctx, tx, plan.ID, domain.TestPlanEventItemSet, map[string]any{
		"moduleId":      item.ModuleID,
		"branch":        item.Branch,
		"mode":          string(item.Mode),
		"buildRecordId": buildRecordID,
	}, actorUserID)
}

func insertTestPlanMember(ctx context.Context, tx pgx.Tx, planID string, member domain.TestPlanMemberInput, actorUserID string) error {
	tag, err := tx.Exec(ctx, `
		insert into test_plan_members (test_plan_id, user_id, note, added_by_user_id)
		values ($1, $2, $3, nullif($4, ''))
		on conflict (test_plan_id, user_id) do update set note = excluded.note
			where test_plan_members.note is distinct from excluded.note and excluded.note <> ''
	`, planID, member.UserID, member.Note, actorUserID)
	if err != nil {
		return fmt.Errorf("insert test plan member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `update test_plans set updated_at = now() where id = $1`, planID); err != nil {
		return fmt.Errorf("touch test plan: %w", err)
	}
	return insertTestPlanEvent(ctx, tx, planID, domain.TestPlanEventMemberAdded, map[string]any{"userId": member.UserID}, actorUserID)
}

// checkTestPlanConflicts 在写入之后检查本提测单与同环境其他进行中提测单的冲突：
//   - 同一模块的同一分支被占用：返回 TestPlanBranchInUseError，不能自动处理；
//   - 成员冲突：同一 userId 在另一张提测单里也测同一模块。resolve 为 false 时返回 TestPlanMemberConflictError，
//     为 true 时把这些 userId 从对方提测单移出，记入对方的操作记录，并返回移出的内容。
//
// 同时检查成员上限。调用方在返回错误时回滚事务，所以失败时不会留下任何修改。
func checkTestPlanConflicts(ctx context.Context, tx pgx.Tx, plan testPlanRef, resolve bool, actorUserID string) ([]domain.TestPlanMemberConflict, error) {
	var branchConflict domain.TestPlanBranchInUseError
	err := tx.QueryRow(ctx, `
		select mine.module_id, mine.branch, other.id::text, other.name
		from test_plan_items mine
		join test_plan_items theirs on theirs.module_id = mine.module_id
			and theirs.branch = mine.branch
			and theirs.test_plan_id <> mine.test_plan_id
		join test_plans other on other.id = theirs.test_plan_id
		where mine.test_plan_id = $1
			and other.environment_code = $2
			and other.status = 'open'
			and other.expires_at > now()
		order by mine.module_id
		limit 1
	`, plan.ID, plan.Environment).Scan(&branchConflict.ModuleID, &branchConflict.Branch, &branchConflict.TestPlan.ID, &branchConflict.TestPlan.Name)
	if err == nil {
		return nil, &branchConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("query test plan branch conflict: %w", err)
	}

	var memberCount int
	if err := tx.QueryRow(ctx, `select count(*) from test_plan_members where test_plan_id = $1`, plan.ID).Scan(&memberCount); err != nil {
		return nil, fmt.Errorf("count test plan members: %w", err)
	}
	if memberCount > domain.MaxTestPlanMembers {
		return nil, domain.ErrTestPlanMemberLimit
	}

	rows, err := tx.Query(ctx, `
		select
			mem.user_id,
			other.id::text,
			other.name,
			array_agg(distinct theirs.module_id order by theirs.module_id)
		from test_plan_members mem
		join test_plan_members other_mem on other_mem.user_id = mem.user_id
			and other_mem.test_plan_id <> mem.test_plan_id
		join test_plans other on other.id = other_mem.test_plan_id
		join test_plan_items theirs on theirs.test_plan_id = other.id
		join test_plan_items mine on mine.test_plan_id = mem.test_plan_id
			and mine.module_id = theirs.module_id
		where mem.test_plan_id = $1
			and other.environment_code = $2
			and other.status = 'open'
			and other.expires_at > now()
		group by mem.user_id, other.id, other.name
		order by mem.user_id, other.name
	`, plan.ID, plan.Environment)
	if err != nil {
		return nil, fmt.Errorf("query test plan member conflicts: %w", err)
	}
	conflicts := []domain.TestPlanMemberConflict{}
	for rows.Next() {
		var conflict domain.TestPlanMemberConflict
		if err := rows.Scan(&conflict.UserID, &conflict.TestPlan.ID, &conflict.TestPlan.Name, &conflict.ModuleIDs); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan test plan member conflict: %w", err)
		}
		conflicts = append(conflicts, conflict)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate test plan member conflicts: %w", err)
	}
	if len(conflicts) == 0 {
		return conflicts, nil
	}
	if !resolve {
		return nil, &domain.TestPlanMemberConflictError{Conflicts: conflicts}
	}

	for _, conflict := range conflicts {
		if _, err := tx.Exec(ctx, `
			delete from test_plan_members where test_plan_id = $1 and user_id = $2
		`, conflict.TestPlan.ID, conflict.UserID); err != nil {
			return nil, fmt.Errorf("remove conflicting test plan member: %w", err)
		}
		if _, err := tx.Exec(ctx, `update test_plans set updated_at = now() where id = $1`, conflict.TestPlan.ID); err != nil {
			return nil, fmt.Errorf("touch conflicting test plan: %w", err)
		}
		if err := insertTestPlanEvent(ctx, tx, conflict.TestPlan.ID, domain.TestPlanEventMemberRemovedByConflict, map[string]any{
			"userId":               conflict.UserID,
			"moduleIds":            conflict.ModuleIDs,
			"causedByTestPlanId":   plan.ID,
			"causedByTestPlanName": plan.Name,
		}, actorUserID); err != nil {
			return nil, err
		}
	}
	return conflicts, nil
}

func insertTestPlanEvent(ctx context.Context, tx pgx.Tx, planID string, kind domain.TestPlanEventKind, detail map[string]any, actorUserID string) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("encode test plan event detail: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into test_plan_events (test_plan_id, kind, detail, actor_user_id)
		values ($1, $2, $3, nullif($4, ''))
	`, planID, kind, raw, actorUserID); err != nil {
		return fmt.Errorf("insert test plan event: %w", err)
	}
	return nil
}

func testPlanSelectSQL() string {
	return `
		select
			tp.id::text,
			tp.application_id,
			coalesce(a.name, ''),
			coalesce(a.slug, ''),
			tp.environment_code,
			coalesce(e.name, ''),
			tp.name,
			tp.link,
			tp.status,
			tp.expires_at,
			tp.created_by_user_id,
			coalesce(u.name, ''),
			tp.created_at,
			tp.updated_at,
			tp.closed_at,
			(select count(*) from test_plan_items i where i.test_plan_id = tp.id),
			(select count(*) from test_plan_members m where m.test_plan_id = tp.id)
		from test_plans tp
		left join applications a on a.id = tp.application_id
		left join environments e on e.code = tp.environment_code
		left join users u on u.id = tp.created_by_user_id
	`
}

type testPlanRow interface {
	Scan(dest ...any) error
}

func scanTestPlan(row testPlanRow) (domain.TestPlan, error) {
	var plan domain.TestPlan
	if err := row.Scan(
		&plan.ID,
		&plan.ApplicationID,
		&plan.ApplicationName,
		&plan.ApplicationSlug,
		&plan.Environment,
		&plan.EnvironmentName,
		&plan.Name,
		&plan.Link,
		&plan.Status,
		&plan.ExpiresAt,
		&plan.CreatedByUserID,
		&plan.CreatedBy,
		&plan.CreatedAt,
		&plan.UpdatedAt,
		&plan.ClosedAt,
		&plan.ItemCount,
		&plan.MemberCount,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.TestPlan{}, err
		}
		return domain.TestPlan{}, fmt.Errorf("scan test plan: %w", err)
	}
	return plan, nil
}
