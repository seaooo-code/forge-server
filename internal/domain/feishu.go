package domain

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrFeishuAuthNotConfigured = errors.New("feishu auth not configured")
	ErrFeishuAuthorization     = errors.New("feishu authorization failed")
	ErrInvalidFeishuSearch     = errors.New("invalid feishu search request")
	ErrFeishuSessionExpired    = errors.New("feishu session expired")
	ErrFeishuSessionNotFound   = errors.New("feishu session not found")
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
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LastLoginAt     time.Time
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

type FeishuAuthResult struct {
	SessionExpiresAt time.Time
	SessionToken     string
	Token            FeishuToken
	User             FeishuUser
}

type FeishuAuthSession struct {
	SessionTokenHash      string
	UnionID               string
	AccessToken           string
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
	TokenType             string
	Scope                 string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	LastSeenAt            time.Time
	User                  FeishuUser
}
