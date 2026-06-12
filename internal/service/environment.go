package service

import (
	"context"

	"forge-server/internal/domain"
)

type EnvironmentRepository interface {
	GetEnvironmentDetail(ctx context.Context, code string) (domain.Environment, error)
	ListEnvironments(ctx context.Context) ([]domain.Environment, error)
}

type EnvironmentService struct {
	repository EnvironmentRepository
}

func NewEnvironmentService(repository EnvironmentRepository) *EnvironmentService {
	return &EnvironmentService{repository: repository}
}

func (s *EnvironmentService) ListEnvironments(ctx context.Context) ([]domain.Environment, error) {
	return s.repository.ListEnvironments(ctx)
}

func (s *EnvironmentService) GetEnvironmentDetail(ctx context.Context, code string) (domain.Environment, error) {
	if !domain.IsValidEnvironmentCode(code) {
		return domain.Environment{}, domain.ErrEnvironmentNotFound
	}

	return s.repository.GetEnvironmentDetail(ctx, code)
}
