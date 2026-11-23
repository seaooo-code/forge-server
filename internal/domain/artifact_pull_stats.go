package domain

import (
	"errors"
	"time"
)

var ErrInvalidArtifactPullStatsInput = errors.New("invalid artifact pull stats request")

// ArtifactPullOutcomeMissingPrefix 加上缺包原因，表示这次请求里该模块没有拿到构建，例如 missing:no_release_for_runtime。
const ArtifactPullOutcomeMissingPrefix = "missing:"

// 拉包统计的保留天数和单次查询的最大天数。
const (
	ArtifactPullStatsRetentionDays = 90
	MaxArtifactPullStatsQueryDays  = 90
)

// ArtifactPullStatKey 是拉包统计的聚合维度：某一天、某个应用环境下，某个模块在某个运行时版本和平台上的解析结果。
// Outcome 是来源（testPlan、percentage、full）或 missing:{原因}；缺包时 BuildRecordID 为空。
type ArtifactPullStatKey struct {
	Day            time.Time
	ApplicationID  string
	Environment    string
	ModuleID       string
	RuntimeVersion string
	Platform       string
	Outcome        string
	BuildRecordID  string
}

type ArtifactPullStat struct {
	ArtifactPullStatKey
	Requests int64
}

// ArtifactPullStatsRow 是查询结果的一行，附带模块名和构建号方便展示；模块或构建已删除时为空。
type ArtifactPullStatsRow struct {
	ArtifactPullStat
	ModuleName  string
	BuildNumber int64
}

// ArtifactPullStatsFilters 中应用、模块、构建至少指定一个，环境可选；Days 是包含今天在内往前的天数。
type ArtifactPullStatsFilters struct {
	ApplicationID string
	Environment   string
	ModuleID      string
	BuildRecordID string
	Days          int
	// From 由服务层按 Days 算出，是查询的起始日期（含）。
	From time.Time
}
