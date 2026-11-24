package domain

import (
	"errors"
	"time"
)

var (
	ErrBuildRecordNotFound = errors.New("build record not found")
	ErrInvalidBuildInput   = errors.New("invalid build request")
	ErrNoBuildRecord       = errors.New("no build record available")
	// ErrBuildLeaseLost 表示构建已不归当前 worker 所有：租约过期后被其他 worker 接管，或记录已结束。
	ErrBuildLeaseLost = errors.New("build lease lost")
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
	ID string
	// Number 是模块内单调递增的构建号。
	Number          int64
	ApplicationSlug string
	ApplicationName string
	ModuleID        string
	ModuleName      string
	// ModuleKind、CommonModuleID 来自模块当前配置，worker 据此决定是否注入所属 Common 模块的产物。
	ModuleKind         ModuleKind
	CommonModuleID     string
	Environment        string
	EnvironmentName    string
	CodeSourceProvider CodeSourceProvider
	Repository         ModuleRepository
	DockerfilePath     string
	BuildContext       string
	// BuildMode、ModuleBuildAdapterID、ModuleBuildAdapterParams 来自模块当前配置，worker 据此选择构建方式。
	BuildMode                BuildMode
	ModuleBuildAdapterID     string
	ModuleBuildAdapterParams map[string]string
	// CIJob、CIArtifactPath 来自模块当前配置，gitlab_ci 模式下 worker 据此取产物。
	CIJob          string
	CIArtifactPath string
	// CIPipelineID、CIPipelineURL 是 gitlab_ci 构建在代码源上创建的 pipeline，创建后立即写回，worker 被接管后据此续接。
	CIPipelineID  int64
	CIPipelineURL string
	// BuildAdapterID、BuildAdapterVersion、RenderedDockerfileSHA256 是 worker 实际使用的适配器版本与渲染结果，Dockerfile 构建为空。
	BuildAdapterID           string
	BuildAdapterVersion      *int
	RenderedDockerfileSHA256 string
	// DependencyBuilds 是 worker 注入到构建上下文的依赖构建，没有注入时为空。
	DependencyBuilds []BuildDependency
	Branch           string
	CommitSHA        string
	RuntimeVersion   string
	Platforms        []string
	// ProvidedCapabilities、RequiredCapabilities 来自产物的 forge.json，没有声明时为空。
	ProvidedCapabilities     Capabilities
	RequiredCapabilities     Capabilities
	DefaultBranch            string
	StorageConfigurationID   string
	StorageConfigurationName string
	StorageBucket            string
	StorageEndpoint          string
	StorageRegion            string
	StoragePublicBaseURL     string
	StorageAccessKeyID       string
	StorageSecretAccessKey   string
	StorageForcePathStyle    bool
	ArtifactKey              string
	ArtifactURL              string
	ManifestKey              string
	// Signature 是产物文件列表的签名，未签名时各字段为空，见 docs/artifact-signing.md。
	Signature      ArtifactSignature
	FileCount      int
	TotalSizeBytes int64
	ArtifactTree   ArtifactTree
	Status         BuildStatus
	TriggerID      string
	TriggerType    BuildTriggerType
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

// 依赖产物注入的目录约定：{构建上下文}/.forge/deps/common/index.json 与 {runtimeVersion}/{platform}/...。
const (
	// BuildDependenciesDir 是依赖产物在 .forge 下的目录，构建适配器的辅助文件不能使用。
	BuildDependenciesDir = "deps"
	// BuildCommonDependencyDir 是所属 Common 模块产物在依赖目录下的子目录。
	BuildCommonDependencyDir = "common"
	// BuildDependencyIndexFileName 是依赖目录下描述注入内容的索引文件。
	BuildDependencyIndexFileName = "index.json"
	// BuildDependenciesDirArg 是传给 docker build 的 build-arg 名，值为相对构建上下文的依赖目录。
	BuildDependenciesDirArg = "FORGE_DEPS_DIR"
)

// BuildDependency 是一次构建注入的依赖构建。落库只保存 ModuleID、RuntimeVersion、BuildRecordID，
// ModuleName 和 BuildNumber 在读取时按 id 关联得到，依赖构建或模块不存在时为空。
type BuildDependency struct {
	ModuleID       string
	ModuleName     string
	RuntimeVersion string
	BuildRecordID  string
	BuildNumber    int64
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

// BuildQueueStats 是构建队列的即时状态，供扩缩容判断使用。
type BuildQueueStats struct {
	// Waiting 是等待 worker 领取的构建数，包括租约已过期、等待其他 worker 接管的构建。
	Waiting int
	// OldestWaitSeconds 是等待最久的构建已排队的秒数，没有等待中的构建时为 0。
	OldestWaitSeconds float64
	// Running 是租约有效、正在执行的构建数。
	Running int
	// ActiveWorkers 是持有有效租约的 worker 数。
	ActiveWorkers int
}

type BuildRecordCompletion struct {
	// Signature 是产物文件列表的签名，应用没有 active 签名密钥时为空。
	Signature            ArtifactSignature
	CommitSHA            string
	RuntimeVersion       string
	Platforms            []string
	ProvidedCapabilities Capabilities
	RequiredCapabilities Capabilities
	ArtifactKey          string
	ArtifactURL          string
	ManifestKey          string
	FileCount            int
	TotalSizeBytes       int64
	ErrorMessage         string
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
