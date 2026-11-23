package worker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"forge-server/internal/domain"
)

const maxForgeManifestBytes = 64 * 1024

// validateArtifactContract 校验导出产物是否满足构建契约：根目录只有 forge.json 和平台目录，至少一个平台，
// 每个平台目录至少有一个文件，forge.json 只包含合法的 runtimeVersion。返回 runtimeVersion 和按名称排序的平台列表。
func validateArtifactContract(artifactRoot string) (domain.ForgeManifest, []string, error) {
	entries, err := os.ReadDir(artifactRoot)
	if err != nil {
		return domain.ForgeManifest{}, nil, fmt.Errorf("产物校验失败：读取产物目录失败：%w", err)
	}

	hasManifest := false
	platforms := []string{}
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case name == domain.ForgeManifestFileName:
			if !entry.Type().IsRegular() {
				return domain.ForgeManifest{}, nil, fmt.Errorf("产物校验失败：%s 必须是普通文件", domain.ForgeManifestFileName)
			}
			hasManifest = true
		case entry.IsDir():
			if !domain.IsValidPlatform(name) {
				return domain.ForgeManifest{}, nil, fmt.Errorf("产物校验失败：根目录下的 %q 不是合法的平台目录，只允许 %s", name, strings.Join(domain.BuildPlatforms, "、"))
			}
			hasFile, err := directoryHasRegularFile(filepath.Join(artifactRoot, name))
			if err != nil {
				return domain.ForgeManifest{}, nil, fmt.Errorf("产物校验失败：读取平台目录 %s 失败：%w", name, err)
			}
			if !hasFile {
				return domain.ForgeManifest{}, nil, fmt.Errorf("产物校验失败：平台目录 %s 没有任何文件", name)
			}
			platforms = append(platforms, name)
		default:
			return domain.ForgeManifest{}, nil, fmt.Errorf("产物校验失败：根目录只允许 %s 和平台目录，发现 %q", domain.ForgeManifestFileName, name)
		}
	}
	if !hasManifest {
		return domain.ForgeManifest{}, nil, fmt.Errorf("产物校验失败：根目录缺少 %s", domain.ForgeManifestFileName)
	}
	if len(platforms) == 0 {
		return domain.ForgeManifest{}, nil, fmt.Errorf("产物校验失败：至少需要一个平台目录（%s）", strings.Join(domain.BuildPlatforms, "、"))
	}

	manifest, err := readForgeManifest(filepath.Join(artifactRoot, domain.ForgeManifestFileName))
	if err != nil {
		return domain.ForgeManifest{}, nil, fmt.Errorf("产物校验失败：%w", err)
	}
	return manifest, platforms, nil
}

// readForgeManifest 严格解析 forge.json：必须是单个 JSON 对象，键名大小写精确匹配，不允许未知字段。
// encoding/json 的字段匹配不区分大小写，所以先解析成 map 再逐个检查键名。
func readForgeManifest(path string) (domain.ForgeManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return domain.ForgeManifest{}, fmt.Errorf("读取 %s 失败：%w", domain.ForgeManifestFileName, err)
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, maxForgeManifestBytes+1))
	if err != nil {
		return domain.ForgeManifest{}, fmt.Errorf("读取 %s 失败：%w", domain.ForgeManifestFileName, err)
	}
	if len(content) > maxForgeManifestBytes {
		return domain.ForgeManifest{}, fmt.Errorf("%s 超过 %d 字节", domain.ForgeManifestFileName, maxForgeManifestBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(content))
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		return domain.ForgeManifest{}, fmt.Errorf("%s 必须是 JSON 对象", domain.ForgeManifestFileName)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return domain.ForgeManifest{}, fmt.Errorf("%s 在 JSON 对象之后还有多余内容", domain.ForgeManifestFileName)
	}
	for key := range fields {
		if key != "runtimeVersion" && key != "provides" && key != "requires" {
			return domain.ForgeManifest{}, fmt.Errorf("%s 包含未知字段 %q，只允许 runtimeVersion、provides、requires", domain.ForgeManifestFileName, key)
		}
	}
	raw, ok := fields["runtimeVersion"]
	if !ok {
		return domain.ForgeManifest{}, fmt.Errorf("%s 缺少 runtimeVersion", domain.ForgeManifestFileName)
	}
	var runtimeVersion string
	if err := json.Unmarshal(raw, &runtimeVersion); err != nil {
		return domain.ForgeManifest{}, fmt.Errorf("%s 的 runtimeVersion 必须是字符串", domain.ForgeManifestFileName)
	}
	if !domain.IsValidRuntimeVersion(runtimeVersion) {
		return domain.ForgeManifest{}, fmt.Errorf("%s 的 runtimeVersion %q 不合法：长度 1-64，只允许字母、数字和 . _ -", domain.ForgeManifestFileName, runtimeVersion)
	}
	manifest := domain.ForgeManifest{RuntimeVersion: runtimeVersion, Provides: domain.Capabilities{}, Requires: domain.Capabilities{}}
	for key, target := range map[string]*domain.Capabilities{"provides": &manifest.Provides, "requires": &manifest.Requires} {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		var capabilities domain.Capabilities
		// 能力等级必须是整数，json 解码到 int64 会拒绝小数和超出范围的值。
		if err := json.Unmarshal(raw, &capabilities); err != nil || capabilities == nil {
			return domain.ForgeManifest{}, fmt.Errorf("%s 的 %s 必须是对象，值为非负整数，例如 {\"commonApi\": 3}", domain.ForgeManifestFileName, key)
		}
		if !domain.IsValidCapabilities(capabilities) {
			return domain.ForgeManifest{}, fmt.Errorf("%s 的 %s 不合法：最多 %d 项，能力名以字母开头、长度不超过 64、只允许字母、数字和 . _ -，等级为 0 到 %d 的整数",
				domain.ForgeManifestFileName, key, domain.MaxCapabilities, domain.MaxCapabilityLevel)
		}
		*target = capabilities
	}
	return manifest, nil
}

func directoryHasRegularFile(dir string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found, err
}
