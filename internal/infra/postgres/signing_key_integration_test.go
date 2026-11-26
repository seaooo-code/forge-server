package postgres

import (
	"errors"
	"testing"

	"forge-server/internal/domain"
)

// 生成新密钥会把原 active 密钥转为 retired；active 密钥不能删除；打开「只允许发布已签名的构建」需要 active 密钥，
// 打开后未签名构建不能发布，签名写回构建记录后可以发布，manifest 来源构建带上签名。
func TestIntegrationSigningKeysAndSignedReleases(t *testing.T) {
	f := newChannelFixture(t)
	ctx := integrationCtx()
	keys := NewSigningKeyRepository(f.pool)
	builds := NewBuildRepository(f.pool)

	if err := keys.SetRequireSignedReleases(ctx, f.slug, true); !errors.Is(err, domain.ErrSigningKeyRequired) {
		t.Fatalf("expected signing key required, got %v", err)
	}
	if _, ok, err := keys.ActiveSigningKey(ctx, f.slug); err != nil || ok {
		t.Fatalf("expected no active key, got ok=%v err=%v", ok, err)
	}

	newKey := func(keyID string) domain.SigningKey {
		t.Helper()
		key, err := keys.CreateSigningKey(ctx, domain.NewSigningKeyInput{
			ApplicationSlug: f.slug, KeyID: keyID, PublicKey: "pub-" + keyID, PrivateKeyCiphertext: "enc-" + keyID, CreatedByUserID: f.userID,
		})
		if err != nil {
			t.Fatalf("create key %s: %v", keyID, err)
		}
		return key
	}
	suffix := f.application[len(f.application)-8:]
	firstID, secondID := "aaaaaaaa"+suffix, "bbbbbbbb"+suffix
	first := newKey(firstID)
	if first.Status != domain.SigningKeyStatusActive || first.CreatedBy == "" {
		t.Fatalf("unexpected first key: %+v", first)
	}
	newKey(secondID)
	signing, err := keys.GetApplicationSigning(ctx, f.slug)
	if err != nil || len(signing.Keys) != 2 || signing.Keys[0].KeyID != secondID || signing.Keys[0].Status != domain.SigningKeyStatusActive ||
		signing.Keys[1].Status != domain.SigningKeyStatusRetired || signing.Keys[1].RetiredAt == nil {
		t.Fatalf("expected the new key active and the old one retired, got %+v %v", signing.Keys, err)
	}
	if active, ok, err := keys.ActiveSigningKey(ctx, f.slug); err != nil || !ok || active.KeyID != secondID || active.PrivateKeyCiphertext != "enc-"+secondID {
		t.Fatalf("unexpected active key %+v ok=%v err=%v", active, ok, err)
	}
	if err := keys.DeleteSigningKey(ctx, f.slug, secondID); !errors.Is(err, domain.ErrSigningKeyActive) {
		t.Fatalf("expected active key deletion to fail, got %v", err)
	}
	if err := keys.DeleteSigningKey(ctx, f.slug, firstID); err != nil {
		t.Fatalf("delete retired key: %v", err)
	}
	if err := keys.DeleteSigningKey(ctx, f.slug, firstID); !errors.Is(err, domain.ErrSigningKeyNotFound) {
		t.Fatalf("expected deleted key to be gone, got %v", err)
	}

	if err := keys.SetRequireSignedReleases(ctx, f.slug, true); err != nil {
		t.Fatalf("require signed releases: %v", err)
	}
	unsigned, _ := f.build(t, f.feature, "main", "rt-1")
	if _, err := f.release(unsigned, "", 0); !errors.Is(err, domain.ErrReleaseBuildUnsigned) {
		t.Fatalf("expected unsigned build to be rejected, got %v", err)
	}

	// 走 CompleteBuildRecord 写回签名，和 worker 的路径一致。
	record, err := builds.CreateBuildRecord(ctx, domain.BuildRecordInput{ModuleID: f.feature, Environment: f.environment, TriggerType: domain.BuildTriggerTypeManual})
	if err != nil {
		t.Fatalf("create build: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `update build_records set status = 'running', worker_id = 'worker-1' where id = $1`, record.ID); err != nil {
		t.Fatalf("mark build running: %v", err)
	}
	signature := domain.ArtifactSignature{KeyID: secondID, Payload: `{"version":1}`, Signature: "c2ln"}
	completed, err := builds.CompleteBuildRecord(ctx, record.ID, "worker-1", domain.BuildRecordCompletion{
		Signature: signature, CommitSHA: "sha", RuntimeVersion: "rt-1", Platforms: []string{"android"},
		ArtifactKey: "k/" + record.ID, ManifestKey: "k/" + record.ID + "/manifest.json", FileCount: 1, TotalSizeBytes: 1,
	})
	if err != nil || completed.Signature != signature {
		t.Fatalf("complete build with signature: %+v %v", completed.Signature, err)
	}
	if _, err := f.release(record.ID, "", 0); err != nil {
		t.Fatalf("release signed build: %v", err)
	}
	if got, err := builds.GetBuildRecord(ctx, record.ID); err != nil || got.Signature != signature {
		t.Fatalf("signature should be readable from the build record, got %+v %v", got.Signature, err)
	}
	signing, err = keys.GetApplicationSigning(ctx, f.slug)
	if err != nil || !signing.RequireSignedReleases || signing.Keys[0].SignedBuilds != 1 {
		t.Fatalf("expected one signed build on the active key, got %+v %v", signing, err)
	}

	if err := keys.SetRequireSignedReleases(ctx, f.slug, false); err != nil {
		t.Fatalf("turn off require signed releases: %v", err)
	}
	if _, err := f.release(unsigned, "", 0); err != nil {
		t.Fatalf("unsigned build should be releasable after turning the switch off: %v", err)
	}
}
