package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrInvalidSigningInput = errors.New("invalid signing request")
	ErrSigningKeyNotFound  = errors.New("signing key not found")
	// ErrSigningKeyActive 表示要删除的是 active 密钥：先生成新密钥让它转为 retired。
	ErrSigningKeyActive = errors.New("signing key active")
	// ErrSigningKeyRequired 表示打开「只允许发布已签名的构建」时应用还没有 active 密钥。
	ErrSigningKeyRequired = errors.New("signing key required")
	// ErrReleaseBuildUnsigned 表示应用要求签名，但要发布的构建没有签名。
	ErrReleaseBuildUnsigned = errors.New("release build unsigned")
)

// ArtifactSigningAlgorithm 是包签名使用的算法，目前只有 Ed25519。
const ArtifactSigningAlgorithm = "ed25519"

// ArtifactSignaturePayloadVersion 是签名内容 JSON 的版本，字段变化时递增。
const ArtifactSignaturePayloadVersion = 1

type SigningKeyStatus string

const (
	SigningKeyStatusActive  SigningKeyStatus = "active"
	SigningKeyStatusRetired SigningKeyStatus = "retired"
)

// SigningKey 是应用的一把签名密钥，只含公开信息。私钥只在仓储和签名服务内部流转。
type SigningKey struct {
	ID            string
	ApplicationID string
	KeyID         string
	Algorithm     string
	// PublicKey 是 base64 编码的原始 32 字节 Ed25519 公钥。
	PublicKey       string
	Status          SigningKeyStatus
	CreatedByUserID string
	CreatedBy       string
	CreatedAt       time.Time
	RetiredAt       *time.Time
	// SignedBuilds 是用这把密钥签过的构建数。
	SignedBuilds int
}

type ApplicationSigning struct {
	RequireSignedReleases bool
	Keys                  []SigningKey
}

// NewSigningKeyInput 是仓储保存新密钥需要的数据，私钥已经加密。
type NewSigningKeyInput struct {
	ApplicationSlug      string
	KeyID                string
	PublicKey            string
	PrivateKeyCiphertext string
	CreatedByUserID      string
}

// ActiveSigningKey 是签名时用的 active 密钥，PrivateKeyCiphertext 由签名服务解密。
type ActiveSigningKey struct {
	KeyID                string
	PrivateKeyCiphertext string
}

// ArtifactSignature 是写回构建记录、随 manifest 下发的签名。未签名时三个字段都为空。
type ArtifactSignature struct {
	KeyID string
	// Payload 是被签名的 JSON 原文。
	Payload string
	// Signature 是 base64 编码的 Ed25519 签名。
	Signature string
}

// ArtifactSignaturePayload 是被签名的内容，字段说明见 docs/artifact-signing.md 第 4 节。
// 字段顺序就是 JSON 输出顺序；客户端对原始字节验签，不需要重新序列化。
type ArtifactSignaturePayload struct {
	Version        int                            `json:"version"`
	Application    string                         `json:"application"`
	ModuleID       string                         `json:"moduleId"`
	Environment    string                         `json:"environment"`
	BuildRecordID  string                         `json:"buildRecordId"`
	BuildNumber    int64                          `json:"buildNumber"`
	RuntimeVersion string                         `json:"runtimeVersion"`
	CommitSHA      string                         `json:"commitSha"`
	SignedAt       time.Time                      `json:"signedAt"`
	Files          []ArtifactSignaturePayloadFile `json:"files"`
}

type ArtifactSignaturePayloadFile struct {
	// Path 带平台前缀，例如 android/index.android.bundle。
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}

// SigningKeyID 由公钥算出：sha256 的前 8 个字节，十六进制。
func SigningKeyID(publicKey []byte) string {
	sum := sha256.Sum256(publicKey)
	return hex.EncodeToString(sum[:8])
}
