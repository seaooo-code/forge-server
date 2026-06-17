package service

import (
	"context"
	"strings"
	"time"

	"forge-server/internal/domain"
)

type BuildRecordRepository interface {
	AcquireNextBuildRecord(ctx context.Context, workerID string, leaseDuration time.Duration) (domain.BuildRecord, error)
	CompleteBuildRecord(ctx context.Context, id string, completion domain.BuildRecordCompletion) (domain.BuildRecord, error)
	CreateBuildRecord(ctx context.Context, input domain.BuildRecordInput) (domain.BuildRecord, error)
	FailBuildRecord(ctx context.Context, id string, message string) (domain.BuildRecord, error)
	GetBuildRecord(ctx context.Context, id string) (domain.BuildRecord, error)
	ListBuildRecords(ctx context.Context) ([]domain.BuildRecord, domain.BuildRecordSummary, error)
}

type BuildService struct {
	repository BuildRecordRepository
}

func NewBuildService(repository BuildRecordRepository) *BuildService {
	return &BuildService{repository: repository}
}

func (s *BuildService) CreateBuildRecord(ctx context.Context, input domain.BuildRecordInput) (domain.BuildRecord, error) {
	normalized, err := validateBuildRecordInput(input)
	if err != nil {
		return domain.BuildRecord{}, err
	}
	return s.repository.CreateBuildRecord(ctx, normalized)
}

func (s *BuildService) ListBuildRecords(ctx context.Context) ([]domain.BuildRecord, domain.BuildRecordSummary, error) {
	return s.repository.ListBuildRecords(ctx)
}

func (s *BuildService) GetBuildRecord(ctx context.Context, id string) (domain.BuildRecord, error) {
	normalizedID := strings.TrimSpace(id)
	if normalizedID == "" {
		return domain.BuildRecord{}, domain.ErrBuildRecordNotFound
	}
	return s.repository.GetBuildRecord(ctx, normalizedID)
}

func (s *BuildService) AcquireNextBuildRecord(ctx context.Context, workerID string, leaseDuration time.Duration) (domain.BuildRecord, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" || leaseDuration <= 0 {
		return domain.BuildRecord{}, domain.ErrInvalidBuildInput
	}
	return s.repository.AcquireNextBuildRecord(ctx, workerID, leaseDuration)
}

func (s *BuildService) CompleteBuildRecord(ctx context.Context, id string, completion domain.BuildRecordCompletion) (domain.BuildRecord, error) {
	normalizedID := strings.TrimSpace(id)
	if normalizedID == "" {
		return domain.BuildRecord{}, domain.ErrBuildRecordNotFound
	}

	normalized := domain.BuildRecordCompletion{
		ArtifactKey:  strings.Trim(strings.TrimSpace(completion.ArtifactKey), "/"),
		ArtifactURL:  strings.TrimSpace(completion.ArtifactURL),
		ErrorMessage: strings.TrimSpace(completion.ErrorMessage),
	}
	if normalized.ArtifactKey == "" {
		return domain.BuildRecord{}, domain.ErrInvalidBuildInput
	}
	return s.repository.CompleteBuildRecord(ctx, normalizedID, normalized)
}

func (s *BuildService) FailBuildRecord(ctx context.Context, id string, message string) (domain.BuildRecord, error) {
	normalizedID := strings.TrimSpace(id)
	if normalizedID == "" {
		return domain.BuildRecord{}, domain.ErrBuildRecordNotFound
	}

	normalizedMessage := strings.TrimSpace(message)
	if normalizedMessage == "" {
		normalizedMessage = "build failed"
	}
	return s.repository.FailBuildRecord(ctx, normalizedID, normalizedMessage)
}

func validateBuildRecordInput(input domain.BuildRecordInput) (domain.BuildRecordInput, error) {
	triggerType := domain.BuildTriggerType(strings.TrimSpace(string(input.TriggerType)))
	if triggerType == "" {
		triggerType = domain.BuildTriggerTypeManual
	}

	normalized := domain.BuildRecordInput{
		ModuleID:    strings.TrimSpace(input.ModuleID),
		Environment: strings.TrimSpace(input.Environment),
		Branch:      strings.TrimSpace(input.Branch),
		TriggerID:   strings.TrimSpace(input.TriggerID),
		TriggerType: triggerType,
	}
	if !domain.IsValidModuleID(normalized.ModuleID) || normalized.Environment == "" {
		return domain.BuildRecordInput{}, domain.ErrInvalidBuildInput
	}
	if !domain.IsValidBuildTriggerType(normalized.TriggerType) {
		return domain.BuildRecordInput{}, domain.ErrInvalidBuildInput
	}
	if normalized.TriggerType == domain.BuildTriggerTypePipeline && normalized.TriggerID == "" {
		return domain.BuildRecordInput{}, domain.ErrInvalidBuildInput
	}
	return normalized, nil
}
