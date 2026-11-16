package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"forge-server/internal/domain"
)

type DashboardApplicationService interface {
	ListApplications(ctx context.Context, filters domain.ApplicationListFilters) ([]domain.Application, domain.ApplicationSummary, error)
}

type DashboardModuleService interface {
	ListModules(ctx context.Context, filters domain.ModuleListFilters) ([]domain.Module, domain.ModuleSummary, error)
}

type DashboardBuildService interface {
	ListBuildRecords(ctx context.Context, filters domain.BuildRecordFilters) ([]domain.BuildRecord, domain.BuildRecordSummary, error)
}

type DashboardReleaseService interface {
	ListReleases(ctx context.Context, filters domain.ReleaseRecordFilters) ([]domain.ReleaseRecord, domain.ReleaseRecordSummary, error)
}

type DashboardCodeSourceService interface {
	ListCodeSources(ctx context.Context) ([]domain.CodeSourceApp, error)
}

type DashboardStorageService interface {
	ListStorageConfigurations(ctx context.Context, pagination domain.Pagination) ([]domain.StorageConfiguration, int, error)
}

type DashboardEnvironmentService interface {
	ListEnvironments(ctx context.Context) ([]domain.Environment, error)
}

type DashboardArtifactAccessTokenService interface {
	ListArtifactAccessTokens(ctx context.Context) ([]domain.ArtifactAccessToken, error)
}

type DashboardService struct {
	applications         DashboardApplicationService
	modules              DashboardModuleService
	builds               DashboardBuildService
	releases             DashboardReleaseService
	codeSources          DashboardCodeSourceService
	storage              DashboardStorageService
	environments         DashboardEnvironmentService
	artifactAccessTokens DashboardArtifactAccessTokenService
	now                  func() time.Time
}

type DashboardDependencies struct {
	ApplicationService         DashboardApplicationService
	ModuleService              DashboardModuleService
	BuildService               DashboardBuildService
	ReleaseService             DashboardReleaseService
	CodeSourceService          DashboardCodeSourceService
	StorageService             DashboardStorageService
	EnvironmentService         DashboardEnvironmentService
	ArtifactAccessTokenService DashboardArtifactAccessTokenService
}

func NewDashboardService(deps DashboardDependencies) *DashboardService {
	return &DashboardService{
		applications:         deps.ApplicationService,
		modules:              deps.ModuleService,
		builds:               deps.BuildService,
		releases:             deps.ReleaseService,
		codeSources:          deps.CodeSourceService,
		storage:              deps.StorageService,
		environments:         deps.EnvironmentService,
		artifactAccessTokens: deps.ArtifactAccessTokenService,
		now:                  time.Now,
	}
}

func (s *DashboardService) GetDashboardSnapshot(ctx context.Context, ownerUnionID string) (domain.DashboardSnapshot, error) {
	now := s.currentTime()
	_, applicationSummary, err := s.listApplications(ctx)
	if err != nil {
		return domain.DashboardSnapshot{}, err
	}
	_, moduleSummary, err := s.listModules(ctx)
	if err != nil {
		return domain.DashboardSnapshot{}, err
	}
	builds, _, err := s.listBuilds(ctx)
	if err != nil {
		return domain.DashboardSnapshot{}, err
	}
	releases, _, err := s.listReleases(ctx)
	if err != nil {
		return domain.DashboardSnapshot{}, err
	}
	codeSources, err := s.listCodeSources(ctx)
	if err != nil {
		return domain.DashboardSnapshot{}, err
	}
	storage, _, err := s.listStorage(ctx)
	if err != nil {
		return domain.DashboardSnapshot{}, err
	}
	environments, err := s.listEnvironments(ctx)
	if err != nil {
		return domain.DashboardSnapshot{}, err
	}
	tokens, err := s.listArtifactAccessTokens(ctx)
	if err != nil {
		return domain.DashboardSnapshot{}, err
	}

	recentBuilds := recentDashboardBuilds(builds)
	recentReleases := recentDashboardReleases(releases)
	trend, recentBuildTotal, recentBuildSuccess := buildTrend(builds, now)
	resourceTotal := applicationSummary.Total + moduleSummary.Total + len(codeSources) + len(storage) + len(environments) + len(tokens)

	return domain.DashboardSnapshot{
		Metrics: []domain.DashboardMetric{
			{
				ID:          "apps",
				Label:       "应用数量",
				Description: "已接入控制台的应用",
				Value:       strconv.Itoa(applicationSummary.Total),
				Trend:       statusTrend(applicationSummary.Running, "运行中"),
				TrendTone:   "positive",
				Icon:        "app",
				IconTone:    "blue",
				Sparkline:   repeatedSparkline(applicationSummary.Total),
			},
			{
				ID:          "modules",
				Label:       "模块数量",
				Description: "可独立构建的业务模块",
				Value:       strconv.Itoa(moduleSummary.Total),
				Trend:       statusTrend(moduleSummary.Feature, "Feature"),
				TrendTone:   "positive",
				Icon:        "module",
				IconTone:    "cyan",
				Sparkline:   repeatedSparkline(moduleSummary.Total),
			},
			{
				ID:          "builds",
				Label:       "近 7 天构建",
				Description: "手动与系统触发构建总量",
				Value:       strconv.Itoa(recentBuildTotal),
				Trend:       recentTrend(recentBuildTotal),
				TrendTone:   "positive",
				Icon:        "build",
				IconTone:    "emerald",
				Sparkline:   trendBuildCounts(trend),
			},
			{
				ID:          "build-success",
				Label:       "构建成功率",
				Description: "近 7 天成功构建占比",
				Value:       successRate(recentBuildSuccess, recentBuildTotal),
				Trend:       statusTrend(recentBuildSuccess, "成功"),
				TrendTone:   successTrendTone(recentBuildSuccess, recentBuildTotal),
				Icon:        "build",
				IconTone:    "indigo",
				Sparkline:   trendSuccessRates(trend),
			},
			{
				ID:          "releases",
				Label:       "发布记录",
				Description: "当前环境发布目标",
				Value:       strconv.Itoa(len(releases)),
				Trend:       statusTrend(activeReleaseCount(releases), "生效中"),
				TrendTone:   "positive",
				Icon:        "release",
				IconTone:    "amber",
				Sparkline:   repeatedSparkline(len(releases)),
			},
			{
				ID:          "resources",
				Label:       "平台资源",
				Description: "代码源、存储、环境、令牌",
				Value:       strconv.Itoa(resourceTotal),
				Trend:       "稳定",
				TrendTone:   "neutral",
				Icon:        "storage",
				IconTone:    "slate",
				Sparkline:   repeatedSparkline(resourceTotal),
			},
		},
		BuildTrend: trend,
		Builds:     recentBuilds,
		Releases:   recentReleases,
		Resources:  dashboardResources(applicationSummary, moduleSummary, codeSources, storage, environments, tokens),
		UpdatedAt:  now,
	}, nil
}

func (s *DashboardService) currentTime() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

func (s *DashboardService) listApplications(ctx context.Context) ([]domain.Application, domain.ApplicationSummary, error) {
	if s.applications == nil {
		return nil, domain.ApplicationSummary{}, nil
	}
	return s.applications.ListApplications(ctx, domain.ApplicationListFilters{})
}

func (s *DashboardService) listModules(ctx context.Context) ([]domain.Module, domain.ModuleSummary, error) {
	if s.modules == nil {
		return nil, domain.ModuleSummary{}, nil
	}
	return s.modules.ListModules(ctx, domain.ModuleListFilters{})
}

func (s *DashboardService) listBuilds(ctx context.Context) ([]domain.BuildRecord, domain.BuildRecordSummary, error) {
	if s.builds == nil {
		return nil, domain.BuildRecordSummary{}, nil
	}
	return s.builds.ListBuildRecords(ctx, domain.BuildRecordFilters{})
}

func (s *DashboardService) listReleases(ctx context.Context) ([]domain.ReleaseRecord, domain.ReleaseRecordSummary, error) {
	if s.releases == nil {
		return nil, domain.ReleaseRecordSummary{}, nil
	}
	return s.releases.ListReleases(ctx, domain.ReleaseRecordFilters{})
}

func (s *DashboardService) listCodeSources(ctx context.Context) ([]domain.CodeSourceApp, error) {
	if s.codeSources == nil {
		return nil, nil
	}
	return s.codeSources.ListCodeSources(ctx)
}

func (s *DashboardService) listStorage(ctx context.Context) ([]domain.StorageConfiguration, int, error) {
	if s.storage == nil {
		return nil, 0, nil
	}
	return s.storage.ListStorageConfigurations(ctx, domain.Pagination{})
}

func (s *DashboardService) listEnvironments(ctx context.Context) ([]domain.Environment, error) {
	if s.environments == nil {
		return nil, nil
	}
	return s.environments.ListEnvironments(ctx)
}

func (s *DashboardService) listArtifactAccessTokens(ctx context.Context) ([]domain.ArtifactAccessToken, error) {
	if s.artifactAccessTokens == nil {
		return nil, nil
	}
	return s.artifactAccessTokens.ListArtifactAccessTokens(ctx)
}

func buildTrend(records []domain.BuildRecord, now time.Time) ([]domain.DashboardBuildTrendPoint, int, int) {
	localNow := now.Local()
	start := beginningOfDay(localNow).AddDate(0, 0, -6)
	points := make([]domain.DashboardBuildTrendPoint, 0, 7)
	indexByDay := map[string]int{}
	for index := 0; index < 7; index++ {
		day := start.AddDate(0, 0, index)
		key := day.Format("2006-01-02")
		indexByDay[key] = index
		points = append(points, domain.DashboardBuildTrendPoint{Date: day.Format("1/2")})
	}

	total := 0
	success := 0
	for _, record := range records {
		createdAt := record.CreatedAt.Local()
		if createdAt.Before(start) || !createdAt.Before(start.AddDate(0, 0, 7)) {
			continue
		}
		pointIndex, ok := indexByDay[createdAt.Format("2006-01-02")]
		if !ok {
			continue
		}
		points[pointIndex].BuildCount++
		total++
		if record.Status == domain.BuildStatusSucceeded {
			points[pointIndex].SuccessCount++
			success++
		}
	}
	return points, total, success
}

func beginningOfDay(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location())
}

func recentDashboardBuilds(records []domain.BuildRecord) []domain.DashboardBuildRecord {
	sorted := append([]domain.BuildRecord(nil), records...)
	sort.SliceStable(sorted, func(i int, j int) bool {
		return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
	})
	if len(sorted) > 5 {
		sorted = sorted[:5]
	}

	items := make([]domain.DashboardBuildRecord, 0, len(sorted))
	for _, record := range sorted {
		items = append(items, domain.DashboardBuildRecord{
			Application: record.ApplicationName,
			Duration:    formatDashboardBuildDuration(record.StartedAt, record.FinishedAt),
			Environment: record.Environment,
			ID:          record.ID,
			Module:      fallbackString(record.ModuleName, record.ModuleID),
			Operator:    fallbackString(record.WorkerID, "-"),
			StartedAt:   formatDashboardTime(firstBuildTime(record)),
			Status:      dashboardBuildStatus(record.Status),
		})
	}
	return items
}

func recentDashboardReleases(records []domain.ReleaseRecord) []domain.DashboardReleaseRecord {
	sorted := append([]domain.ReleaseRecord(nil), records...)
	sort.SliceStable(sorted, func(i int, j int) bool {
		return sorted[i].UpdatedAt.After(sorted[j].UpdatedAt)
	})
	if len(sorted) > 5 {
		sorted = sorted[:5]
	}

	items := make([]domain.DashboardReleaseRecord, 0, len(sorted))
	for _, record := range sorted {
		items = append(items, domain.DashboardReleaseRecord{
			Application: record.ApplicationName,
			ID:          record.ID,
			Environment: record.Environment,
			Gray:        dashboardReleaseGray(record),
			Module:      fallbackString(record.ModuleName, record.ModuleID),
			Status:      string(record.Status),
			UpdatedAt:   formatDashboardTime(record.UpdatedAt),
		})
	}
	return items
}

func dashboardResources(applications domain.ApplicationSummary, modules domain.ModuleSummary, codeSources []domain.CodeSourceApp, storage []domain.StorageConfiguration, environments []domain.Environment, tokens []domain.ArtifactAccessToken) []domain.DashboardResourceStatus {
	codeSourceAttention := 0
	for _, app := range codeSources {
		if app.Status != domain.CodeSourceAppStatusInstalled || app.ConfigurationStatus != domain.CodeSourceAppConfigurationStatusConfigured {
			codeSourceAttention++
		}
	}
	storageAttention := 0
	for _, config := range storage {
		if config.Status != domain.StorageStatusHealthy {
			storageAttention++
		}
	}
	grayEnvironments := 0
	for _, environment := range environments {
		if environment.Status == domain.EnvironmentStatusGray {
			grayEnvironments++
		}
	}
	disabledTokens := 0
	for _, token := range tokens {
		if token.Status == domain.ArtifactAccessTokenStatusDisabled {
			disabledTokens++
		}
	}

	return []domain.DashboardResourceStatus{
		{ID: "apps", Label: "应用", Value: strconv.Itoa(applications.Total), Description: "应用详情、成员和接入信息", Href: "/apps", Status: resourceStatus(applications.Offline, "healthy"), StatusLabel: applicationResourceStatusLabel(applications.Offline)},
		{ID: "modules", Label: "模块", Value: strconv.Itoa(modules.Total), Description: "模块仓库与构建入口", Href: "/modules", Status: "healthy", StatusLabel: "可构建"},
		{ID: "code-sources", Label: "代码源", Value: strconv.Itoa(len(codeSources)), Description: "代码源应用配置和安装状态", Href: "/code-sources", Status: resourceStatus(codeSourceAttention, "healthy"), StatusLabel: issueStatusLabel(codeSourceAttention, "可用")},
		{ID: "storage", Label: "存储配置", Value: strconv.Itoa(len(storage)), Description: "Bundle 产物上传与分发配置", Href: "/storage", Status: resourceStatus(storageAttention, "healthy"), StatusLabel: issueStatusLabel(storageAttention, "正常")},
		{ID: "environments", Label: "环境", Value: strconv.Itoa(len(environments)), Description: "开发、预发、灰度和生产环境", Href: "/environments", Status: environmentResourceStatus(grayEnvironments), StatusLabel: environmentResourceStatusLabel(grayEnvironments)},
		{ID: "access-tokens", Label: "访问令牌", Value: strconv.Itoa(len(tokens)), Description: "用于制品下载与外部集成", Href: "/access-tokens", Status: resourceStatus(disabledTokens, "healthy"), StatusLabel: issueStatusLabel(disabledTokens, "可用")},
	}
}

func resourceStatus(issueCount int, defaultStatus string) string {
	if issueCount > 0 {
		return "attention"
	}
	return defaultStatus
}

func applicationResourceStatusLabel(offline int) string {
	if offline > 0 {
		return strconv.Itoa(offline) + " 个下线"
	}
	return "运行中"
}

func issueStatusLabel(issueCount int, okLabel string) string {
	if issueCount > 0 {
		return strconv.Itoa(issueCount) + " 个需处理"
	}
	return okLabel
}

func environmentResourceStatus(grayCount int) string {
	if grayCount > 0 {
		return "neutral"
	}
	return "healthy"
}

func environmentResourceStatusLabel(grayCount int) string {
	if grayCount > 0 {
		return strconv.Itoa(grayCount) + " 个灰度"
	}
	return "已配置"
}

func activeReleaseCount(records []domain.ReleaseRecord) int {
	count := 0
	for _, record := range records {
		if record.Status == domain.ReleaseStatusActive {
			count++
		}
	}
	return count
}

func trendBuildCounts(points []domain.DashboardBuildTrendPoint) []int {
	values := make([]int, 0, len(points))
	for _, point := range points {
		values = append(values, point.BuildCount)
	}
	return values
}

func trendSuccessRates(points []domain.DashboardBuildTrendPoint) []int {
	values := make([]int, 0, len(points))
	for _, point := range points {
		if point.BuildCount == 0 {
			values = append(values, 0)
			continue
		}
		values = append(values, int(float64(point.SuccessCount)/float64(point.BuildCount)*100))
	}
	return values
}

func repeatedSparkline(value int) []int {
	return []int{value, value, value, value, value, value, value}
}

func successRate(success int, total int) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", float64(success)/float64(total)*100)
}

func successTrendTone(success int, total int) string {
	if total == 0 || success == total {
		return "neutral"
	}
	return "negative"
}

func recentTrend(value int) string {
	if value == 0 {
		return "0"
	}
	return "+" + strconv.Itoa(value)
}

func statusTrend(value int, suffix string) string {
	return strconv.Itoa(value) + " " + suffix
}

func firstBuildTime(record domain.BuildRecord) time.Time {
	if record.StartedAt != nil {
		return *record.StartedAt
	}
	if !record.QueuedAt.IsZero() {
		return record.QueuedAt
	}
	return record.CreatedAt
}

func formatDashboardTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format("01-02 15:04")
}

func formatDashboardBuildDuration(startedAt *time.Time, finishedAt *time.Time) string {
	if startedAt == nil {
		return "-"
	}
	end := time.Now()
	if finishedAt != nil {
		end = *finishedAt
	}
	if end.Before(*startedAt) {
		return "-"
	}
	seconds := int(end.Sub(*startedAt).Seconds())
	if seconds < 1 {
		seconds = 1
	}
	minutes := seconds / 60
	seconds = seconds % 60
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func dashboardBuildStatus(status domain.BuildStatus) string {
	switch status {
	case domain.BuildStatusPending:
		return "queued"
	case domain.BuildStatusRunning:
		return "running"
	case domain.BuildStatusSucceeded:
		return "success"
	case domain.BuildStatusFailed:
		return "failed"
	default:
		return string(status)
	}
}

func dashboardReleaseGray(record domain.ReleaseRecord) string {
	if record.ReleaseType == domain.ReleaseTypeFull {
		return "全量"
	}
	if record.GrayStrategy == domain.ReleaseGrayStrategyPercentage {
		return strconv.Itoa(record.GrayPercentage) + "%"
	}
	if record.Identifier != "" {
		return "定向"
	}
	return "灰度"
}

func fallbackString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
