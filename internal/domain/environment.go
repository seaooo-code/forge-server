package domain

import (
	"errors"
	"time"
)

var ErrEnvironmentNotFound = errors.New("environment not found")

type EnvironmentStatus string

const (
	EnvironmentStatusHealthy EnvironmentStatus = "healthy"
	EnvironmentStatusGray    EnvironmentStatus = "gray"
)

var validEnvironmentCodes = map[string]struct{}{
	"dev":        {},
	"preProduct": {},
	"preview":    {},
	"product":    {},
	"staging":    {},
}

type Environment struct {
	Code               string
	ShortCode          string
	Name               string
	Status             EnvironmentStatus
	Tone               string
	Description        string
	CurrentCombination string
	TrafficLabel       string
	UpdatedAt          time.Time
}

type EnvironmentGuardrail struct {
	ID     string
	Label  string
	Value  string
	Status string
}

type EnvironmentTimelineEvent struct {
	ID          string
	DisplayTime string
	Action      string
	Operator    string
	Status      string
	Detail      string
}

type EnvironmentDetail struct {
	Environment    Environment
	ID             string
	Endpoint       string
	CDNDomain      string
	BranchPolicy   string
	ReleasePolicy  string
	CurrentRelease string
	BundleCount    int
	TotalSize      string
	HealthScore    string
	Guardrails     []EnvironmentGuardrail
	Timeline       []EnvironmentTimelineEvent
}

func IsValidEnvironmentCode(code string) bool {
	_, ok := validEnvironmentCodes[code]
	return ok
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
