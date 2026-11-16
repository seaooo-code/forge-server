package domain

import (
	"strings"
	"time"
)

type User struct {
	ID            string
	Name          string
	Email         string
	AvatarURL     string
	DepartmentIDs []string
	FeishuUnionID string
	FeishuOpenID  string
	FeishuUserID  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// UserSummary 是接口里展示创建人等关联用户时使用的最小字段集合。
type UserSummary struct {
	ID        string
	Name      string
	AvatarURL string
}

type UserInput struct {
	ID            string
	Name          string
	Email         string
	AvatarURL     string
	DepartmentIDs []string
	FeishuUnionID string
	FeishuOpenID  string
	FeishuUserID  string
}

func ConsoleUserIDFromFeishu(user FeishuUser) string {
	for _, value := range []string{user.OpenID, user.UserID, user.UnionID} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
