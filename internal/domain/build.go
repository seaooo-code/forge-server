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
	BuildTriggerTypeManual  BuildTriggerType = "manual"
	BuildTriggerTypeWebhook BuildTriggerType = "webhook"
)

type BuildRecord struct {
	ID                       string
	ApplicationName          string
	ModuleID                 string
	ModuleName               string
	Environment              string
	EnvironmentName          string
	CodeSourceProvider       CodeSourceProvider
	Repository               ModuleRepository
	DockerfilePath           string
	BuildContext             string
	Branch                   string
	CommitSHA                string
	RuntimeVersion           string
	Platforms                []string
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
	ManifestKey              string
	FileCount                int
	TotalSizeBytes           int64
	ArtifactTree             ArtifactTree
	Status                   BuildStatus
	TriggerID                string
	TriggerType              BuildTriggerType
	// CreatedByUserID 为空表示历史构建或创建人已删除。
	CreatedByUserID string
	CreatedBy       *UserSummary
	ErrorMessage    string
	WorkerID        string
	QueuedAt        time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type BuildRecordInput struct {
	ModuleID    string
	Environment string
	Branch      string
	CommitSHA   string
	TriggerID   string
	TriggerType BuildTriggerType
	// CreatedByUserID 手动构建为当前登录用户，webhook 构建为代码源连接创建人。
	CreatedByUserID string
}

type BuildRecordFilters struct {
	ApplicationID string
	Environment   string
	ModuleID      string
	Pagination    Pagination
	Status        BuildStatus
	TriggerType   BuildTriggerType
}

type BuildRecordSummary struct {
	Failed    int
	Pending   int
	Running   int
	Succeeded int
	Total     int
}

type BuildRecordCompletion struct {
	CommitSHA      string
	RuntimeVersion string
	Platforms      []string
	ArtifactKey    string
	ArtifactURL    string
	ManifestKey    string
	FileCount      int
	TotalSizeBytes int64
	ErrorMessage   string
}

type ArtifactManifest struct {
	Version        int                    `json:"version"`
	ArtifactKey    string                 `json:"artifactKey"`
	GeneratedAt    time.Time              `json:"generatedAt"`
	FileCount      int                    `json:"fileCount"`
	TotalSizeBytes int64                  `json:"totalSizeBytes"`
	Files          []ArtifactManifestFile `json:"files"`
}

type ArtifactManifestFile struct {
	Path        string `json:"path"`
	ObjectKey   string `json:"objectKey"`
	SizeBytes   int64  `json:"sizeBytes"`
	ContentType string `json:"contentType"`
	SHA256      string `json:"sha256"`
	// Digests 按配置 FORGE_ARTIFACT_DIGESTS 计算的摘要，键为算法名（sha256、md5）。旧清单没有该字段。
	Digests map[string]string `json:"digests,omitempty"`
}

type ArtifactTree struct {
	FileCount      int                `json:"fileCount"`
	TotalSizeBytes int64              `json:"totalSizeBytes"`
	Nodes          []ArtifactTreeNode `json:"nodes"`
}

type ArtifactTreeNode struct {
	Name        string             `json:"name"`
	Path        string             `json:"path"`
	Type        string             `json:"type"`
	SizeBytes   int64              `json:"sizeBytes"`
	ObjectKey   string             `json:"objectKey,omitempty"`
	ContentType string             `json:"contentType,omitempty"`
	SHA256      string             `json:"sha256,omitempty"`
	Children    []ArtifactTreeNode `json:"children,omitempty"`
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
	return triggerType == BuildTriggerTypeManual || triggerType == BuildTriggerTypeWebhook
}
