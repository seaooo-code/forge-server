package domain

import "errors"

var (
	// ErrCINotSupported 表示代码源不支持把构建转交给 CI。
	ErrCINotSupported = errors.New("code source does not support ci builds")
	// ErrCIPipelineNotFound 表示 pipeline 已不存在（被删除，或属于其他项目）。
	ErrCIPipelineNotFound = errors.New("ci pipeline not found")
	// ErrCIArtifactsNotFound 表示 job 没有可下载的 artifacts（未配置、已过期或已被清理）。
	ErrCIArtifactsNotFound = errors.New("ci job artifacts not found")
)

// CIStatus 是 CI pipeline 或 job 的状态，取值沿用 GitLab 的状态名。
type CIStatus string

const (
	CIStatusSuccess  CIStatus = "success"
	CIStatusFailed   CIStatus = "failed"
	CIStatusCanceled CIStatus = "canceled"
	CIStatusSkipped  CIStatus = "skipped"
)

// IsTerminal 判断状态是否已经结束，不会再变化。
func (s CIStatus) IsTerminal() bool {
	return s == CIStatusSuccess || s == CIStatusFailed || s == CIStatusCanceled || s == CIStatusSkipped
}

// CIPipeline 是代码源上的一次 CI 运行。
type CIPipeline struct {
	ID     int64
	URL    string
	SHA    string
	Status CIStatus
}

// CIJob 是 pipeline 里的一个 job。
type CIJob struct {
	ID     int64
	Name   string
	Status CIStatus
	URL    string
}

// CIVariable 是触发 pipeline 时传入的变量。
type CIVariable struct {
	Key   string
	Value string
}
