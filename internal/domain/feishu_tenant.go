package domain

import (
	"errors"
	"regexp"
	"time"
)

var (
	ErrFeishuTenantNotFound      = errors.New("feishu tenant not found")
	ErrInvalidFeishuTenantInput  = errors.New("invalid feishu tenant request")
	ErrFeishuTenantIDConflict    = errors.New("feishu tenant id already exists")
	ErrFeishuTenantAppIDConflict = errors.New("feishu tenant app id already exists")
	// ErrFeishuTenantLastActive 表示操作会让平台没有任何可登录的租户。
	ErrFeishuTenantLastActive = errors.New("at least one feishu tenant must stay active")
	// ErrFeishuTenantRequired 表示有多个可用租户，但登录请求没有指定租户。
	ErrFeishuTenantRequired = errors.New("feishu tenant required")
)

type FeishuTenantStatus string

const (
	FeishuTenantStatusActive   FeishuTenantStatus = "active"
	FeishuTenantStatusDisabled FeishuTenantStatus = "disabled"
)

// DefaultFeishuTenantID 是用 FEISHU_APP_ID / FEISHU_APP_SECRET 首次初始化的租户 ID。
const DefaultFeishuTenantID = "default"

const (
	MaxFeishuTenantNameLength   = 64
	MaxFeishuTenantSecretLength = 256
)

var (
	feishuTenantIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)
	feishuTenantAppIDPattern = regexp.MustCompile(`^cli_[A-Za-z0-9]{8,64}$`)
)

// FeishuTenant 是某个飞书租户里的一个自建应用。AppSecret 只在服务端内部使用，接口不返回。
type FeishuTenant struct {
	ID        string
	Name      string
	AppID     string
	AppSecret string
	Status    FeishuTenantStatus
	SortOrder int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// FeishuTenantInput 是新建或编辑租户的输入。编辑时 AppID 不能修改；AppSecret 为空表示沿用原值。
type FeishuTenantInput struct {
	ID        string
	Name      string
	AppID     string
	AppSecret string
	Status    FeishuTenantStatus
	SortOrder int
}

// FeishuApp 是调用飞书 OAuth 接口所需的应用凭证。
type FeishuApp struct {
	AppID     string
	AppSecret string
}

func IsValidFeishuTenantID(value string) bool {
	return feishuTenantIDPattern.MatchString(value)
}

func IsValidFeishuTenantAppID(value string) bool {
	return feishuTenantAppIDPattern.MatchString(value)
}

func IsValidFeishuTenantStatus(status FeishuTenantStatus) bool {
	return status == FeishuTenantStatusActive || status == FeishuTenantStatusDisabled
}
