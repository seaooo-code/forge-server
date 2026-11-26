package domain

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"time"
)

var (
	ErrTenantNotFound      = errors.New("tenant not found")
	ErrInvalidTenantInput  = errors.New("invalid tenant request")
	ErrTenantIDConflict    = errors.New("tenant id already exists")
	ErrFeishuAppIDConflict = errors.New("feishu app id already used by another tenant")
	// ErrTenantLastActive 表示操作会让平台没有任何可登录的租户。
	ErrTenantLastActive = errors.New("at least one tenant must stay active")
	// ErrTenantRequired 表示有多个可用租户，但登录请求没有指定租户。
	ErrTenantRequired = errors.New("tenant required")
	// ErrInvalidFeishuRedirectURI 表示前端传入的登录回调地址不合法。
	ErrInvalidFeishuRedirectURI = errors.New("invalid feishu redirect uri")
	// ErrPlatformSetupCompleted 表示平台已经有租户，首次初始化入口已关闭。
	ErrPlatformSetupCompleted = errors.New("platform setup already completed")
	// ErrInvalidSetupToken 表示首次初始化令牌不正确。
	ErrInvalidSetupToken = errors.New("invalid setup token")
	// ErrTenantScopeMissing 表示请求 context 里既没有租户范围也没有系统范围，查询拒绝执行。
	ErrTenantScopeMissing = errors.New("tenant scope missing")
)

// FeishuCallbackPath 是前端处理飞书授权回调的页面路径，回调地址只允许 {前端 origin}/login/feishu/callback。
const FeishuCallbackPath = "/login/feishu/callback"

type TenantStatus string

const (
	TenantStatusActive   TenantStatus = "active"
	TenantStatusDisabled TenantStatus = "disabled"
)

const (
	MaxTenantNameLength      = 64
	MaxFeishuAppSecretLength = 256
)

var (
	tenantIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)
	feishuAppIDPattern = regexp.MustCompile(`^cli_[A-Za-z0-9]{8,64}$`)
)

// Tenant 是相互隔离的一组用户、配置和业务数据。每个租户用一个飞书自建应用登录。
// FeishuAppSecret 只在服务端内部使用，接口不返回。
type Tenant struct {
	ID              string
	Name            string
	Status          TenantStatus
	SortOrder       int
	FeishuAppID     string
	FeishuAppSecret string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TenantSummary 是会话里展示租户时使用的字段。
type TenantSummary struct {
	ID   string
	Name string
}

// TenantInput 是新建或编辑租户的输入。编辑时 FeishuAppID 不能修改；FeishuAppSecret 为空表示沿用原值。
type TenantInput struct {
	ID              string
	Name            string
	FeishuAppID     string
	FeishuAppSecret string
	Status          TenantStatus
	SortOrder       int
}

// FeishuApp 是调用飞书 OAuth 接口所需的应用凭证。
type FeishuApp struct {
	AppID     string
	AppSecret string
}

func IsValidTenantID(value string) bool {
	return tenantIDPattern.MatchString(value)
}

func IsValidFeishuAppID(value string) bool {
	return feishuAppIDPattern.MatchString(value)
}

func IsValidTenantStatus(status TenantStatus) bool {
	return status == TenantStatusActive || status == TenantStatusDisabled
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

// TenantScope 是一次请求可以访问的数据范围：某个租户，或者不限租户的系统范围。
type TenantScope struct {
	TenantID string
	System   bool
}

type tenantScopeKey struct{}

// WithTenant 把当前租户放进 context，之后的查询只能读写这个租户的数据。
func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantScopeKey{}, TenantScope{TenantID: tenantID})
}

// WithSystemScope 用于后台任务、客户端接口和 webhook：查询不按租户过滤，由调用方自己确定数据归属。
func WithSystemScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, tenantScopeKey{}, TenantScope{System: true})
}

// TenantScopeFromContext 读取当前范围。既没有租户也不是系统范围时返回 ErrTenantScopeMissing。
func TenantScopeFromContext(ctx context.Context) (TenantScope, error) {
	scope, ok := ctx.Value(tenantScopeKey{}).(TenantScope)
	if !ok || (!scope.System && scope.TenantID == "") {
		return TenantScope{}, ErrTenantScopeMissing
	}
	return scope, nil
}

// TenantIDFromContext 返回当前租户 ID，写入归属租户的数据时使用。系统范围或没有范围时返回 ErrTenantScopeMissing。
func TenantIDFromContext(ctx context.Context) (string, error) {
	scope, err := TenantScopeFromContext(ctx)
	if err != nil {
		return "", err
	}
	if scope.System {
		return "", ErrTenantScopeMissing
	}
	return scope.TenantID, nil
}
