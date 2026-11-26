package domain

import (
	"errors"
	"fmt"
)

var (
	ErrFeishuAuthNotConfigured = errors.New("feishu auth not configured")
	ErrFeishuAuthorization     = errors.New("feishu authorization failed")
	ErrInvalidFeishuSearch     = errors.New("invalid feishu search request")
)

type FeishuToken struct {
	AccessToken           string
	ExpiresIn             int
	RefreshToken          string
	RefreshTokenExpiresIn int
	TokenType             string
	Scope                 string
}

type FeishuAPIError struct {
	Code     int
	Endpoint string
	Msg      string
}

func (e *FeishuAPIError) Error() string {
	return fmt.Sprintf("%s: endpoint=%s code=%d msg=%s", ErrFeishuAuthorization, e.Endpoint, e.Code, e.Msg)
}

func (e *FeishuAPIError) Unwrap() error {
	return ErrFeishuAuthorization
}

type FeishuUser struct {
	UnionID         string
	OpenID          string
	UserID          string
	Name            string
	EnName          string
	AvatarURL       string
	AvatarThumb     string
	AvatarMiddle    string
	AvatarBig       string
	Email           string
	EnterpriseEmail string
	Mobile          string
	TenantKey       string
	EmployeeNo      string
}

type FeishuUserSearchResult struct {
	UnionID       string
	OpenID        string
	UserID        string
	Name          string
	EnName        string
	AvatarURL     string
	Email         string
	Mobile        string
	DepartmentIDs []string
}

type FeishuUserSearchPage struct {
	Users     []FeishuUserSearchResult
	HasMore   bool
	PageToken string
}

// ExternalUser 把飞书登录拿到的用户信息转换成登录身份，issuer 是登录所用的飞书 App ID。
func (u FeishuUser) ExternalUser(appID string) ExternalUser {
	return ExternalUser{
		Provider:  IdentityProviderFeishu,
		Issuer:    appID,
		Subject:   u.OpenID,
		Name:      firstNonEmptyString(u.Name, u.EnName, u.OpenID),
		Email:     firstNonEmptyString(u.EnterpriseEmail, u.Email),
		AvatarURL: firstNonEmptyString(u.AvatarURL, u.AvatarMiddle, u.AvatarThumb, u.AvatarBig),
		Profile: compactProfile(map[string]string{
			"unionId":    u.UnionID,
			"userId":     u.UserID,
			"enName":     u.EnName,
			"employeeNo": u.EmployeeNo,
			"tenantKey":  u.TenantKey,
		}),
	}
}

// ExternalUser 把通讯录搜索结果转换成登录身份。open_id 按应用隔离，所以 issuer 必须是搜索所用的同一个飞书 App ID。
func (r FeishuUserSearchResult) ExternalUser(appID string) ExternalUser {
	return ExternalUser{
		Provider:  IdentityProviderFeishu,
		Issuer:    appID,
		Subject:   r.OpenID,
		Name:      firstNonEmptyString(r.Name, r.EnName, r.OpenID),
		Email:     r.Email,
		AvatarURL: r.AvatarURL,
		Profile: compactProfile(map[string]string{
			"unionId": r.UnionID,
			"userId":  r.UserID,
			"enName":  r.EnName,
		}),
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func compactProfile(profile map[string]string) map[string]string {
	for key, value := range profile {
		if value == "" {
			delete(profile, key)
		}
	}
	return profile
}
