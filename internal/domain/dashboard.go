package domain

import "time"

type DashboardSnapshot struct {
	Metrics    []DashboardMetric
	BuildTrend []DashboardBuildTrendPoint
	Builds     []DashboardBuildRecord
	Releases   []DashboardReleaseRecord
	Resources  []DashboardResourceStatus
	UpdatedAt  time.Time
}

type DashboardMetric struct {
	Description string
	ID          string
	Label       string
	Value       string
	Trend       string
	TrendTone   string
	Icon        string
	IconTone    string
	Sparkline   []int
}

type DashboardBuildTrendPoint struct {
	Date         string
	BuildCount   int
	SuccessCount int
}

type DashboardBuildRecord struct {
	Application string
	Duration    string
	Environment string
	ID          string
	Module      string
	Operator    string
	StartedAt   string
	Status      string
}

type DashboardReleaseRecord struct {
	Application string
	ID          string
	Environment string
	Gray        string
	Module      string
	Status      string
	UpdatedAt   string
}

type DashboardResourceStatus struct {
	Description string
	Href        string
	ID          string
	Label       string
	Status      string
	StatusLabel string
	Value       string
}
