package domain

import (
	"errors"
	"time"
)

var (
	ErrInvalidReleaseInput  = errors.New("invalid release request")
	ErrReleaseNotFound      = errors.New("release not found")
	ErrReleaseRouteNotFound = errors.New("release route not found")
)

type ReleaseType string
type ReleaseStatus string
type ReleaseRouteType string
type ReleaseRouteStatus string

const (
	ReleaseTypeFull     ReleaseType = "full"
	ReleaseTypeTargeted ReleaseType = "targeted"
)

const (
	ReleaseStatusActive   ReleaseStatus = "active"
	ReleaseStatusInactive ReleaseStatus = "inactive"
	ReleaseStatusRevoked  ReleaseStatus = "revoked"
)

const (
	ReleaseRouteTypeDefault    ReleaseRouteType = "default"
	ReleaseRouteTypeIdentifier ReleaseRouteType = "identifier"
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
	Identifier      string
	Description     string
	CreatedByUserID string
}

type ReleaseResolveInput struct {
	ModuleID    string
	Environment string
	Identifier  string
}

func IsValidReleaseType(releaseType ReleaseType) bool {
	return releaseType == ReleaseTypeFull || releaseType == ReleaseTypeTargeted
}
