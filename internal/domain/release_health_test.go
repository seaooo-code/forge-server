package domain

import "testing"

func TestEvaluateReleaseHealth(t *testing.T) {
	policy := DefaultReleaseHealthPolicy()
	full := func(launched, failed int64) *LaunchCounts {
		return &LaunchCounts{Launched: launched, LaunchFailed: failed}
	}
	for _, tc := range []struct {
		name       string
		hasReports bool
		counts     LaunchCounts
		baseline   *LaunchCounts
		want       ReleaseHealthStatus
		baseRate   bool
	}{
		{name: "never reported", counts: LaunchCounts{}, want: ReleaseHealthNoReports},
		{name: "too few users", hasReports: true, counts: LaunchCounts{Launched: 40, LaunchFailed: 9}, want: ReleaseHealthInsufficientData},
		{name: "healthy", hasReports: true, counts: LaunchCounts{Launched: 98, LaunchFailed: 2}, want: ReleaseHealthHealthy},
		{name: "over threshold without baseline", hasReports: true, counts: LaunchCounts{Launched: 95, LaunchFailed: 5}, want: ReleaseHealthUnhealthy},
		// 全量本身失败率 4%，灰度 5% 只高 1 个百分点，没超过 min_delta。
		{name: "over threshold but close to baseline", hasReports: true, counts: LaunchCounts{Launched: 95, LaunchFailed: 5}, baseline: full(96, 4), want: ReleaseHealthHealthy, baseRate: true},
		{name: "over threshold and baseline", hasReports: true, counts: LaunchCounts{Launched: 90, LaunchFailed: 10}, baseline: full(96, 4), want: ReleaseHealthUnhealthy, baseRate: true},
		// 基线用户数不够时不用基线，只看绝对阈值。
		{name: "baseline too small", hasReports: true, counts: LaunchCounts{Launched: 95, LaunchFailed: 5}, baseline: full(9, 1), want: ReleaseHealthUnhealthy},
	} {
		status, _, baseline := EvaluateReleaseHealth(policy, tc.hasReports, tc.counts, tc.baseline)
		if status != tc.want || (baseline != nil) != tc.baseRate {
			t.Fatalf("%s: got %s (baseline %v), want %s (baseline %v)", tc.name, status, baseline, tc.want, tc.baseRate)
		}
	}
}
