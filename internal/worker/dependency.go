package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"forge-server/internal/domain"
	s3infra "forge-server/internal/infra/s3"
	"forge-server/internal/service"
)

// BuildDependencyProvider 解析构建需要注入的依赖构建，并把注入结果写回构建记录，由 service.BuildDependencyService 实现。
type BuildDependencyProvider interface {
	ResolveBuildDependencies(ctx context.Context, record domain.BuildRecord) (service.BuildDependencyPlan, bool, error)
	RecordBuildDependencies(ctx context.Context, buildID string, dependencies []domain.BuildDependency) error
}

// DependencyArtifactReader 按依赖构建自己的存储配置读取产物清单和文件，默认实现是 infra/s3。
type DependencyArtifactReader interface {
	service.ArtifactManifestReader
	service.ArtifactObjectReader
}

type s3DependencyArtifactReader struct {
	s3infra.ManifestReader
	s3infra.ObjectReader
}

// buildDependenciesDirArgValue 是 FORGE_DEPS_DIR 的值，相对构建上下文，Dockerfile 里可以直接 COPY。
const buildDependenciesDirArgValue = domain.BuildAdapterFilesDir + "/" + domain.BuildDependenciesDir

// dependencyIndex 是 .forge/deps/common/index.json 的内容。
type dependencyIndex struct {
	ModuleID    string                `json:"moduleId"`
	Environment string                `json:"environment"`
	Items       []dependencyIndexItem `json:"items"`
}

type dependencyIndexItem struct {
	RuntimeVersion string   `json:"runtimeVersion"`
	BuildRecordID  string   `json:"buildRecordId"`
	BuildNumber    int64    `json:"buildNumber"`
	CommitSHA      string   `json:"commitSha"`
	Platforms      []string `json:"platforms"`
}

type dependencyFile struct {
	build *domain.BuildRecord
	file  domain.ArtifactManifestFile
	// path 是 {platform}/{平台目录内相对路径}，已校验不会越界。
	path     string
	platform string
}

// ensureForgeDirAvailable 在需要写入 .forge 时确认仓库里没有同名的文件、目录或软链，避免覆盖仓库内容。
// .forge 由 worker 新建，内部不会有仓库带来的软链。
func ensureForgeDirAvailable(contextDir string) error {
	forgeDir := filepath.Join(contextDir, domain.BuildAdapterFilesDir)
	if _, err := os.Lstat(forgeDir); err == nil {
		return fmt.Errorf("构建上下文中已存在 %s，Forge 需要在其中写入构建适配器辅助文件或依赖产物，不能覆盖仓库内容", domain.BuildAdapterFilesDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("检查 %s 目录失败：%w", domain.BuildAdapterFilesDir, err)
	}
	return nil
}

// injectBuildDependencies 把依赖构建的平台目录下载到 {构建上下文}/.forge/deps/common/{runtimeVersion}/{platform}/，
// 并写入 index.json。先读取全部清单、校验路径和总大小，再逐个下载并校验 sha256。调用前必须已通过 ensureForgeDirAvailable。
func (e *Executor) injectBuildDependencies(ctx context.Context, contextDir string, plan service.BuildDependencyPlan, logs *buildLogStream) ([]domain.BuildDependency, error) {
	depsRoot := filepath.Join(contextDir, domain.BuildAdapterFilesDir, domain.BuildDependenciesDir, domain.BuildCommonDependencyDir)
	if err := os.MkdirAll(depsRoot, 0o755); err != nil {
		return nil, fmt.Errorf("创建依赖目录失败：%w", err)
	}
	realDepsRoot, err := filepath.EvalSymlinks(depsRoot)
	if err != nil {
		return nil, fmt.Errorf("解析依赖目录失败：%w", err)
	}

	index := dependencyIndex{ModuleID: plan.ModuleID, Environment: plan.Environment, Items: []dependencyIndexItem{}}
	dependencies := make([]domain.BuildDependency, 0, len(plan.Builds))
	if len(plan.Builds) == 0 {
		logs.Linef("警告：所属 Common 模块 %s 在环境 %s 下没有生效中的默认路由，只写入空的 %s", plan.ModuleID, plan.Environment, domain.BuildDependencyIndexFileName)
		return dependencies, writeDependencyIndex(depsRoot, index)
	}
	logs.Linef("注入所属 Common 模块 %s 在环境 %s 的生效产物：%d 个运行时版本", plan.ModuleID, plan.Environment, len(plan.Builds))

	maxBytes := e.cfg.DepsMaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultDepsMaxBytes
	}
	files := []dependencyFile{}
	var totalBytes int64
	for i := range plan.Builds {
		build := &plan.Builds[i]
		// runtimeVersion 会作为目录名：合法字符集仍允许 . 和 ..，还要避开 index.json。
		if rv := build.RuntimeVersion; !domain.IsValidRuntimeVersion(rv) || rv == "." || rv == ".." || rv == domain.BuildDependencyIndexFileName {
			return nil, fmt.Errorf("依赖构建 #%d 的 runtimeVersion %q 不能作为依赖目录名", build.Number, rv)
		}
		manifest, err := e.dependencyArtifacts.ReadArtifactManifest(ctx, *build)
		if err != nil {
			return nil, fmt.Errorf("读取依赖构建 #%d 的产物清单失败：%w", build.Number, err)
		}
		for _, file := range manifest.Files {
			filePath, platform, ok, err := dependencyFilePath(file.Path, build.Platforms)
			if err != nil {
				return nil, fmt.Errorf("依赖构建 #%d：%w", build.Number, err)
			}
			if !ok {
				continue
			}
			if file.SizeBytes < 0 || strings.TrimSpace(file.SHA256) == "" {
				return nil, fmt.Errorf("依赖构建 #%d 的清单文件 %s 缺少大小或 sha256", build.Number, filePath)
			}
			totalBytes += file.SizeBytes
			if totalBytes > maxBytes {
				return nil, fmt.Errorf("依赖产物总大小超过上限 %d 字节（FORGE_WORKER_DEPS_MAX_BYTES）", maxBytes)
			}
			files = append(files, dependencyFile{build: build, file: file, path: filePath, platform: platform})
		}
	}

	for _, item := range files {
		target, err := artifactLocalPath(filepath.Join(depsRoot, item.build.RuntimeVersion), item.path)
		if err != nil {
			return nil, fmt.Errorf("依赖构建 #%d：%w", item.build.Number, err)
		}
		if err := downloadDependencyFile(ctx, e.dependencyArtifacts, realDepsRoot, target, *item.build, item.file); err != nil {
			return nil, fmt.Errorf("依赖构建 #%d 的文件 %s：%w", item.build.Number, item.path, err)
		}
	}

	for i := range plan.Builds {
		build := &plan.Builds[i]
		platforms := []string{}
		fileCount := 0
		var sizeBytes int64
		for _, item := range files {
			if item.build != build {
				continue
			}
			fileCount++
			sizeBytes += item.file.SizeBytes
			if !slices.Contains(platforms, item.platform) {
				platforms = append(platforms, item.platform)
			}
		}
		slices.Sort(platforms)
		index.Items = append(index.Items, dependencyIndexItem{
			RuntimeVersion: build.RuntimeVersion,
			BuildRecordID:  build.ID,
			BuildNumber:    build.Number,
			CommitSHA:      build.CommitSHA,
			Platforms:      platforms,
		})
		dependencies = append(dependencies, domain.BuildDependency{
			ModuleID:       plan.ModuleID,
			RuntimeVersion: build.RuntimeVersion,
			BuildRecordID:  build.ID,
		})
		logs.Linef("已注入依赖：runtimeVersion %s，构建 #%d（commit %s），平台 %s，%d 个文件，%d 字节",
			build.RuntimeVersion, build.Number, build.CommitSHA[:min(7, len(build.CommitSHA))], strings.Join(platforms, ","), fileCount, sizeBytes)
	}
	return dependencies, writeDependencyIndex(depsRoot, index)
}

// defaultDepsMaxBytes 与 FORGE_WORKER_DEPS_MAX_BYTES 的默认值一致，测试等未配置上限的场景兜底。
const defaultDepsMaxBytes int64 = 200 * 1024 * 1024

// dependencyFilePath 校验清单路径并只保留平台目录下的文件：拒绝空路径、绝对路径、反斜杠和任何 .. 段，
// 根目录文件（例如 forge.json）和不属于构建平台的目录返回 ok=false。
func dependencyFilePath(value string, platforms []string) (string, string, bool, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.Contains(trimmed, "\\") || path.IsAbs(trimmed) || slices.Contains(strings.Split(trimmed, "/"), "..") {
		return "", "", false, fmt.Errorf("清单包含非法路径 %q", value)
	}
	cleaned := path.Clean(trimmed)
	platform, rest, found := strings.Cut(cleaned, "/")
	if !found || rest == "" || !domain.IsValidPlatform(platform) || !slices.Contains(platforms, platform) {
		return "", "", false, nil
	}
	return cleaned, platform, true, nil
}

// downloadDependencyFile 下载一个依赖文件并校验大小和 sha256。目标文件用 O_EXCL 创建，不会跟随已存在的软链；
// 父目录解析软链后必须仍在依赖目录内。
func downloadDependencyFile(ctx context.Context, reader DependencyArtifactReader, realDepsRoot string, target string, build domain.BuildRecord, file domain.ArtifactManifestFile) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录失败：%w", err)
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("解析目录失败：%w", err)
	}
	if inside, err := filepath.Rel(realDepsRoot, realDir); err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return errors.New("目标目录指向依赖目录外部")
	}

	object, err := reader.GetArtifactObject(ctx, build, service.ArtifactObjectKey(build, file))
	if err != nil {
		return fmt.Errorf("下载失败：%w", err)
	}
	defer object.Body.Close()

	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("创建文件失败：%w", err)
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(object.Body, file.SizeBytes+1))
	if err := errors.Join(copyErr, output.Close()); err != nil {
		return fmt.Errorf("写入失败：%w", err)
	}
	if size != file.SizeBytes {
		return fmt.Errorf("大小与清单不一致：清单 %d 字节，下载 %d 字节", file.SizeBytes, size)
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(actual, strings.TrimSpace(file.SHA256)) {
		return fmt.Errorf("sha256 校验失败：清单 %s，下载 %s", file.SHA256, actual)
	}
	return nil
}

func writeDependencyIndex(depsRoot string, index dependencyIndex) error {
	payload, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("编码 %s 失败：%w", domain.BuildDependencyIndexFileName, err)
	}
	target := filepath.Join(depsRoot, domain.BuildDependencyIndexFileName)
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("写入 %s 失败：%w", domain.BuildDependencyIndexFileName, err)
	}
	_, writeErr := output.Write(append(payload, '\n'))
	if err := errors.Join(writeErr, output.Close()); err != nil {
		return fmt.Errorf("写入 %s 失败：%w", domain.BuildDependencyIndexFileName, err)
	}
	return nil
}
