package domain

import (
	"errors"
	"net/url"
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
	// ErrInvalidFeishuRedirectURI 表示前端传入的登录回调地址不合法。
	ErrInvalidFeishuRedirectURI = errors.New("invalid feishu redirect uri")
	// ErrPlatformSetupCompleted 表示平台已经有租户，首次初始化入口已关闭。
	ErrPlatformSetupCompleted = errors.New("platform setup already completed")
	// ErrInvalidSetupToken 表示首次初始化令牌不正确。
	ErrInvalidSetupToken = errors.New("invalid setup token")
)

// FeishuCallbackPath 是前端处理飞书授权回调的页面路径，回调地址只允许 {前端 origin}/login/feishu/callback。
const FeishuCallbackPath = "/login/feishu/callback"

type FeishuTenantStatus string

const (
	FeishuTenantStatusActive   FeishuTenantStatus = "active"
	FeishuTenantStatusDisabled FeishuTenantStatus = "disabled"
)

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

// IsValidFeishuRedirectURI 校验前端传入的回调地址：只能是 http/https，有主机名，路径必须是 FeishuCallbackPath，
// 不带用户信息、查询参数和片段。地址最终还要在飞书应用的重定向白名单里，飞书会再校验一次。
func IsValidFeishuRedirectURI(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.Host != "" &&
		parsed.User == nil &&
		parsed.Path == FeishuCallbackPath &&
		parsed.RawQuery == "" &&
		parsed.Fragment == "" &&
		!parsed.ForceQuery
}

func IsValidFeishuTenantStatus(status FeishuTenantStatus) bool {
	return status == FeishuTenantStatusActive || status == FeishuTenantStatusDisabled
}
