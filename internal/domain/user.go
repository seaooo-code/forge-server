package domain

import (
	"errors"
	"time"
)

var (
	ErrUserNotFound = errors.New("user not found")
	// ErrUserTenantMismatch 表示引用的用户不属于当前租户，例如把其他租户的人设为负责人。
	ErrUserTenantMismatch = errors.New("user does not belong to current tenant")
)

// User 是控制台用户。用户只属于一个租户，ID 由 Forge 生成，和登录方式无关。
type User struct {
	ID        string
	TenantID  string
	Name      string
	Email     string
	AvatarURL string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UserSummary 是接口里展示创建人等关联用户时使用的最小字段集合。
type UserSummary struct {
	ID        string
	Name      string
	AvatarURL string
}

type IdentityProvider string

const IdentityProviderFeishu IdentityProvider = "feishu"

// UserIdentity 是用户的一种登录身份。(Provider, Issuer, Subject) 全平台唯一：
// 飞书身份的 Issuer 是飞书 App ID，Subject 是 open_id。
type UserIdentity struct {
	ID       string
	UserID   string
	Provider IdentityProvider
	Issuer   string
	Subject  string
	// Profile 是登录方式自己的资料，例如飞书的 unionId、userId、enName。
	Profile map[string]string
}

// ExternalUser 是登录方式或通讯录返回的一个人，用来查找或新建对应的 Forge 用户。
type ExternalUser struct {
	Provider  IdentityProvider
	Issuer    string
	Subject   string
	Name      string
	Email     string
	AvatarURL string
	Profile   map[string]string
}
