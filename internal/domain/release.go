package domain

import (
	"errors"
	"time"
)

var (
	ErrInvalidReleaseInput  = errors.New("invalid release request")
	ErrReleaseAlreadyActive = errors.New("release already active")
	ErrReleaseNotFound      = errors.New("release not found")
	ErrReleaseRouteNotFound = errors.New("release route not found")
	// ErrReleaseNotActive 表示要停用或回滚的发布已经失效。
	ErrReleaseNotActive = errors.New("release not active")
	// ErrReleaseRollbackUnavailable 表示回滚的发布不是全量，或者同一模块、环境、运行时版本下没有更早的全量可以回滚。
	ErrReleaseRollbackUnavailable = errors.New("release rollback unavailable")
)

type ReleaseType string
type ReleaseGrayStrategy string
type ReleaseStatus string
type ReleaseRouteType string
type ReleaseRouteStatus string

const (
	ReleaseTypeFull ReleaseType = "full"
	ReleaseTypeGray ReleaseType = "gray"
)

// 按 identifier 定向灰度（targeted）已由提测单取代，见 docs/test-plans.md；历史发布记录里仍可能读到 "targeted"。
const (
	ReleaseGrayStrategyPercentage ReleaseGrayStrategy = "percentage"
)

const (
	ReleaseStatusActive   ReleaseStatus = "active"
	ReleaseStatusInactive ReleaseStatus = "inactive"
)

const (
	ReleaseRouteTypeDefault    ReleaseRouteType = "default"
	ReleaseRouteTypePercentage ReleaseRouteType = "percentage"
)

const (
	ReleaseRouteStatusActive   ReleaseRouteStatus = "active"
	ReleaseRouteStatusInactive ReleaseRouteStatus = "inactive"
)

type ReleaseRecord struct {
	ID              string
	ApplicationName string
	ModuleID        string
	ModuleName      string
	Environment     string
	EnvironmentName string
	BuildRecordID   string
	RuntimeVersion  string
	ReleaseType     ReleaseType
	GrayStrategy    ReleaseGrayStrategy
	GrayPercentage  int
	ExpiresAt       *time.Time
	Status          ReleaseStatus
	ArtifactKey     string
	ArtifactURL     string
	Description     string
	CreatedByUserID string
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ReleaseRoute struct {
	ID              string
	ApplicationName string
	ModuleID        string
	ModuleName      string
	Environment     string
	EnvironmentName string
	ReleaseID       string
	BuildRecordID   string
	RuntimeVersion  string
	RouteType       ReleaseRouteType
	GrayStrategy    ReleaseGrayStrategy
	GrayPercentage  int
	Status          ReleaseRouteStatus
	ArtifactKey     string
	ArtifactURL     string
	CreatedByUserID string
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ReleaseCreateInput struct {
	BuildRecordID   string
	ReleaseType     ReleaseType
	GrayStrategy    ReleaseGrayStrategy
	GrayPercentage  int
	Description     string
	CreatedByUserID string
	// RollbackFromReleaseID 非空表示这次发布是从该全量发布回滚，只记录在操作记录里。
	RollbackFromReleaseID string
}

// ReleaseDeactivateInput 停用一条生效中的发布。停用全量后，该运行时版本下没有命中灰度的客户端拿不到包，会回退到内置包。
type ReleaseDeactivateInput struct {
	ID          string
	Reason      string
	ActorUserID string
}

// ReleaseRollbackInput 把一条生效中的全量发布回滚到同一模块、环境、运行时版本下上一次全量发布的构建。
type ReleaseRollbackInput struct {
	ID          string
	Description string
	ActorUserID string
}

type ReleaseEventKind string

// 发布操作记录的类型。
const (
	ReleaseEventCreated               ReleaseEventKind = "created"
	ReleaseEventGrayPercentageUpdated ReleaseEventKind = "gray_percentage_updated"
	ReleaseEventPromotedToFull        ReleaseEventKind = "promoted_to_full"
	// ReleaseEventReplaced：被同一运行时版本下的新发布替换而失效，detail.byReleaseId 是新发布。
	ReleaseEventReplaced    ReleaseEventKind = "replaced"
	ReleaseEventDeactivated ReleaseEventKind = "deactivated"
)

// MaxReleaseEvents 是一次返回的发布操作记录上限。
const MaxReleaseEvents = 100

// MaxReleaseReasonLength 是停用原因、回滚说明的最大长度（按字符计）。
const MaxReleaseReasonLength = 200

type ReleaseEvent struct {
	ID          int64
	ReleaseID   string
	Kind        ReleaseEventKind
	Detail      map[string]any
	ActorUserID string
	ActorName   string
	CreatedAt   time.Time
}

type ReleaseRecordFilters struct {
	ApplicationID string
	Environment   string
	ModuleID      string
	Pagination    Pagination
	ReleaseType   ReleaseType
	Status        ReleaseStatus
}

type ReleaseRecordSummary struct {
	Active   int
	Inactive int
	Total    int
}

type ReleaseGrayPercentageUpdateInput struct {
	ID              string
	GrayPercentage  int
	UpdatedByUserID string
}

type ReleaseResolveInput struct {
	ModuleID    string
	Environment string
	// RuntimeVersion 为空时不限制运行时版本，按创建时间取最新路由；非空时只在该运行时版本下解析。
	RuntimeVersion string
	UserID         string
}

func IsValidReleaseType(releaseType ReleaseType) bool {
	return releaseType == ReleaseTypeFull || releaseType == ReleaseTypeGray
}

func IsValidReleaseStatus(status ReleaseStatus) bool {
	return status == ReleaseStatusActive || status == ReleaseStatusInactive
}

func IsValidReleaseGrayStrategy(strategy ReleaseGrayStrategy) bool {
	return strategy == ReleaseGrayStrategyPercentage
}

// 发布能力冲突的原因。
const (
	// ReleaseCapabilityUnmet：Common 构建提供的能力等级低于 Feature 构建的要求。
	ReleaseCapabilityUnmet = "unmet"
	// ReleaseCapabilityCommonMissing：Feature 构建声明了要求，但所属 Common 模块在该环境、运行时版本下没有生效中的发布。
	ReleaseCapabilityCommonMissing = "common_missing"
)

// ReleaseCapabilityConflict 是一条和本次发布不兼容的发布。Common 发布时列出不满足的 Feature 构建，
// Feature 发布时列出不满足要求的 Common 构建；Reason 为 common_missing 时 BuildRecordID 为空。
type ReleaseCapabilityConflict struct {
	ModuleID      string
	ModuleName    string
	ReleaseID     string
	ReleaseType   ReleaseType
	BuildRecordID string
	BuildNumber   int64
	Reason        string
	Shortfalls    []CapabilityShortfall
}

// ReleaseCapabilityConflictError 表示发布后会出现 Common 与 Feature 构建能力不兼容的组合，发布被拒绝。
type ReleaseCapabilityConflictError struct {
	Conflicts []ReleaseCapabilityConflict
}

func (e *ReleaseCapabilityConflictError) Error() string {
	return "release capability conflict"
}
