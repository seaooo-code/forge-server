package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrTestPlanNotFound       = errors.New("test plan not found")
	ErrInvalidTestPlanInput   = errors.New("invalid test plan request")
	ErrTestPlanClosed         = errors.New("test plan closed")
	ErrTestPlanItemNotFound   = errors.New("test plan item not found")
	ErrTestPlanMemberNotFound = errors.New("test plan member not found")
	ErrTestAccountNotFound    = errors.New("test account not found")
	// ErrTestPlanBuildNotFound 表示条目指定的构建不可用：不属于该模块、该分支、该环境，或没有构建成功。
	ErrTestPlanBuildNotFound = errors.New("test plan build not found")
	// ErrTestPlanMemberLimit 表示提测单成员超过 MaxTestPlanMembers。
	ErrTestPlanMemberLimit = errors.New("test plan member limit exceeded")
	// ErrTestPlansDisabled 表示提测单所在环境没有开启「允许提测单」。
	ErrTestPlansDisabled = errors.New("test plans disabled for environment")
)

const (
	// DefaultTestPlanTTL 是新建提测单默认的有效期。
	DefaultTestPlanTTL = 7 * 24 * time.Hour
	// MaxTestPlanTTL 是提测单从当前时间起最长的有效期，延长时同样受限。
	MaxTestPlanTTL = 30 * 24 * time.Hour
	// MaxTestPlanMembers 是单张提测单的成员上限。
	MaxTestPlanMembers = 200
	// MaxTestPlanEvents 是详情接口返回的操作记录条数上限，按时间倒序。
	MaxTestPlanEvents = 100
)

type TestPlanStatus string

const (
	TestPlanStatusOpen   TestPlanStatus = "open"
	TestPlanStatusClosed TestPlanStatus = "closed"
)

type TestPlanItemMode string

const (
	// TestPlanItemModeFollow：该分支在同环境下构建成功后，条目自动换成新构建。
	TestPlanItemModeFollow TestPlanItemMode = "follow"
	// TestPlanItemModePinned：构建固定不变，用于回归或复测某一次构建。
	TestPlanItemModePinned TestPlanItemMode = "pinned"
)

type TestPlanEventKind string

const (
	TestPlanEventCreated                 TestPlanEventKind = "created"
	TestPlanEventUpdated                 TestPlanEventKind = "updated"
	TestPlanEventClosed                  TestPlanEventKind = "closed"
	TestPlanEventExtended                TestPlanEventKind = "extended"
	TestPlanEventItemSet                 TestPlanEventKind = "item_set"
	TestPlanEventItemDeleted             TestPlanEventKind = "item_deleted"
	TestPlanEventItemFollowed            TestPlanEventKind = "item_followed"
	TestPlanEventMemberAdded             TestPlanEventKind = "member_added"
	TestPlanEventMemberRemoved           TestPlanEventKind = "member_removed"
	TestPlanEventMemberRemovedByConflict TestPlanEventKind = "member_removed_by_conflict"
)

type TestPlan struct {
	ID              string
	ApplicationID   string
	ApplicationName string
	ApplicationSlug string
	Environment     string
	EnvironmentName string
	Name            string
	Link            string
	Status          TestPlanStatus
	ExpiresAt       time.Time
	CreatedByUserID string
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ClosedAt        *time.Time
	ItemCount       int
	MemberCount     int
}

// Active 表示提测单进行中且未过期；只有进行中的提测单参与 manifest 解析和冲突校验。
func (p TestPlan) Active(now time.Time) bool {
	return p.Status == TestPlanStatusOpen && p.ExpiresAt.After(now)
}

type TestPlanItem struct {
	TestPlanID      string
	ModuleID        string
	ModuleName      string
	ModuleKind      ModuleKind
	Branch          string
	Mode            TestPlanItemMode
	BuildRecordID   string
	BuildNumber     int64
	CommitSHA       string
	RuntimeVersion  string
	Platforms       []string
	UpdatedByUserID string
	UpdatedBy       string
	UpdatedAt       time.Time
}

type TestPlanMember struct {
	TestPlanID    string
	UserID        string
	Note          string
	AddedByUserID string
	AddedBy       string
	CreatedAt     time.Time
}

type TestPlanEvent struct {
	ID          int64
	TestPlanID  string
	Kind        TestPlanEventKind
	Detail      map[string]any
	ActorUserID string
	Actor       string
	CreatedAt   time.Time
}

type TestPlanDetail struct {
	Plan    TestPlan
	Items   []TestPlanItem
	Members []TestPlanMember
	Events  []TestPlanEvent
}

type TestPlanFilters struct {
	ApplicationID string
	Environment   string
	Status        TestPlanStatus
	MemberUserID  string
	Pagination    Pagination
}

type TestPlanCreateInput struct {
	ApplicationID string
	Environment   string
	Name          string
	Link          string
	ExpiresAt     *time.Time
	Items         []TestPlanItemInput
	Members       []TestPlanMemberInput
	// ResolveConflicts 为 true 时，把与本提测单冲突的成员从对方提测单移出；否则冲突时返回 TestPlanMemberConflictError。
	ResolveConflicts bool
	ActorUserID      string
}

type TestPlanUpdateInput struct {
	Name        *string
	Link        *string
	ActorUserID string
}

type TestPlanExtendInput struct {
	ExpiresAt   time.Time
	ActorUserID string
}

type TestPlanItemInput struct {
	ModuleID string
	Branch   string
	Mode     TestPlanItemMode
	// BuildRecordID 在固定模式下必填；跟随模式下为空时取该分支在同环境下最新的成功构建。
	BuildRecordID string
}

type TestPlanMemberInput struct {
	UserID string
	Note   string
}

// TestPlanMutationResult 是会触发冲突校验的写操作结果：最新详情，以及因冲突被移出的成员。
type TestPlanMutationResult struct {
	Detail  TestPlanDetail
	Removed []TestPlanMemberConflict
}

// TestPlanRef 是提测单的简要引用，用在冲突提示里。
type TestPlanRef struct {
	ID   string
	Name string
}

// TestPlanMemberConflict 表示 UserID 已在另一张进行中的提测单 TestPlan 里测试 ModuleIDs 这些模块。
type TestPlanMemberConflict struct {
	UserID    string
	TestPlan  TestPlanRef
	ModuleIDs []string
}

// TestPlanMemberConflictError 表示写操作会造成成员冲突，且请求没有确认移出。
type TestPlanMemberConflictError struct {
	Conflicts []TestPlanMemberConflict
}

func (e *TestPlanMemberConflictError) Error() string {
	return "test plan member conflict"
}

// TestPlanBranchInUseError 表示模块的分支已被另一张进行中的提测单占用，不能自动处理。
type TestPlanBranchInUseError struct {
	ModuleID string
	Branch   string
	TestPlan TestPlanRef
}

func (e *TestPlanBranchInUseError) Error() string {
	return "test plan branch in use"
}

type TestAccount struct {
	OwnerUserID string
	Environment string
	UserID      string
	Note        string
	CreatedAt   time.Time
}

type TestAccountInput struct {
	OwnerUserID string
	Environment string
	UserID      string
	Note        string
}

var testPlanUserIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// IsValidTestPlanUserID 校验客户端 userId：1 到 64 位字母、数字和 . _ -。
func IsValidTestPlanUserID(value string) bool {
	return testPlanUserIDPattern.MatchString(value)
}

func IsValidTestPlanItemMode(mode TestPlanItemMode) bool {
	return mode == TestPlanItemModeFollow || mode == TestPlanItemModePinned
}

func IsValidTestPlanStatus(status TestPlanStatus) bool {
	return status == TestPlanStatusOpen || status == TestPlanStatusClosed
}

// IsValidTestPlanName 校验提测单名称：1 到 100 个字符，不含控制字符。
func IsValidTestPlanName(value string) bool {
	count := utf8.RuneCountInString(value)
	return count >= 1 && count <= 100 && !strings.ContainsFunc(value, isControlRune)
}

// IsValidTestPlanNote 校验成员和测试账号备注：最多 64 个字符，不含控制字符。
func IsValidTestPlanNote(value string) bool {
	return utf8.RuneCountInString(value) <= 64 && !strings.ContainsFunc(value, isControlRune)
}

// IsValidTestPlanLink 校验关联链接：可为空，非空时最长 500 个字符，必须是 http 或 https 地址。
func IsValidTestPlanLink(value string) bool {
	if value == "" {
		return true
	}
	if utf8.RuneCountInString(value) > 500 || strings.ContainsFunc(value, isControlRune) {
		return false
	}
	return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://")
}

func isControlRune(r rune) bool {
	return r < 0x20 || r == 0x7f
}

// IsValidTestPlanBranch 校验条目绑定的分支名：1 到 255 个字符，不含空白和控制字符。
// 分支名只用于和构建记录的 branch 精确匹配，不做 Git 引用格式的完整校验。
func IsValidTestPlanBranch(value string) bool {
	count := utf8.RuneCountInString(value)
	return count >= 1 && count <= 255 && !strings.ContainsFunc(value, func(r rune) bool { return isControlRune(r) || r == ' ' || r == '\t' })
}
