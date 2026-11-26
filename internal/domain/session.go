package domain

import (
	"errors"
	"time"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
	// ErrTenantSwitchForbidden 表示非 SuperAdmin 尝试切换到其他租户。
	ErrTenantSwitchForbidden = errors.New("tenant switch forbidden")
)

// AuthSession 是落库的控制台会话。Provider* 是登录方式签发的 token，飞书登录用它刷新会话、搜索通讯录。
type AuthSession struct {
	TokenHash                     string
	UserID                        string
	IdentityID                    string
	ActiveTenantID                string
	ProviderAccessToken           string
	ProviderAccessTokenExpiresAt  time.Time
	ProviderRefreshToken          string
	ProviderRefreshTokenExpiresAt time.Time
	ProviderTokenType             string
	ProviderScope                 string

	// 以下字段读取会话时关联得到。
	User               User
	Identity           UserIdentity
	HomeTenant         TenantSummary
	HomeTenantStatus   TenantStatus
	ActiveTenant       TenantSummary
	ActiveTenantStatus TenantStatus
}

// ConsoleSession 是鉴权通过后的会话，handler 从这里取当前用户和当前租户。
type ConsoleSession struct {
	SessionToken string
	ExpiresAt    time.Time
	User         User
	Identity     UserIdentity
	// HomeTenant 是用户所属租户；ActiveTenant 是当前所在租户，只有 SuperAdmin 切换后才和 HomeTenant 不同。
	HomeTenant   TenantSummary
	ActiveTenant TenantSummary
	Roles        []Role
	Capabilities TenantCapabilities
	// ProviderAccessToken 只在服务端使用，不返回给前端。
	ProviderAccessToken string
	ProviderToken       FeishuToken
}

// InHomeTenant 表示当前所在租户就是用户所属租户。只有这时才能用登录方式的 token 搜索通讯录。
func (s ConsoleSession) InHomeTenant() bool {
	return s.ActiveTenant.ID == s.HomeTenant.ID
}
