package domain

import (
	"errors"
	"time"
)

var (
	ErrInvalidReleaseInput  = errors.New("invalid release request")
	ErrReleaseAlreadyActive = errors.New("release already active")
	ErrReleaseNotFound      = errors.New("release not found")
	ErrReleaseRouteNotFound = errors.New("release route not found")
)

type ReleaseType string
type ReleaseGrayStrategy string
type ReleaseStatus string
type ReleaseRouteType string
type ReleaseRouteStatus string

const (
	ReleaseTypeFull ReleaseType = "full"
	ReleaseTypeGray ReleaseType = "gray"
)

// 按 identifier 定向灰度（targeted）已由提测单取代，见 docs/test-plans.md；历史发布记录里仍可能读到 "targeted"。
const (
	ReleaseGrayStrategyPercentage ReleaseGrayStrategy = "percentage"
)

const (
	ReleaseStatusActive   ReleaseStatus = "active"
	ReleaseStatusInactive ReleaseStatus = "inactive"
)

const (
	ReleaseRouteTypeDefault    ReleaseRouteType = "default"
	ReleaseRouteTypePercentage ReleaseRouteType = "percentage"
)

const (
	ReleaseRouteStatusActive   ReleaseRouteStatus = "active"
	ReleaseRouteStatusInactive ReleaseRouteStatus = "inactive"
)

type ReleaseRecord struct {
	ID              string
	ApplicationName string
	ModuleID        string
	ModuleName      string
	Environment     string
	EnvironmentName string
	BuildRecordID   string
	RuntimeVersion  string
	ReleaseType     ReleaseType
	GrayStrategy    ReleaseGrayStrategy
	GrayPercentage  int
	ExpiresAt       *time.Time
	Status          ReleaseStatus
	ArtifactKey     string
	ArtifactURL     string
	Description     string
	CreatedByUserID string
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ReleaseRoute struct {
	ID              string
	ApplicationName string
	ModuleID        string
	ModuleName      string
	Environment     string
	EnvironmentName string
	ReleaseID       string
	BuildRecordID   string
	RuntimeVersion  string
	RouteType       ReleaseRouteType
	GrayStrategy    ReleaseGrayStrategy
	GrayPercentage  int
	Status          ReleaseRouteStatus
	ArtifactKey     string
	ArtifactURL     string
	CreatedByUserID string
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ReleaseCreateInput struct {
	BuildRecordID   string
	ReleaseType     ReleaseType
	GrayStrategy    ReleaseGrayStrategy
	GrayPercentage  int
	Description     string
	CreatedByUserID string
}

type ReleaseRecordFilters struct {
	ApplicationID string
	Environment   string
	ModuleID      string
	Pagination    Pagination
	ReleaseType   ReleaseType
	Status        ReleaseStatus
}

type ReleaseRecordSummary struct {
	Active   int
	Inactive int
	Total    int
}

type ReleaseGrayPercentageUpdateInput struct {
	ID              string
	GrayPercentage  int
	UpdatedByUserID string
}

type ReleaseResolveInput struct {
	ModuleID    string
	Environment string
	// RuntimeVersion 为空时不限制运行时版本，按创建时间取最新路由；非空时只在该运行时版本下解析。
	RuntimeVersion string
	UserID         string
}

func IsValidReleaseType(releaseType ReleaseType) bool {
	return releaseType == ReleaseTypeFull || releaseType == ReleaseTypeGray
}

func IsValidReleaseStatus(status ReleaseStatus) bool {
	return status == ReleaseStatusActive || status == ReleaseStatusInactive
}

func IsValidReleaseGrayStrategy(strategy ReleaseGrayStrategy) bool {
	return strategy == ReleaseGrayStrategyPercentage
}
