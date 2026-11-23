package domain

import (
	"errors"
	"regexp"
	"time"
)

var (
	ErrInvalidBuildVariableInput = errors.New("invalid build variable request")
	ErrBuildVariableNotFound     = errors.New("build variable not found")
)

// 构建产物平台，即 dist/ 下的一级目录名。新增平台只需要在这里追加常量和列表。
const (
	PlatformAndroid   = "android"
	PlatformIOS       = "ios"
	PlatformHarmonyOS = "harmonyos"
)

var BuildPlatforms = []string{PlatformAndroid, PlatformIOS, PlatformHarmonyOS}

const (
	ForgeManifestFileName   = "forge.json"
	maxRuntimeVersionLength = 64
)

var (
	runtimeVersionPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	buildVariableKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

// ForgeManifest 对应产物根目录下的 forge.json，只包含平台需要的字段。
type ForgeManifest struct {
	RuntimeVersion string `json:"runtimeVersion"`
	// Provides、Requires 可选，见 Capabilities。
	Provides Capabilities `json:"provides"`
	Requires Capabilities `json:"requires"`
}

func IsValidPlatform(platform string) bool {
	for _, value := range BuildPlatforms {
		if value == platform {
			return true
		}
	}
	return false
}

// IsValidRuntimeVersion 校验 runtimeVersion：长度 1-64，字符集 [A-Za-z0-9._-]。平台只做精确匹配，不解释含义。
func IsValidRuntimeVersion(value string) bool {
	return len(value) <= maxRuntimeVersionLength && runtimeVersionPattern.MatchString(value)
}

func IsValidBuildVariableKey(key string) bool {
	return buildVariableKeyPattern.MatchString(key)
}

// ModuleBuildVariable 对应 module_build_variables。ValueEncrypted 只在存储层流转，Value 是解密后的明文，
// 密钥类变量的明文不得出现在接口响应和日志中。EnvironmentCode 为空表示适用于所有环境。
type ModuleBuildVariable struct {
	ModuleID        string
	Key             string
	ValueEncrypted  string
	Value           string
	IsSecret        bool
	EnvironmentCode string
	UpdatedByUserID string
	// UpdatedBy 是更新人的显示名称，读取时由存储层关联 users 表得到。
	UpdatedBy string
	UpdatedAt time.Time
}

// ModuleBuildVariableInput 是按 (ModuleID, Key, EnvironmentCode) upsert 构建变量的输入，Value 为明文。
type ModuleBuildVariableInput struct {
	ModuleID        string
	Key             string
	Value           string
	IsSecret        bool
	EnvironmentCode string
	UpdatedByUserID string
}

// BuildLogChunk 对应 build_log_chunks，seq 在单次构建内递增。
type BuildLogChunk struct {
	BuildRecordID string
	Seq           int
	Content       string
	CreatedAt     time.Time
}

type BuildLogPage struct {
	Chunks   []BuildLogChunk
	Finished bool
}
