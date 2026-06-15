package service

import (
	"context"
	"errors"
	"testing"

	"forge-server/internal/domain"
)

func TestEnvironmentServiceRejectsBlankEnvironmentCode(t *testing.T) {
	repository := fakeEnvironmentRepository{}
	service := NewEnvironmentService(&repository)

	_, err := service.GetEnvironmentDetail(context.Background(), " ")
	if !errors.Is(err, domain.ErrEnvironmentNotFound) {
		t.Fatalf("expected environment not found, got %v", err)
	}

	if repository.detailCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.detailCalls)
	}
}

func TestEnvironmentServiceCreatesEnvironment(t *testing.T) {
	repository := fakeEnvironmentRepository{
		created: domain.Environment{Code: "qa"},
	}
	service := NewEnvironmentService(&repository)

	_, err := service.CreateEnvironment(context.Background(), domain.EnvironmentInput{
		Code:                   " qa ",
		Name:                   " QA 环境 ",
		Status:                 domain.EnvironmentStatusHealthy,
		Tone:                   "blue",
		StorageConfigurationID: " storage-qa ",
	})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}

	if repository.lastCreateInput.Code != "qa" {
		t.Fatalf("expected trimmed code, got %q", repository.lastCreateInput.Code)
	}
	if repository.lastCreateInput.Status != domain.EnvironmentStatusHealthy {
		t.Fatalf("expected normalized status, got %q", repository.lastCreateInput.Status)
	}
	if repository.lastCreateInput.StorageConfigurationID != "storage-qa" {
		t.Fatalf("expected storage id, got %q", repository.lastCreateInput.StorageConfigurationID)
	}
}

func TestEnvironmentServiceRequiresStorageConfiguration(t *testing.T) {
	repository := fakeEnvironmentRepository{}
	service := NewEnvironmentService(&repository)

	_, err := service.CreateEnvironment(context.Background(), domain.EnvironmentInput{
		Code:   "qa",
		Name:   "QA 环境",
		Status: domain.EnvironmentStatusHealthy,
		Tone:   "blue",
	})
	if !errors.Is(err, domain.ErrInvalidEnvironmentInput) {
		t.Fatalf("expected invalid environment input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.createCalls)
	}
}

func TestEnvironmentServiceUpdatesEnvironment(t *testing.T) {
	repository := fakeEnvironmentRepository{
		updated: domain.Environment{Code: "qa"},
	}
	service := NewEnvironmentService(&repository)

	_, err := service.UpdateEnvironment(context.Background(), " qa ", domain.EnvironmentInput{
		Code:                   "qa",
		Name:                   " QA 环境 ",
		Status:                 domain.EnvironmentStatusHealthy,
		Tone:                   "sky",
		StorageConfigurationID: " storage-qa ",
	})
	if err != nil {
		t.Fatalf("update environment: %v", err)
	}

	if repository.lastUpdateCode != "qa" {
		t.Fatalf("expected trimmed path code, got %q", repository.lastUpdateCode)
	}
	if repository.lastUpdateInput.Status != domain.EnvironmentStatusHealthy {
		t.Fatalf("expected normalized status, got %q", repository.lastUpdateInput.Status)
	}
	if repository.lastUpdateInput.StorageConfigurationID != "storage-qa" {
		t.Fatalf("expected storage id, got %q", repository.lastUpdateInput.StorageConfigurationID)
	}
}

func TestEnvironmentServiceRejectsEnvironmentCodeChange(t *testing.T) {
	repository := fakeEnvironmentRepository{}
	service := NewEnvironmentService(&repository)

	_, err := service.UpdateEnvironment(context.Background(), "qa", domain.EnvironmentInput{
		Code:                   "prod",
		Name:                   "生产环境",
		Status:                 domain.EnvironmentStatusHealthy,
		Tone:                   "blue",
		StorageConfigurationID: "storage-prod",
	})
	if !errors.Is(err, domain.ErrInvalidEnvironmentInput) {
		t.Fatalf("expected invalid environment input, got %v", err)
	}
	if repository.updateCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.updateCalls)
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
	created         domain.Environment
	detail          domain.Environment
	updated         domain.Environment
	lastCreateInput domain.EnvironmentInput
	lastUpdateCode  string
	lastUpdateInput domain.EnvironmentInput
	createCalls     int
	detailCalls     int
	updateCalls     int
}

func (r *fakeEnvironmentRepository) ListEnvironments(context.Context) ([]domain.Environment, error) {
	return nil, nil
}

func (r *fakeEnvironmentRepository) CreateEnvironment(_ context.Context, input domain.EnvironmentInput) (domain.Environment, error) {
	r.createCalls++
	r.lastCreateInput = input
	return r.created, nil
}

func (r *fakeEnvironmentRepository) GetEnvironmentDetail(context.Context, string) (domain.Environment, error) {
	r.detailCalls++
	return r.detail, nil
}

func (r *fakeEnvironmentRepository) UpdateEnvironment(_ context.Context, code string, input domain.EnvironmentInput) (domain.Environment, error) {
	r.updateCalls++
	r.lastUpdateCode = code
	r.lastUpdateInput = input
	return r.updated, nil
}
