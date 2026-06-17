package domain

import (
	"errors"
	"time"
)

var (
	ErrBuildRecordNotFound = errors.New("build record not found")
	ErrInvalidBuildInput   = errors.New("invalid build request")
	ErrNoBuildRecord       = errors.New("no build record available")
)

type BuildStatus string
type BuildTriggerType string

const (
	BuildStatusPending   BuildStatus = "pending"
	BuildStatusRunning   BuildStatus = "running"
	BuildStatusSucceeded BuildStatus = "succeeded"
	BuildStatusFailed    BuildStatus = "failed"
)

const (
	BuildTriggerTypeManual   BuildTriggerType = "manual"
	BuildTriggerTypePipeline BuildTriggerType = "pipeline"
)

type BuildRecord struct {
	ID                       string
	ApplicationName          string
	ModuleID                 string
	ModuleName               string
	Environment              string
	EnvironmentName          string
	RepositoryProvider       CodeSourceProvider
	RepositoryID             int64
	RepositoryInstallationID int64
	RepositoryFullName       string
	RepositoryCloneURL       string
	Branch                   string
	DefaultBranch            string
	StorageConfigurationID   string
	StorageConfigurationName string
	StorageBucket            string
	StorageEndpoint          string
	StorageRegion            string
	StoragePublicBaseURL     string
	StorageAccessKeyID       string
	StorageSecretAccessKey   string
	ArtifactKey              string
	ArtifactURL              string
	Status                   BuildStatus
	TriggerID                string
	TriggerType              BuildTriggerType
	ErrorMessage             string
	WorkerID                 string
	QueuedAt                 time.Time
	StartedAt                *time.Time
	FinishedAt               *time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type BuildRecordInput struct {
	ModuleID    string
	Environment string
	Branch      string
	TriggerID   string
	TriggerType BuildTriggerType
}

type BuildRecordSummary struct {
	Failed    int
	Pending   int
	Running   int
	Succeeded int
	Total     int
}

type BuildRecordCompletion struct {
	ArtifactKey  string
	ArtifactURL  string
	ErrorMessage string
}

func IsValidBuildStatus(status BuildStatus) bool {
	switch status {
	case BuildStatusPending, BuildStatusRunning, BuildStatusSucceeded, BuildStatusFailed:
		return true
	default:
		return false
	}
}

func IsValidBuildTriggerType(triggerType BuildTriggerType) bool {
	return triggerType == BuildTriggerTypeManual || triggerType == BuildTriggerTypePipeline
}
