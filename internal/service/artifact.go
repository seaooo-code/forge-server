package service

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"path"
	"slices"
	"strings"

	"forge-server/internal/domain"

	"github.com/google/uuid"
)

type ArtifactRepository interface {
	GetArtifactBuildRecord(ctx context.Context, input domain.ArtifactDownloadInput) (domain.BuildRecord, error)
	GetArtifactManifestSource(ctx context.Context, input domain.ArtifactManifestInput) (domain.ArtifactManifestSource, error)
}

type ArtifactBuildRepository interface {
	GetBuildRecord(ctx context.Context, id string) (domain.BuildRecord, error)
}

type ArtifactObjectReader interface {
	GetArtifactObject(ctx context.Context, record domain.BuildRecord, objectKey string) (domain.ArtifactObject, error)
}

// ArtifactPullRecorder 记录每次 manifest 请求的解析结果，由 ArtifactPullStatsService 实现。
type ArtifactPullRecorder interface {
	RecordManifest(manifest domain.ArtifactApplicationManifest)
}

type ArtifactService struct {
	buildRepository ArtifactBuildRepository
	manifestReader  ArtifactManifestReader
	objectReader    ArtifactObjectReader
	repository      ArtifactRepository
	pullRecorder    ArtifactPullRecorder
}

// WithPullRecorder 开启拉包统计。
func (s *ArtifactService) WithPullRecorder(recorder ArtifactPullRecorder) *ArtifactService {
	s.pullRecorder = recorder
	return s
}

func NewArtifactService(repository ArtifactRepository, buildRepository ArtifactBuildRepository, manifestReader ArtifactManifestReader, objectReader ArtifactObjectReader) *ArtifactService {
	return &ArtifactService{
		buildRepository: buildRepository,
		manifestReader:  manifestReader,
		objectReader:    objectReader,
		repository:      repository,
	}
}

func (s *ArtifactService) DownloadArtifactFile(ctx context.Context, input domain.ArtifactDownloadInput) (domain.ArtifactObject, error) {
	normalized, err := normalizeArtifactDownloadInput(input)
	if err != nil {
		return domain.ArtifactObject{}, err
	}
	if s.repository == nil || s.manifestReader == nil || s.objectReader == nil {
		return domain.ArtifactObject{}, domain.ErrArtifactNotFound
	}

	record, err := s.repository.GetArtifactBuildRecord(ctx, normalized)
	if err != nil {
		if errors.Is(err, domain.ErrBuildRecordNotFound) {
			return domain.ArtifactObject{}, domain.ErrArtifactNotFound
		}
		return domain.ArtifactObject{}, err
	}
	if record.Status != domain.BuildStatusSucceeded || record.ManifestKey == "" {
		return domain.ArtifactObject{}, domain.ErrArtifactNotFound
	}

	manifest, err := s.manifestReader.ReadArtifactManifest(ctx, record)
	if err != nil {
		return domain.ArtifactObject{}, err
	}
	// 拉包清单里的 path 已去掉平台前缀，带 platform 时拼回完整路径再查找。
	manifestPath := normalized.Path
	if normalized.Platform != "" {
		manifestPath = normalized.Platform + "/" + normalized.Path
	}
	file, ok := artifactManifestFileByPath(manifest, manifestPath)
	if !ok {
		return domain.ArtifactObject{}, domain.ErrArtifactNotFound
	}

	objectKey := ArtifactObjectKey(record, file)
	object, err := s.objectReader.GetArtifactObject(ctx, record, objectKey)
	if err != nil {
		return domain.ArtifactObject{}, err
	}
	object.ContentType = firstNonEmpty(object.ContentType, file.ContentType)
	if object.ContentLength <= 0 {
		object.ContentLength = file.SizeBytes
	}
	object.Name = path.Base(normalized.Path)
	object.ObjectKey = objectKey
	object.Path = normalized.Path
	return object, nil
}

func (s *ArtifactService) GetApplicationManifest(ctx context.Context, input domain.ArtifactManifestInput) (domain.ArtifactApplicationManifest, error) {
	normalized, err := normalizeArtifactManifestInput(input)
	if err != nil {
		return domain.ArtifactApplicationManifest{}, err
	}
	if s.repository == nil || s.buildRepository == nil || s.manifestReader == nil {
		return domain.ArtifactApplicationManifest{}, domain.ErrArtifactNotFound
	}

	source, err := s.repository.GetArtifactManifestSource(ctx, normalized)
	if err != nil {
		return domain.ArtifactApplicationManifest{}, err
	}

	result := domain.ArtifactApplicationManifest{
		Application:    source.Application,
		Environment:    source.Environment,
		Platform:       normalized.Platform,
		RuntimeVersion: normalized.RuntimeVersion,
		UserID:         normalized.UserID,
		Channel:        source.Channel,
		Modules:        []domain.ArtifactModuleManifest{},
		MissingModules: []domain.ArtifactMissingModule{},
	}
	// resolvedBuilds 记录已解析出构建的模块，用来在最后检查 Feature 与所属 Common 构建的能力是否兼容。
	resolvedBuilds := map[string]domain.BuildRecord{}
	for _, module := range source.Modules {
		if module.BuildRecordID == "" {
			reason := domain.ArtifactMissingReasonNoReleaseForRuntime
			if module.HasRuntimeRoute {
				// 该运行时版本下有路由，但都没有命中本次请求（例如只有未命中分桶的比例灰度）。
				reason = domain.ArtifactMissingReasonReleaseNotFound
			}
			result.MissingModules = append(result.MissingModules, missingArtifactModule(module, reason))
			continue
		}

		record, err := s.buildRepository.GetBuildRecord(ctx, module.BuildRecordID)
		if err != nil {
			if errors.Is(err, domain.ErrBuildRecordNotFound) {
				result.MissingModules = append(result.MissingModules, missingArtifactModule(module, domain.ArtifactMissingReasonBuildNotFound))
				continue
			}
			return domain.ArtifactApplicationManifest{}, err
		}
		if record.Status != domain.BuildStatusSucceeded || record.ManifestKey == "" {
			result.MissingModules = append(result.MissingModules, missingArtifactModule(module, domain.ArtifactMissingReasonArtifactUnavailable))
			continue
		}
		if !slices.Contains(record.Platforms, normalized.Platform) {
			result.MissingModules = append(result.MissingModules, missingArtifactModule(module, domain.ArtifactMissingReasonPlatformUnavailable))
			continue
		}

		manifest, err := s.manifestReader.ReadArtifactManifest(ctx, record)
		if err != nil {
			return domain.ArtifactApplicationManifest{}, err
		}

		if module.TestPlanMatches > 1 {
			// 冲突规则保证一个 userId 在一个模块上最多命中一张提测单，出现多张说明并发写入留下了例外，已按最近更新取了一张。
			slog.WarnContext(ctx, "artifact manifest matched multiple test plans", "module", module.ID, "environment", normalized.Environment, "userId", normalized.UserID, "matches", module.TestPlanMatches, "testPlan", module.TestPlanID)
		}
		resolvedBuilds[module.ID] = record
		result.Modules = append(result.Modules, domain.ArtifactModuleManifest{
			ID:             module.ID,
			Name:           module.Name,
			Kind:           module.Kind,
			ReleaseID:      module.ReleaseID,
			ReleaseType:    module.ReleaseType,
			GrayStrategy:   module.GrayStrategy,
			GrayPercentage: module.GrayPercentage,
			Source:         artifactSource(module),
			Channel:        module.Channel,
			TestPlanID:     module.TestPlanID,
			TestPlanName:   module.TestPlanName,
			BuildRecordID:  record.ID,
			Branch:         record.Branch,
			CommitSHA:      record.CommitSHA,
			ArtifactKey:    record.ArtifactKey,
			ManifestKey:    record.ManifestKey,
			Files:          resolvedArtifactManifestFiles(normalized, record, manifest),
		})
	}

	// 发布时已保证生效中的全量和灰度两两兼容；提测单只换其中一方时仍可能不兼容，这里按本次请求实际拿到的构建兜底。
	// 不兼容的 Feature 模块移到 missingModules，客户端回退到内置包。Common 没有可用构建时客户端整组回退，不需要再处理。
	commonModuleIDs := map[string]string{}
	for _, module := range source.Modules {
		commonModuleIDs[module.ID] = module.CommonModuleID
	}
	compatible := result.Modules[:0]
	for _, module := range result.Modules {
		common, ok := resolvedBuilds[commonModuleIDs[module.ID]]
		if module.Kind == domain.ModuleKindFeature && ok &&
			len(domain.UnmetCapabilities(resolvedBuilds[module.ID].RequiredCapabilities, common.ProvidedCapabilities)) > 0 {
			result.MissingModules = append(result.MissingModules, domain.ArtifactMissingModule{
				ID: module.ID, Name: module.Name, Kind: module.Kind, Reason: domain.ArtifactMissingReasonCommonIncompatible,
			})
			continue
		}
		compatible = append(compatible, module)
	}
	result.Modules = compatible
	if s.pullRecorder != nil {
		s.pullRecorder.RecordManifest(result)
	}
	return result, nil
}

func normalizeArtifactDownloadInput(input domain.ArtifactDownloadInput) (domain.ArtifactDownloadInput, error) {
	normalized := domain.ArtifactDownloadInput{
		ApplicationSlug: strings.TrimSpace(input.ApplicationSlug),
		ModuleID:        strings.TrimSpace(input.ModuleID),
		Environment:     strings.TrimSpace(input.Environment),
		BuildRecordID:   strings.TrimSpace(input.BuildRecordID),
		Platform:        strings.TrimSpace(input.Platform),
	}
	if normalized.Platform != "" && !domain.IsValidPlatform(normalized.Platform) {
		return domain.ArtifactDownloadInput{}, domain.ErrInvalidArtifactInput
	}
	artifactPath, err := normalizeArtifactFilePath(input.Path)
	if err != nil {
		return domain.ArtifactDownloadInput{}, err
	}
	normalized.Path = artifactPath
	if !domain.IsValidApplicationSlug(normalized.ApplicationSlug) ||
		!domain.IsValidModuleID(normalized.ModuleID) ||
		normalized.Environment == "" {
		return domain.ArtifactDownloadInput{}, domain.ErrInvalidArtifactInput
	}
	if _, err := uuid.Parse(normalized.BuildRecordID); err != nil {
		return domain.ArtifactDownloadInput{}, domain.ErrInvalidArtifactInput
	}
	return normalized, nil
}

func normalizeArtifactManifestInput(input domain.ArtifactManifestInput) (domain.ArtifactManifestInput, error) {
	normalized := domain.ArtifactManifestInput{
		ApplicationSlug: strings.TrimSpace(input.ApplicationSlug),
		Environment:     strings.TrimSpace(input.Environment),
		Platform:        strings.TrimSpace(input.Platform),
		RuntimeVersion:  strings.TrimSpace(input.RuntimeVersion),
		UserID:          strings.TrimSpace(input.UserID),
		Channel:         strings.TrimSpace(input.Channel),
	}
	if !domain.IsValidApplicationSlug(normalized.ApplicationSlug) ||
		normalized.Environment == "" ||
		len(normalized.UserID) > 128 ||
		normalized.Channel != "" && !domain.IsValidReleaseChannelCode(normalized.Channel) ||
		!domain.IsValidPlatform(normalized.Platform) ||
		!domain.IsValidRuntimeVersion(normalized.RuntimeVersion) {
		return domain.ArtifactManifestInput{}, domain.ErrInvalidArtifactInput
	}
	return normalized, nil
}

func normalizeArtifactFilePath(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.Contains(trimmed, "\\") || strings.HasPrefix(trimmed, "/") {
		return "", domain.ErrInvalidArtifactInput
	}
	for _, segment := range strings.Split(trimmed, "/") {
		if segment == ".." {
			return "", domain.ErrInvalidArtifactInput
		}
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", domain.ErrInvalidArtifactInput
	}
	return cleaned, nil
}

func artifactManifestFileByPath(manifest domain.ArtifactManifest, filePath string) (domain.ArtifactManifestFile, bool) {
	for _, file := range manifest.Files {
		if cleanArtifactRelativePath(file.Path) == filePath {
			return file, true
		}
	}
	return domain.ArtifactManifestFile{}, false
}

// resolvedArtifactManifestFiles 只返回 {platform}/ 下的文件，path 去掉平台前缀。
func resolvedArtifactManifestFiles(input domain.ArtifactManifestInput, record domain.BuildRecord, manifest domain.ArtifactManifest) []domain.ArtifactManifestResolvedFile {
	files := make([]domain.ArtifactManifestResolvedFile, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		filePath, ok := strings.CutPrefix(cleanArtifactRelativePath(file.Path), input.Platform+"/")
		if !ok || filePath == "" {
			continue
		}
		objectKey := ArtifactObjectKey(record, file)
		// 旧清单没有 digests，用 sha256 补齐，客户端可以只读 digests 做校验。
		digests := make(map[string]string, len(file.Digests)+1)
		for algorithm, value := range file.Digests {
			digests[algorithm] = value
		}
		if _, ok := digests["sha256"]; !ok && file.SHA256 != "" {
			digests["sha256"] = file.SHA256
		}
		files = append(files, domain.ArtifactManifestResolvedFile{
			Path:        filePath,
			URL:         artifactFileURL(input, record, objectKey, filePath),
			ContentType: file.ContentType,
			SizeBytes:   file.SizeBytes,
			SHA256:      file.SHA256,
			Digests:     digests,
		})
	}
	return files
}

func artifactFileURL(input domain.ArtifactManifestInput, record domain.BuildRecord, objectKey string, filePath string) string {
	if strings.TrimSpace(record.StoragePublicBaseURL) != "" && objectKey != "" {
		return strings.TrimRight(strings.TrimSpace(record.StoragePublicBaseURL), "/") + "/" + strings.TrimLeft(objectKey, "/")
	}
	values := url.Values{}
	values.Set("platform", input.Platform)
	values.Set("path", filePath)
	return "/api/v1/artifacts/applications/" +
		url.PathEscape(input.ApplicationSlug) +
		"/modules/" +
		url.PathEscape(record.ModuleID) +
		"/environments/" +
		url.PathEscape(input.Environment) +
		"/builds/" +
		url.PathEscape(record.ID) +
		"/file?" +
		values.Encode()
}

// ArtifactObjectKey 返回清单文件项在对象存储中的 key：优先用清单记录的 objectKey，旧清单按 {artifactKey}/{path} 推算。
// API 下载和 worker 读取依赖产物共用这条规则。
func ArtifactObjectKey(record domain.BuildRecord, file domain.ArtifactManifestFile) string {
	objectKey := strings.Trim(strings.TrimSpace(file.ObjectKey), "/")
	if objectKey != "" {
		return objectKey
	}
	return strings.TrimRight(strings.TrimSpace(record.ArtifactKey), "/") + "/" + cleanArtifactRelativePath(file.Path)
}

func artifactSource(module domain.ArtifactModuleReleaseSource) domain.ArtifactSource {
	switch {
	case module.TestPlanID != "":
		return domain.ArtifactSourceTestPlan
	case module.ReleaseType == domain.ReleaseTypeGray && module.GrayStrategy == domain.ReleaseGrayStrategyPercentage:
		return domain.ArtifactSourcePercentage
	default:
		return domain.ArtifactSourceFull
	}
}

func missingArtifactModule(module domain.ArtifactModuleReleaseSource, reason string) domain.ArtifactMissingModule {
	return domain.ArtifactMissingModule{
		ID:     module.ID,
		Name:   module.Name,
		Kind:   module.Kind,
		Reason: reason,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
