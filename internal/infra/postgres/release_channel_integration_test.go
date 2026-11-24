package postgres

import (
	"context"
	"errors"
	"testing"

	"forge-server/internal/domain"
)

// channelFixture 在提测单测试的应用上加发布渠道相关的辅助方法。
type channelFixture struct {
	*testPlanFixture
	channels *ReleaseChannelRepository
	releases *ReleaseRepository
}

func newChannelFixture(t *testing.T, codes ...string) *channelFixture {
	t.Helper()
	f := &channelFixture{testPlanFixture: newTestPlanFixture(t)}
	f.channels = NewReleaseChannelRepository(f.pool)
	f.releases = NewReleaseRepository(f.pool)
	for _, code := range codes {
		if _, err := f.channels.CreateReleaseChannel(context.Background(), f.slug, domain.ReleaseChannelInput{Code: code, Name: code, CreatedByUserID: f.userID}); err != nil {
			t.Fatalf("create channel %s: %v", code, err)
		}
	}
	return f
}

// capabilityBuild 插入一条 rt-1 的构建并写入能力声明。
func (f *channelFixture) capabilityBuild(t *testing.T, moduleID string, provides string, requires string) string {
	t.Helper()
	id, _ := f.build(t, moduleID, "main", "rt-1")
	if _, err := f.pool.Exec(context.Background(), `update build_records set provided_capabilities = $2::jsonb, required_capabilities = $3::jsonb where id = $1`, id, provides, requires); err != nil {
		t.Fatalf("set capabilities: %v", err)
	}
	return id
}

func (f *channelFixture) release(buildID string, channel string, grayPercentage int) (domain.ReleaseRecord, error) {
	input := domain.ReleaseCreateInput{BuildRecordID: buildID, Channel: channel, ReleaseType: domain.ReleaseTypeFull, CreatedByUserID: f.userID}
	if grayPercentage > 0 {
		input.ReleaseType, input.GrayStrategy, input.GrayPercentage = domain.ReleaseTypeGray, domain.ReleaseGrayStrategyPercentage, grayPercentage
	}
	record, _, err := f.releases.CreateRelease(context.Background(), input)
	return record, err
}

func (f *channelFixture) releaseErr(buildID string, channel string, grayPercentage int) error {
	_, err := f.release(buildID, channel, grayPercentage)
	return err
}

func (f *channelFixture) mustRelease(t *testing.T, buildID string, channel string, grayPercentage int) domain.ReleaseRecord {
	t.Helper()
	record, err := f.release(buildID, channel, grayPercentage)
	if err != nil {
		t.Fatalf("release build %s to channel %q: %v", buildID, channel, err)
	}
	return record
}

// manifest 返回请求渠道下解析出的整份清单来源。
func (f *channelFixture) manifest(t *testing.T, userID string, channel string) domain.ArtifactManifestSource {
	t.Helper()
	source, err := f.artifacts.GetArtifactManifestSource(context.Background(), domain.ArtifactManifestInput{
		ApplicationSlug: f.slug,
		Environment:     f.environment,
		Platform:        domain.PlatformAndroid,
		RuntimeVersion:  "rt-1",
		UserID:          userID,
		Channel:         channel,
	})
	if err != nil {
		t.Fatalf("manifest source: %v", err)
	}
	return source
}

func (f *channelFixture) module(t *testing.T, userID string, channel string, moduleID string) domain.ArtifactModuleReleaseSource {
	t.Helper()
	for _, module := range f.manifest(t, userID, channel).Modules {
		if module.ID == moduleID {
			return module
		}
	}
	t.Fatalf("module %s missing from manifest source", moduleID)
	return domain.ArtifactModuleReleaseSource{}
}

// 渠道客户端按模块回落到默认渠道；渠道只有灰度时没分到灰度的用户也回落；未注册的渠道等同于不传；提测单优先于渠道。
func TestIntegrationChannelManifestResolution(t *testing.T) {
	f := newChannelFixture(t, "beta")
	ctx := context.Background()

	commonFull, _ := f.build(t, f.common, "main", "rt-1")
	featureFull, _ := f.build(t, f.feature, "main", "rt-1")
	featureBeta, _ := f.build(t, f.feature, "main", "rt-1")
	f.mustRelease(t, commonFull, "", 0)
	defaultFeature := f.mustRelease(t, featureFull, "", 0)
	betaFeature := f.mustRelease(t, featureBeta, "beta", 0)

	if betaFeature.Channel != "beta" {
		t.Fatalf("expected release channel beta, got %q", betaFeature.Channel)
	}
	if got := f.module(t, "1001", "", f.feature); got.BuildRecordID != featureFull || got.Channel != "" {
		t.Fatalf("default client: expected default full, got %+v", got)
	}
	beta := f.manifest(t, "1001", "beta")
	if beta.Channel != "beta" {
		t.Fatalf("expected resolved channel beta, got %q", beta.Channel)
	}
	for _, module := range beta.Modules {
		switch module.ID {
		case f.feature:
			if module.BuildRecordID != featureBeta || module.Channel != "beta" {
				t.Fatalf("beta client feature: expected beta full, got %+v", module)
			}
		case f.common:
			if module.BuildRecordID != commonFull || module.Channel != "" {
				t.Fatalf("beta client common: expected fallback to default full, got %+v", module)
			}
		}
	}
	unknown := f.manifest(t, "1001", "nope")
	if unknown.Channel != "" {
		t.Fatalf("unregistered channel should resolve to default, got %q", unknown.Channel)
	}
	if got := f.module(t, "1001", "nope", f.feature); got.BuildRecordID != featureFull {
		t.Fatalf("unregistered channel: expected default full, got %+v", got)
	}

	// 渠道只有 Common 比例灰度：命中的用户拿渠道灰度，没命中的回落到默认渠道全量。
	commonBetaGray, _ := f.build(t, f.common, "main", "rt-1")
	f.mustRelease(t, commonBetaGray, "beta", 50)
	hit, miss := 0, 0
	for _, userID := range []string{"2001", "2002", "2003", "2004", "2005", "2006", "2007", "2008", "2009", "2010", "2011", "2012", "2013", "2014", "2015", "2016"} {
		switch got := f.module(t, userID, "beta", f.common); got.BuildRecordID {
		case commonBetaGray:
			hit++
		case commonFull:
			miss++
		default:
			t.Fatalf("unexpected common build for %s: %+v", userID, got)
		}
		if got := f.module(t, userID, "", f.common); got.BuildRecordID != commonFull {
			t.Fatalf("default client must not see beta gray: %+v", got)
		}
	}
	if hit == 0 || miss == 0 {
		t.Fatalf("expected both hits and misses at 50%%, got hit=%d miss=%d", hit, miss)
	}

	// 新的渠道全量只替换渠道路由，默认渠道全量不受影响；回滚在同一渠道内找上一次全量。
	featureBeta2, _ := f.build(t, f.feature, "main", "rt-1")
	betaFeature2 := f.mustRelease(t, featureBeta2, "beta", 0)
	if record, err := f.releases.GetRelease(ctx, defaultFeature.ID); err != nil || record.Status != domain.ReleaseStatusActive {
		t.Fatalf("default full should stay active, got %+v %v", record, err)
	}
	if record, err := f.releases.GetRelease(ctx, betaFeature.ID); err != nil || record.Status != domain.ReleaseStatusInactive {
		t.Fatalf("previous beta full should be replaced, got %+v %v", record, err)
	}
	previous, err := f.releases.PreviousFullRelease(ctx, betaFeature2.ID)
	if err != nil || previous.ID != betaFeature.ID {
		t.Fatalf("expected previous beta full %s, got %+v %v", betaFeature.ID, previous, err)
	}

	// 提测单优先于渠道。
	planBuild, _ := f.build(t, f.feature, "feature/plan", "rt-1")
	f.createPlan(t, "plan", []domain.TestPlanItemInput{follow(f.feature, "feature/plan")}, []string{"3001"})
	if got := f.module(t, "3001", "beta", f.feature); got.BuildRecordID != planBuild || got.TestPlanID == "" || got.Channel != "" {
		t.Fatalf("test plan should win over channel, got %+v", got)
	}

	// 停用渠道全量后，渠道客户端回落到默认渠道全量。
	if _, _, err := f.releases.DeactivateRelease(ctx, domain.ReleaseDeactivateInput{ID: betaFeature2.ID, ActorUserID: f.userID}); err != nil {
		t.Fatalf("deactivate beta full: %v", err)
	}
	if got := f.module(t, "1001", "beta", f.feature); got.BuildRecordID != featureFull || got.Channel != "" {
		t.Fatalf("after deactivation beta client should fall back to default full, got %+v", got)
	}

	// 列表按渠道筛选：nil 不筛选，空字符串只看默认渠道。
	defaultChannel, betaChannel := "", "beta"
	for _, tc := range []struct {
		channel *string
		want    int
	}{{nil, 5}, {&defaultChannel, 2}, {&betaChannel, 3}} {
		_, summary, err := f.releases.ListReleases(ctx, domain.ReleaseRecordFilters{ApplicationID: f.application, Channel: tc.channel})
		if err != nil || summary.Total != tc.want {
			t.Fatalf("list releases with channel %v: expected %d, got %d %v", tc.channel, tc.want, summary.Total, err)
		}
	}
}

// 发布到未注册的渠道被拒绝；有生效发布的渠道不能删除；同名渠道不能重复创建。
func TestIntegrationChannelLifecycle(t *testing.T) {
	f := newChannelFixture(t, "beta")
	ctx := context.Background()

	build, _ := f.build(t, f.feature, "main", "rt-1")
	if _, err := f.release(build, "alpha", 0); !errors.Is(err, domain.ErrReleaseChannelNotFound) {
		t.Fatalf("expected channel not found, got %v", err)
	}
	if _, err := f.channels.CreateReleaseChannel(ctx, f.slug, domain.ReleaseChannelInput{Code: "beta", Name: "dup"}); !errors.Is(err, domain.ErrReleaseChannelExists) {
		t.Fatalf("expected duplicate channel error, got %v", err)
	}

	record := f.mustRelease(t, build, "beta", 0)
	channels, err := f.channels.ListReleaseChannels(ctx, f.slug)
	if err != nil || len(channels) != 1 || channels[0].ActiveReleases != 1 {
		t.Fatalf("expected one channel with one active release, got %+v %v", channels, err)
	}
	if err := f.channels.DeleteReleaseChannel(ctx, f.slug, "beta"); !errors.Is(err, domain.ErrReleaseChannelInUse) {
		t.Fatalf("expected channel in use, got %v", err)
	}
	if _, _, err := f.releases.DeactivateRelease(ctx, domain.ReleaseDeactivateInput{ID: record.ID, ActorUserID: f.userID}); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	updated, err := f.channels.UpdateReleaseChannel(ctx, f.slug, "beta", domain.ReleaseChannelInput{Name: "内测", Description: "内测 App"})
	if err != nil || updated.Name != "内测" || updated.ActiveReleases != 0 {
		t.Fatalf("update channel: %+v %v", updated, err)
	}
	if err := f.channels.DeleteReleaseChannel(ctx, f.slug, "beta"); err != nil {
		t.Fatalf("delete channel: %v", err)
	}
	if err := f.channels.DeleteReleaseChannel(ctx, f.slug, "beta"); !errors.Is(err, domain.ErrReleaseChannelNotFound) {
		t.Fatalf("expected channel not found after delete, got %v", err)
	}
	if _, err := f.channels.ListReleaseChannels(ctx, "missing-app"); !errors.Is(err, domain.ErrApplicationNotFound) {
		t.Fatalf("expected application not found, got %v", err)
	}
}

// 能力约束按渠道回落计算：渠道客户端在没有渠道 Common 时拿默认 Common；发布默认 Common 时检查会回落的渠道。
func TestIntegrationChannelCapabilityConstraint(t *testing.T) {
	f := newChannelFixture(t, "beta", "gamma")
	conflict := func(err error) *domain.ReleaseCapabilityConflictError {
		t.Helper()
		var target *domain.ReleaseCapabilityConflictError
		if !errors.As(err, &target) {
			t.Fatalf("expected capability conflict, got %v", err)
		}
		return target
	}

	f.mustRelease(t, f.capabilityBuild(t, f.common, `{"commonApi": 2}`, `{}`), "", 0)
	f.mustRelease(t, f.capabilityBuild(t, f.common, `{"commonApi": 3}`, `{}`), "beta", 0)

	// beta 有自己的 Common 3，要求 3 的 Feature 只能发到 beta。
	featureNeeds3 := f.capabilityBuild(t, f.feature, `{}`, `{"commonApi": 3}`)
	if got := conflict(f.releaseErr(featureNeeds3, "", 0)); len(got.Conflicts) != 1 || got.Conflicts[0].ModuleID != f.common {
		t.Fatalf("default release should conflict with default common, got %+v", got.Conflicts)
	}
	if _, err := f.release(featureNeeds3, "beta", 0); err != nil {
		t.Fatalf("release feature to beta: %v", err)
	}
	// gamma 没有 Common，回落到默认 Common 2。
	if got := conflict(f.releaseErr(featureNeeds3, "gamma", 0)); len(got.Conflicts) != 1 || *got.Conflicts[0].Shortfalls[0].Provided != 2 {
		t.Fatalf("gamma release should conflict with fallback default common, got %+v", got.Conflicts)
	}
	featureNeeds2 := f.capabilityBuild(t, f.feature, `{}`, `{"commonApi": 2}`)
	f.mustRelease(t, featureNeeds2, "gamma", 0)

	// 默认 Common 降到 1：gamma 的 Feature 会回落拿到它，被拒绝；beta 有自己的 Common，不受影响。
	got := conflict(f.releaseErr(f.capabilityBuild(t, f.common, `{"commonApi": 1}`, `{}`), "", 0))
	if len(got.Conflicts) != 1 || got.Conflicts[0].BuildRecordID != featureNeeds2 {
		t.Fatalf("expected conflict with the gamma feature only, got %+v", got.Conflicts)
	}

	// beta 的 Common 降到 2 会让 beta 的 Feature（要求 3）不满足。
	got = conflict(f.releaseErr(f.capabilityBuild(t, f.common, `{"commonApi": 2}`, `{}`), "beta", 0))
	if len(got.Conflicts) != 1 || got.Conflicts[0].BuildRecordID != featureNeeds3 {
		t.Fatalf("expected conflict with the beta feature, got %+v", got.Conflicts)
	}
}
