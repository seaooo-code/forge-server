package service

import (
	"context"
	"errors"
	"testing"

	"forge-server/internal/domain"
)

func TestEnvironmentServiceRejectsUnknownEnvironmentCode(t *testing.T) {
	repository := fakeEnvironmentRepository{}
	service := NewEnvironmentService(&repository)

	_, err := service.GetEnvironmentDetail(context.Background(), "unknown")
	if !errors.Is(err, domain.ErrEnvironmentNotFound) {
		t.Fatalf("expected environment not found, got %v", err)
	}

	if repository.detailCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.detailCalls)
	}
}

func TestEnvironmentServiceReturnsEnvironmentDetail(t *testing.T) {
	repository := fakeEnvironmentRepository{
		detail: domain.Environment{
			Code: "dev",
		},
	}
	service := NewEnvironmentService(&repository)

	detail, err := service.GetEnvironmentDetail(context.Background(), "dev")
	if err != nil {
		t.Fatalf("get environment detail: %v", err)
	}

	if detail.Code != "dev" {
		t.Fatalf("expected dev environment, got %q", detail.Code)
	}
	if repository.detailCalls != 1 {
		t.Fatalf("expected repository to be called once, got %d", repository.detailCalls)
	}
}

type fakeEnvironmentRepository struct {
	detail      domain.Environment
	detailCalls int
}

func (r *fakeEnvironmentRepository) ListEnvironments(context.Context) ([]domain.Environment, error) {
	return nil, nil
}

func (r *fakeEnvironmentRepository) GetEnvironmentDetail(context.Context, string) (domain.Environment, error) {
	r.detailCalls++
	return r.detail, nil
}
