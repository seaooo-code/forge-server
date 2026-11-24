package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"forge-server/internal/domain"
)

type SigningKeyRepository interface {
	GetApplicationSigning(ctx context.Context, applicationSlug string) (domain.ApplicationSigning, error)
	CreateSigningKey(ctx context.Context, input domain.NewSigningKeyInput) (domain.SigningKey, error)
	DeleteSigningKey(ctx context.Context, applicationSlug string, keyID string) error
	SetRequireSignedReleases(ctx context.Context, applicationSlug string, require bool) error
	ActiveSigningKey(ctx context.Context, applicationSlug string) (domain.ActiveSigningKey, bool, error)
}

// ArtifactSigningService 管理应用的包签名密钥，并为 worker 签名产物文件列表，规则见 docs/artifact-signing.md。
// 私钥种子用 secretbox 加密后落库，只在签名时解密。
type ArtifactSigningService struct {
	repository SigningKeyRepository
	secrets    SecretBox
	now        func() time.Time
}

func NewArtifactSigningService(repository SigningKeyRepository, secrets SecretBox) *ArtifactSigningService {
	return &ArtifactSigningService{repository: repository, secrets: secrets, now: time.Now}
}

func (s *ArtifactSigningService) GetApplicationSigning(ctx context.Context, applicationSlug string) (domain.ApplicationSigning, error) {
	applicationSlug = strings.TrimSpace(applicationSlug)
	if !domain.IsValidApplicationSlug(applicationSlug) {
		return domain.ApplicationSigning{}, domain.ErrInvalidSigningInput
	}
	return s.repository.GetApplicationSigning(ctx, applicationSlug)
}

// CreateSigningKey 生成新的 Ed25519 密钥对作为 active 密钥，原 active 密钥转为 retired。
func (s *ArtifactSigningService) CreateSigningKey(ctx context.Context, applicationSlug string, actorUserID string) (domain.SigningKey, error) {
	applicationSlug = strings.TrimSpace(applicationSlug)
	if !domain.IsValidApplicationSlug(applicationSlug) {
		return domain.SigningKey{}, domain.ErrInvalidSigningInput
	}
	if s.secrets == nil {
		return domain.SigningKey{}, errors.New("signing secret box not configured")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return domain.SigningKey{}, fmt.Errorf("generate signing key: %w", err)
	}
	ciphertext, err := s.secrets.Encrypt(base64.StdEncoding.EncodeToString(privateKey.Seed()))
	if err != nil {
		return domain.SigningKey{}, fmt.Errorf("encrypt signing key: %w", err)
	}
	return s.repository.CreateSigningKey(ctx, domain.NewSigningKeyInput{
		ApplicationSlug:      applicationSlug,
		KeyID:                domain.SigningKeyID(publicKey),
		PublicKey:            base64.StdEncoding.EncodeToString(publicKey),
		PrivateKeyCiphertext: ciphertext,
		CreatedByUserID:      strings.TrimSpace(actorUserID),
	})
}

func (s *ArtifactSigningService) DeleteSigningKey(ctx context.Context, applicationSlug string, keyID string) error {
	applicationSlug, keyID = strings.TrimSpace(applicationSlug), strings.TrimSpace(keyID)
	if !domain.IsValidApplicationSlug(applicationSlug) || keyID == "" {
		return domain.ErrInvalidSigningInput
	}
	return s.repository.DeleteSigningKey(ctx, applicationSlug, keyID)
}

func (s *ArtifactSigningService) SetRequireSignedReleases(ctx context.Context, applicationSlug string, require bool) (domain.ApplicationSigning, error) {
	applicationSlug = strings.TrimSpace(applicationSlug)
	if !domain.IsValidApplicationSlug(applicationSlug) {
		return domain.ApplicationSigning{}, domain.ErrInvalidSigningInput
	}
	if err := s.repository.SetRequireSignedReleases(ctx, applicationSlug, require); err != nil {
		return domain.ApplicationSigning{}, err
	}
	return s.repository.GetApplicationSigning(ctx, applicationSlug)
}

// SignArtifact 用构建所属应用的 active 密钥签名产物文件列表。应用没有 active 密钥时返回 ok=false，构建照常完成、不带签名。
// files 的 Path 带平台前缀，顺序保持产物清单的顺序。
func (s *ArtifactSigningService) SignArtifact(ctx context.Context, record domain.BuildRecord, runtimeVersion string, commitSHA string, files []domain.ArtifactManifestFile) (domain.ArtifactSignature, bool, error) {
	key, ok, err := s.repository.ActiveSigningKey(ctx, record.ApplicationSlug)
	if err != nil || !ok {
		return domain.ArtifactSignature{}, false, err
	}
	if s.secrets == nil {
		return domain.ArtifactSignature{}, false, errors.New("signing secret box not configured")
	}
	seedText, err := s.secrets.Decrypt(key.PrivateKeyCiphertext)
	if err != nil {
		return domain.ArtifactSignature{}, false, fmt.Errorf("decrypt signing key %s: %w", key.KeyID, err)
	}
	seed, err := base64.StdEncoding.DecodeString(seedText)
	if err != nil || len(seed) != ed25519.SeedSize {
		return domain.ArtifactSignature{}, false, fmt.Errorf("signing key %s is malformed", key.KeyID)
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	if domain.SigningKeyID(privateKey.Public().(ed25519.PublicKey)) != key.KeyID {
		return domain.ArtifactSignature{}, false, fmt.Errorf("signing key %s does not match its key id", key.KeyID)
	}

	payload := domain.ArtifactSignaturePayload{
		Version:        domain.ArtifactSignaturePayloadVersion,
		Application:    record.ApplicationSlug,
		ModuleID:       record.ModuleID,
		Environment:    record.Environment,
		BuildRecordID:  record.ID,
		BuildNumber:    record.Number,
		RuntimeVersion: runtimeVersion,
		CommitSHA:      commitSHA,
		SignedAt:       s.now().UTC().Truncate(time.Second),
		Files:          make([]domain.ArtifactSignaturePayloadFile, 0, len(files)),
	}
	for _, file := range files {
		payload.Files = append(payload.Files, domain.ArtifactSignaturePayloadFile{Path: file.Path, SizeBytes: file.SizeBytes, SHA256: file.SHA256})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return domain.ArtifactSignature{}, false, fmt.Errorf("encode signature payload: %w", err)
	}
	return domain.ArtifactSignature{
		KeyID:     key.KeyID,
		Payload:   string(encoded),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, encoded)),
	}, true, nil
}
