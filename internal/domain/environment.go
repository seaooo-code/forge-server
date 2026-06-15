package domain

import (
	"errors"
	"strings"
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
	UpdatedAt                time.Time
}

type EnvironmentInput struct {
	Code                   string
	Name                   string
	Status                 EnvironmentStatus
	Tone                   string
	Description            string
	StorageConfigurationID string
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

func ParseEnvironmentStatus(value string) (EnvironmentStatus, bool) {
	switch strings.TrimSpace(value) {
	case "", string(EnvironmentStatusHealthy), "健康":
		return EnvironmentStatusHealthy, true
	case string(EnvironmentStatusGray), "灰度中":
		return EnvironmentStatusGray, true
	default:
		return "", false
	}
}
