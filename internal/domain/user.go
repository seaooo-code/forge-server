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
