package domain

import "time"

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
