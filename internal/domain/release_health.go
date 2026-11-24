package domain

import (
	"errors"
	"time"
)

var (
	ErrInvalidLaunchReport       = errors.New("invalid launch report")
	ErrInvalidReleaseHealthInput = errors.New("invalid release health request")
)

// LaunchOutcome 是客户端对一个模块一次启动的判定，见 docs/release-health.md 第 3.1 节。
type LaunchOutcome string

const (
	LaunchOutcomeLaunched       LaunchOutcome = "launched"
	LaunchOutcomeLaunchFailed   LaunchOutcome = "launch_failed"
	LaunchOutcomeVerifyFailed   LaunchOutcome = "verify_failed"
	LaunchOutcomeDownloadFailed LaunchOutcome = "download_failed"
)

func IsValidLaunchOutcome(outcome LaunchOutcome) bool {
	switch outcome {
	case LaunchOutcomeLaunched, LaunchOutcomeLaunchFailed, LaunchOutcomeVerifyFailed, LaunchOutcomeDownloadFailed:
		return true
	}
	return false
}

// 启动结果上报的限制。
const (
	MaxLaunchReportEvents      = 50
	MaxLaunchReportReasonBytes = 64
	LaunchReportMaxAge         = 24 * time.Hour
	LaunchReportMaxClockSkew   = 5 * time.Minute
	// ReleaseHealthBucketSize 是健康统计的聚合粒度。
	ReleaseHealthBucketSize = 5 * time.Minute
	// ReleaseHealthRetentionDays 是健康统计桶的保留天数。
	ReleaseHealthRetentionDays = 30
)

type LaunchReport struct {
	ApplicationSlug string
	Environment     string
	Platform        string
	RuntimeVersion  string
	Channel         string
	UserID          string
	Events          []LaunchReportEvent
}

type LaunchReportEvent struct {
	ModuleID      string
	BuildRecordID string
	ReleaseID     string
	Outcome       LaunchOutcome
	Reason        string
	OccurredAt    time.Time
}

// ReleaseHealthBucketKey 是健康统计的聚合维度。RuntimeVersion 不在维度里：发布已经限定了运行时版本。
type ReleaseHealthBucketKey struct {
	BucketStart   time.Time
	ApplicationID string
	Environment   string
	ModuleID      string
	BuildRecordID string
	ReleaseID     string
	Channel       string
	Platform      string
	Outcome       LaunchOutcome
	Reason        string
}

// ReleaseHealthBucketIncrement 是一次写库要累加的用户数和事件数。
type ReleaseHealthBucketIncrement struct {
	ReleaseHealthBucketKey
	Users  int64
	Events int64
}

// ReleaseHealthPolicy 是一个应用、环境的健康策略。Configured 为 false 表示没有保存过，字段是默认值。
type ReleaseHealthPolicy struct {
	ApplicationID   string
	Environment     string
	EnvironmentName string
	Configured      bool
	Enabled         bool
	WindowMinutes   int
	MinUsers        int
	MaxFailureRate  float64
	MinDelta        float64
	AutoStopGray    bool
	UpdatedByUserID string
	UpdatedAt       *time.Time
}

// DefaultReleaseHealthPolicy 是没有保存过策略时的默认值，与迁移里的列默认值一致。
func DefaultReleaseHealthPolicy() ReleaseHealthPolicy {
	return ReleaseHealthPolicy{WindowMinutes: 30, MinUsers: 50, MaxFailureRate: 0.05, MinDelta: 0.02, AutoStopGray: true}
}

func (p ReleaseHealthPolicy) Valid() bool {
	return p.WindowMinutes >= 5 && p.WindowMinutes <= 1440 &&
		p.MinUsers >= 1 && p.MinUsers <= 1000000 &&
		p.MaxFailureRate > 0 && p.MaxFailureRate <= 1 &&
		p.MinDelta >= 0 && p.MinDelta <= 1
}

type ReleaseHealthStatus string

const (
	ReleaseHealthNoReports        ReleaseHealthStatus = "no_reports"
	ReleaseHealthInsufficientData ReleaseHealthStatus = "insufficient_data"
	ReleaseHealthHealthy          ReleaseHealthStatus = "healthy"
	ReleaseHealthUnhealthy        ReleaseHealthStatus = "unhealthy"
)

// LaunchCounts 是一个窗口内的去重用户数。
type LaunchCounts struct {
	Launched       int64
	LaunchFailed   int64
	VerifyFailed   int64
	DownloadFailed int64
}

// FailureRate 是 launch_failed /（launched + launch_failed），没有样本时为 0。
func (c LaunchCounts) FailureRate() float64 {
	total := c.Launched + c.LaunchFailed
	if total == 0 {
		return 0
	}
	return float64(c.LaunchFailed) / float64(total)
}

// ReleaseHealthTarget 是要判定的一条生效发布，以及它所属应用、环境的策略。
type ReleaseHealthTarget struct {
	ReleaseID      string
	ApplicationID  string
	ModuleID       string
	Environment    string
	RuntimeVersion string
	Channel        string
	ReleaseType    ReleaseType
	GrayStrategy   ReleaseGrayStrategy
	CreatedAt      time.Time
	Policy         ReleaseHealthPolicy
	// BaselineReleaseID 是同一模块、环境、渠道、运行时版本下生效中的全量发布，判定比例灰度时用作基线；没有时为空。
	BaselineReleaseID string
	// PreviousStatus 是上一次判定的状态，没有判定过时为空。
	PreviousStatus ReleaseHealthStatus
}

// ReleaseHealth 是一次判定的结果。
type ReleaseHealth struct {
	ReleaseID           string
	Status              ReleaseHealthStatus
	PolicyEnabled       bool
	Policy              ReleaseHealthPolicy
	WindowStart         time.Time
	Counts              LaunchCounts
	FailureRate         float64
	BaselineFailureRate *float64
	EvaluatedAt         time.Time
}

// EvaluateReleaseHealth 按 docs/release-health.md 第 5 节的规则得出结论。hasReports 表示发布创建以来是否收到过
// launched 或 launch_failed 上报；baseline 为空表示没有可用的基线（没有全量，或全量的用户数不够）。
func EvaluateReleaseHealth(policy ReleaseHealthPolicy, hasReports bool, counts LaunchCounts, baseline *LaunchCounts) (ReleaseHealthStatus, float64, *float64) {
	rate := counts.FailureRate()
	var baselineRate *float64
	if baseline != nil && baseline.Launched+baseline.LaunchFailed >= int64(policy.MinUsers) {
		value := baseline.FailureRate()
		baselineRate = &value
	}
	switch {
	case !hasReports:
		return ReleaseHealthNoReports, rate, baselineRate
	case counts.Launched+counts.LaunchFailed < int64(policy.MinUsers):
		return ReleaseHealthInsufficientData, rate, baselineRate
	case rate >= policy.MaxFailureRate && (baselineRate == nil || rate >= *baselineRate+policy.MinDelta):
		return ReleaseHealthUnhealthy, rate, baselineRate
	default:
		return ReleaseHealthHealthy, rate, baselineRate
	}
}
