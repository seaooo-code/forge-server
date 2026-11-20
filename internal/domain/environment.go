package domain

import (
	"errors"
	"time"
)

var (
	ErrEnvironmentNotFound     = errors.New("environment not found")
	ErrInvalidEnvironmentInput = errors.New("invalid environment request")
)

type EnvironmentStatus string

const (
	EnvironmentStatusHealthy EnvironmentStatus = "healthy"
	EnvironmentStatusGray    EnvironmentStatus = "gray"
)

type Environment struct {
	Code                     string
	Name                     string
	Status                   EnvironmentStatus
	Tone                     string
	Description              string
	StorageConfigurationID   string
	StorageConfigurationName string
	// AllowTestPlans 表示该环境里的提测单会在 manifest 解析中生效，默认关闭，见 docs/test-plans.md。
	AllowTestPlans bool
	UpdatedAt      time.Time
}

type EnvironmentInput struct {
	Code                   string
	Name                   string
	Status                 EnvironmentStatus
	Tone                   string
	Description            string
	StorageConfigurationID string
	AllowTestPlans         bool
}

func (s EnvironmentStatus) Label() string {
	switch s {
	case EnvironmentStatusHealthy:
		return "健康"
	case EnvironmentStatusGray:
		return "灰度中"
	default:
		return string(s)
	}
}

func IsValidEnvironmentStatus(status EnvironmentStatus) bool {
	switch status {
	case EnvironmentStatusHealthy, EnvironmentStatusGray:
		return true
	default:
		return false
	}
}
