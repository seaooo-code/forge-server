package service

import (
	"context"
	"encoding/json"
	"io"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"forge-server/internal/domain"
)

type BuildRecordRepository interface {
	AcquireNextBuildRecord(ctx context.Context, workerID string, leaseDuration time.Duration, modes []domain.BuildMode) (domain.BuildRecord, error)
	CompleteBuildRecord(ctx context.Context, id string, workerID string, completion domain.BuildRecordCompletion) (domain.BuildRecord, error)
	CreateBuildRecord(ctx context.Context, input domain.BuildRecordInput) (domain.BuildRecord, error)
	FailBuildRecord(ctx context.Context, id string, workerID string, message string) (domain.BuildRecord, error)
	GetBuildQueueStats(ctx context.Context) (domain.BuildQueueStats, error)
	GetBuildRecord(ctx context.Context, id string) (domain.BuildRecord, error)
	ListBuildRecords(ctx context.Context, filters domain.BuildRecordFilters) ([]domain.BuildRecord, domain.BuildRecordSummary, error)
	RecordBuildCIPipeline(ctx context.Context, id string, workerID string, pipelineID int64, pipelineURL string) error
	RenewBuildLease(ctx context.Context, id string, workerID string, leaseDuration time.Duration) error
	RequeueBuildRecord(ctx context.Context, id string, workerID string) error
}

type ArtifactManifestReader interface {
	ReadArtifactManifest(ctx context.Context, record domain.BuildRecord) (domain.ArtifactManifest, error)
}

// BuildSucceededHook 在构建成功落库后被调用，例如按模块配置创建预览通道发布。
// 实现自行处理并记录错误，不能影响构建状态。
type BuildSucceededHook interface {
	HandleBuildSucceeded(ctx context.Context, record domain.BuildRecord)
}

type BuildService struct {
	manifestReader ArtifactManifestReader
	repository     BuildRecordRepository
	succeededHook  BuildSucceededHook
}

func NewBuildService(repository BuildRecordRepository) *BuildService {
	return &BuildService{repository: repository}
}

func NewBuildServiceWithManifestReader(repository BuildRecordRepository, manifestReader ArtifactManifestReader) *BuildService {
	return &BuildService{repository: repository, manifestReader: manifestReader}
}

// WithSucceededHook 注入构建成功回调。API 进程和 worker 进程在装配时都需要注入。
func (s *BuildService) WithSucceededHook(hook BuildSucceededHook) *BuildService {
	s.succeededHook = hook
	return s
}

func (s *BuildService) CreateBuildRecord(ctx context.Context, input domain.BuildRecordInput) (domain.BuildRecord, error) {
	normalized, err := validateBuildRecordInput(input)
	if err != nil {
		return domain.BuildRecord{}, err
	}
	return s.repository.CreateBuildRecord(ctx, normalized)
}

func (s *BuildService) ListBuildRecords(ctx context.Context, filters domain.BuildRecordFilters) ([]domain.BuildRecord, domain.BuildRecordSummary, error) {
	normalized, err := normalizeBuildRecordFilters(filters)
	if err != nil {
		return nil, domain.BuildRecordSummary{}, err
	}
	return s.repository.ListBuildRecords(ctx, normalized)
}

func (s *BuildService) GetBuildRecord(ctx context.Context, id string) (domain.BuildRecord, error) {
	normalizedID := strings.TrimSpace(id)
	if normalizedID == "" {
		return domain.BuildRecord{}, domain.ErrBuildRecordNotFound
	}
	record, err := s.repository.GetBuildRecord(ctx, normalizedID)
	if err != nil {
		return domain.BuildRecord{}, err
	}
	record.ArtifactTree = domain.ArtifactTree{
		FileCount:      record.FileCount,
		TotalSizeBytes: record.TotalSizeBytes,
		Nodes:          []domain.ArtifactTreeNode{},
	}
	if record.ManifestKey == "" || s.manifestReader == nil {
		return record, nil
	}

	manifest, err := s.manifestReader.ReadArtifactManifest(ctx, record)
	if err != nil {
		return record, nil
	}
	record.ArtifactTree = buildArtifactTree(manifest)
	return record, nil
}

// AcquireNextBuildRecord 领取下一个待执行的构建。modes 是 worker 处理的构建方式，为空时处理全部。
func (s *BuildService) AcquireNextBuildRecord(ctx context.Context, workerID string, leaseDuration time.Duration, modes ...domain.BuildMode) (domain.BuildRecord, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" || leaseDuration <= 0 {
		return domain.BuildRecord{}, domain.ErrInvalidBuildInput
	}
	if len(modes) == 0 {
		modes = domain.BuildModes
	}
	for _, mode := range modes {
		if !domain.IsValidBuildMode(mode) {
			return domain.BuildRecord{}, domain.ErrInvalidBuildInput
		}
	}
	return s.repository.AcquireNextBuildRecord(ctx, workerID, leaseDuration, modes)
}

// RecordBuildCIPipeline 把 gitlab_ci 构建创建的 pipeline 写回构建记录。构建已不归 workerID 所有时返回 ErrBuildLeaseLost。
func (s *BuildService) RecordBuildCIPipeline(ctx context.Context, id string, workerID string, pipelineID int64, pipelineURL string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(workerID) == "" || pipelineID <= 0 {
		return domain.ErrInvalidBuildInput
	}
	return s.repository.RecordBuildCIPipeline(ctx, strings.TrimSpace(id), strings.TrimSpace(workerID), pipelineID, strings.TrimSpace(pipelineURL))
}

// CompleteBuildRecord 写入构建结果。构建已不归 workerID 所有时返回 ErrBuildLeaseLost。
func (s *BuildService) CompleteBuildRecord(ctx context.Context, id string, workerID string, completion domain.BuildRecordCompletion) (domain.BuildRecord, error) {
	normalizedID := strings.TrimSpace(id)
	if normalizedID == "" {
		return domain.BuildRecord{}, domain.ErrBuildRecordNotFound
	}

	normalized := domain.BuildRecordCompletion{
		CommitSHA:      strings.TrimSpace(completion.CommitSHA),
		RuntimeVersion: strings.TrimSpace(completion.RuntimeVersion),
		Platforms:      []string{},
		ArtifactKey:    strings.Trim(strings.TrimSpace(completion.ArtifactKey), "/"),
		ArtifactURL:    strings.TrimSpace(completion.ArtifactURL),
		ManifestKey:    strings.Trim(strings.TrimSpace(completion.ManifestKey), "/"),
		FileCount:      completion.FileCount,
		TotalSizeBytes: completion.TotalSizeBytes,
		ErrorMessage:   strings.TrimSpace(completion.ErrorMessage),
	}
	if normalized.ArtifactKey == "" || normalized.FileCount < 0 || normalized.TotalSizeBytes < 0 {
		return domain.BuildRecord{}, domain.ErrInvalidBuildInput
	}
	if normalized.RuntimeVersion != "" && !domain.IsValidRuntimeVersion(normalized.RuntimeVersion) {
		return domain.BuildRecord{}, domain.ErrInvalidBuildInput
	}
	for _, platform := range completion.Platforms {
		if !domain.IsValidPlatform(platform) {
			return domain.BuildRecord{}, domain.ErrInvalidBuildInput
		}
		if !slices.Contains(normalized.Platforms, platform) {
			normalized.Platforms = append(normalized.Platforms, platform)
		}
	}
	record, err := s.repository.CompleteBuildRecord(ctx, normalizedID, strings.TrimSpace(workerID), normalized)
	if err != nil {
		return domain.BuildRecord{}, err
	}
	if s.succeededHook != nil {
		s.succeededHook.HandleBuildSucceeded(ctx, record)
	}
	return record, nil
}

// FailBuildRecord 把构建标记为失败。构建已不归 workerID 所有时返回 ErrBuildLeaseLost。
func (s *BuildService) FailBuildRecord(ctx context.Context, id string, workerID string, message string) (domain.BuildRecord, error) {
	normalizedID := strings.TrimSpace(id)
	if normalizedID == "" {
		return domain.BuildRecord{}, domain.ErrBuildRecordNotFound
	}

	normalizedMessage := strings.TrimSpace(message)
	if normalizedMessage == "" {
		normalizedMessage = "build failed"
	}
	return s.repository.FailBuildRecord(ctx, normalizedID, strings.TrimSpace(workerID), normalizedMessage)
}

// RenewBuildLease 延长 workerID 持有的构建租约，构建已不归它所有时返回 ErrBuildLeaseLost。
func (s *BuildService) RenewBuildLease(ctx context.Context, id string, workerID string, leaseDuration time.Duration) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(workerID) == "" || leaseDuration <= 0 {
		return domain.ErrInvalidBuildInput
	}
	return s.repository.RenewBuildLease(ctx, strings.TrimSpace(id), strings.TrimSpace(workerID), leaseDuration)
}

// RequeueBuildRecord 把 workerID 持有、因 worker 退出而中断的构建放回队列。
func (s *BuildService) RequeueBuildRecord(ctx context.Context, id string, workerID string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(workerID) == "" {
		return domain.ErrInvalidBuildInput
	}
	return s.repository.RequeueBuildRecord(ctx, strings.TrimSpace(id), strings.TrimSpace(workerID))
}

func (s *BuildService) GetBuildQueueStats(ctx context.Context) (domain.BuildQueueStats, error) {
	return s.repository.GetBuildQueueStats(ctx)
}

func DecodeArtifactManifest(reader io.Reader) (domain.ArtifactManifest, error) {
	var manifest domain.ArtifactManifest
	if err := json.NewDecoder(reader).Decode(&manifest); err != nil {
		return domain.ArtifactManifest{}, err
	}
	return manifest, nil
}

func buildArtifactTree(manifest domain.ArtifactManifest) domain.ArtifactTree {
	root := artifactTreeBuilderNode{
		children: map[string]*artifactTreeBuilderNode{},
		node: domain.ArtifactTreeNode{
			Type: "directory",
		},
	}

	for _, file := range manifest.Files {
		relativePath := cleanArtifactRelativePath(file.Path)
		if relativePath == "" {
			continue
		}
		segments := strings.Split(relativePath, "/")
		current := &root
		for index, segment := range segments {
			if segment == "" || segment == "." {
				continue
			}
			childPath := strings.Join(segments[:index+1], "/")
			child, ok := current.children[segment]
			if !ok {
				nodeType := "directory"
				if index == len(segments)-1 {
					nodeType = "file"
				}
				child = &artifactTreeBuilderNode{
					children: map[string]*artifactTreeBuilderNode{},
					node: domain.ArtifactTreeNode{
						Name: segment,
						Path: childPath,
						Type: nodeType,
					},
				}
				current.children[segment] = child
			}
			child.node.SizeBytes += file.SizeBytes
			if index == len(segments)-1 {
				child.node.Type = "file"
				child.node.ObjectKey = file.ObjectKey
				child.node.ContentType = file.ContentType
				child.node.SHA256 = file.SHA256
			}
			current = child
		}
	}

	return domain.ArtifactTree{
		FileCount:      manifest.FileCount,
		TotalSizeBytes: manifest.TotalSizeBytes,
		Nodes:          artifactTreeNodes(root.children),
	}
}

type artifactTreeBuilderNode struct {
	children map[string]*artifactTreeBuilderNode
	node     domain.ArtifactTreeNode
}

func artifactTreeNodes(children map[string]*artifactTreeBuilderNode) []domain.ArtifactTreeNode {
	names := make([]string, 0, len(children))
	for name := range children {
		names = append(names, name)
	}
	sort.Strings(names)

	nodes := make([]domain.ArtifactTreeNode, 0, len(names))
	for _, name := range names {
		child := children[name]
		node := child.node
		if node.Type == "directory" {
			node.Children = artifactTreeNodes(child.children)
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func cleanArtifactRelativePath(value string) string {
	cleaned := path.Clean(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.HasPrefix(cleaned, "/") {
		return ""
	}
	return cleaned
}

func validateBuildRecordInput(input domain.BuildRecordInput) (domain.BuildRecordInput, error) {
	triggerType := domain.BuildTriggerType(strings.TrimSpace(string(input.TriggerType)))
	if triggerType == "" {
		triggerType = domain.BuildTriggerTypeManual
	}

	normalized := domain.BuildRecordInput{
		ModuleID:        strings.TrimSpace(input.ModuleID),
		Environment:     strings.TrimSpace(input.Environment),
		Branch:          strings.TrimSpace(input.Branch),
		CommitSHA:       strings.TrimSpace(input.CommitSHA),
		TriggerID:       strings.TrimSpace(input.TriggerID),
		TriggerType:     triggerType,
		CreatedByUserID: strings.TrimSpace(input.CreatedByUserID),
	}
	if !domain.IsValidModuleID(normalized.ModuleID) || normalized.Environment == "" {
		return domain.BuildRecordInput{}, domain.ErrInvalidBuildInput
	}
	if !domain.IsValidBuildTriggerType(normalized.TriggerType) {
		return domain.BuildRecordInput{}, domain.ErrInvalidBuildInput
	}
	// webhook 构建必须构建推送时的 commit；手动构建在 worker 检出后回填 commit_sha。
	if normalized.TriggerType == domain.BuildTriggerTypeWebhook && (normalized.CommitSHA == "" || normalized.Branch == "") {
		return domain.BuildRecordInput{}, domain.ErrInvalidBuildInput
	}
	return normalized, nil
}

func normalizeBuildRecordFilters(filters domain.BuildRecordFilters) (domain.BuildRecordFilters, error) {
	normalized := domain.BuildRecordFilters{
		ApplicationID: strings.TrimSpace(filters.ApplicationID),
		Environment:   strings.TrimSpace(filters.Environment),
		ModuleID:      strings.TrimSpace(filters.ModuleID),
		Pagination:    filters.Pagination,
		Status:        domain.BuildStatus(strings.TrimSpace(string(filters.Status))),
		TriggerType:   domain.BuildTriggerType(strings.TrimSpace(string(filters.TriggerType))),
	}
	if !normalized.Pagination.IsValid() || normalized.ModuleID != "" && !domain.IsValidModuleID(normalized.ModuleID) {
		return domain.BuildRecordFilters{}, domain.ErrInvalidBuildInput
	}
	if normalized.Status != "" && !domain.IsValidBuildStatus(normalized.Status) {
		return domain.BuildRecordFilters{}, domain.ErrInvalidBuildInput
	}
	if normalized.TriggerType != "" && !domain.IsValidBuildTriggerType(normalized.TriggerType) {
		return domain.BuildRecordFilters{}, domain.ErrInvalidBuildInput
	}
	return normalized, nil
}
