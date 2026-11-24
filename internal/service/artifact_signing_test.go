package service

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"forge-server/internal/domain"
)

type fakeSigningKeyRepository struct {
	created domain.NewSigningKeyInput
	active  *domain.ActiveSigningKey
}

func (r *fakeSigningKeyRepository) GetApplicationSigning(context.Context, string) (domain.ApplicationSigning, error) {
	return domain.ApplicationSigning{}, nil
}

func (r *fakeSigningKeyRepository) CreateSigningKey(_ context.Context, input domain.NewSigningKeyInput) (domain.SigningKey, error) {
	r.created = input
	r.active = &domain.ActiveSigningKey{KeyID: input.KeyID, PrivateKeyCiphertext: input.PrivateKeyCiphertext}
	return domain.SigningKey{KeyID: input.KeyID, PublicKey: input.PublicKey, Status: domain.SigningKeyStatusActive}, nil
}

func (r *fakeSigningKeyRepository) DeleteSigningKey(context.Context, string, string) error {
	return nil
}

func (r *fakeSigningKeyRepository) SetRequireSignedReleases(context.Context, string, bool) error {
	return nil
}

func (r *fakeSigningKeyRepository) ActiveSigningKey(context.Context, string) (domain.ActiveSigningKey, bool, error) {
	if r.active == nil {
		return domain.ActiveSigningKey{}, false, nil
	}
	return *r.active, true, nil
}

// reverseSecretBox 是可逆的假加密，确认落库的不是私钥明文。
type reverseSecretBox struct{}

func (reverseSecretBox) Encrypt(plaintext string) (string, error) {
	return "enc:" + reverse(plaintext), nil
}

func (reverseSecretBox) Decrypt(ciphertext string) (string, error) {
	value, ok := strings.CutPrefix(ciphertext, "enc:")
	if !ok {
		return "", errors.New("bad ciphertext")
	}
	return reverse(value), nil
}

func reverse(value string) string {
	runes := []rune(value)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// 生成的密钥签出的签名能用公开的公钥验证，签名内容带上构建身份和带平台前缀的文件列表。
func TestArtifactSigningServiceSignsVerifiablePayload(t *testing.T) {
	repository := &fakeSigningKeyRepository{}
	service := NewArtifactSigningService(repository, reverseSecretBox{})
	service.now = func() time.Time { return time.Date(2026, 11, 24, 3, 17, 2, 500, time.UTC) }
	ctx := context.Background()
	record := domain.BuildRecord{ID: "build-1", Number: 12, ApplicationSlug: "forge-demo", ModuleID: "claw", Environment: "test"}
	files := []domain.ArtifactManifestFile{{Path: "android/index.android.bundle", SizeBytes: 10, SHA256: "aa", ObjectKey: "k/android/index.android.bundle"}}

	if _, ok, err := service.SignArtifact(ctx, record, "rt-1", "sha", files); err != nil || ok {
		t.Fatalf("without an active key the build stays unsigned, got ok=%v err=%v", ok, err)
	}

	key, err := service.CreateSigningKey(ctx, "forge-demo", "ou-1")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	publicKey, err := base64.StdEncoding.DecodeString(key.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		t.Fatalf("public key should be raw 32 bytes in base64, got %q %v", key.PublicKey, err)
	}
	if key.KeyID != domain.SigningKeyID(publicKey) || len(key.KeyID) != 16 {
		t.Fatalf("unexpected key id %q", key.KeyID)
	}
	if !strings.HasPrefix(repository.created.PrivateKeyCiphertext, "enc:") {
		t.Fatalf("private key must be stored encrypted, got %q", repository.created.PrivateKeyCiphertext)
	}

	signature, ok, err := service.SignArtifact(ctx, record, "rt-1", "sha", files)
	if err != nil || !ok || signature.KeyID != key.KeyID {
		t.Fatalf("sign: %+v ok=%v err=%v", signature, ok, err)
	}
	rawSignature, _ := base64.StdEncoding.DecodeString(signature.Signature)
	if !ed25519.Verify(publicKey, []byte(signature.Payload), rawSignature) {
		t.Fatal("signature does not verify with the public key")
	}
	if ed25519.Verify(publicKey, []byte(strings.Replace(signature.Payload, `"aa"`, `"bb"`, 1)), rawSignature) {
		t.Fatal("tampered payload must not verify")
	}
	var payload domain.ArtifactSignaturePayload
	if err := json.Unmarshal([]byte(signature.Payload), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Version != 1 || payload.Application != "forge-demo" || payload.ModuleID != "claw" || payload.Environment != "test" ||
		payload.BuildRecordID != "build-1" || payload.BuildNumber != 12 || payload.RuntimeVersion != "rt-1" || payload.CommitSHA != "sha" ||
		!payload.SignedAt.Equal(time.Date(2026, 11, 24, 3, 17, 2, 0, time.UTC)) ||
		len(payload.Files) != 1 || payload.Files[0] != (domain.ArtifactSignaturePayloadFile{Path: "android/index.android.bundle", SizeBytes: 10, SHA256: "aa"}) {
		t.Fatalf("unexpected payload: %s", signature.Payload)
	}
	if strings.Contains(signature.Payload, "objectKey") {
		t.Fatalf("payload must not carry storage object keys: %s", signature.Payload)
	}

	// 私钥密文损坏时签名失败，构建会因此失败而不是悄悄不签名。
	repository.active.PrivateKeyCiphertext = "broken"
	if _, _, err := service.SignArtifact(ctx, record, "rt-1", "sha", files); err == nil {
		t.Fatal("expected signing to fail with a broken key")
	}
}
