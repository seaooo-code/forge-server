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

const (
	ReleaseGrayStrategyTargeted   ReleaseGrayStrategy = "targeted"
	ReleaseGrayStrategyPercentage ReleaseGrayStrategy = "percentage"
)

const (
	ReleaseStatusActive   ReleaseStatus = "active"
	ReleaseStatusInactive ReleaseStatus = "inactive"
)

const (
	ReleaseRouteTypeDefault    ReleaseRouteType = "default"
	ReleaseRouteTypeIdentifier ReleaseRouteType = "identifier"
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
	Identifier      string
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
	RouteType       ReleaseRouteType
	Identifier      string
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
	ExpiresAt       *time.Time
	Identifier      string
	Description     string
	CreatedByUserID string
}

type ReleaseGrayPercentageUpdateInput struct {
	ID              string
	GrayPercentage  int
	UpdatedByUserID string
}

type ReleaseInactivateInput struct {
	ID              string
	UpdatedByUserID string
}

type ReleaseResolveInput struct {
	ModuleID    string
	Environment string
	Identifier  string
	UserID      string
}

func IsValidReleaseType(releaseType ReleaseType) bool {
	return releaseType == ReleaseTypeFull || releaseType == ReleaseTypeGray
}

func IsValidReleaseGrayStrategy(strategy ReleaseGrayStrategy) bool {
	return strategy == ReleaseGrayStrategyTargeted || strategy == ReleaseGrayStrategyPercentage
}
