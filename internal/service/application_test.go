package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"forge-server/internal/domain"
)

func TestApplicationServiceRejectsInvalidSlugOnCreate(t *testing.T) {
	repository := fakeApplicationRepository{}
	service := NewApplicationService(&repository)

	_, err := service.CreateApplication(context.Background(), validApplicationInput("Bad Slug"))
	if !errors.Is(err, domain.ErrInvalidApplicationInput) {
		t.Fatalf("expected invalid application input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.createCalls)
	}
}

func TestApplicationServiceRejectsUnknownEnvironment(t *testing.T) {
	repository := fakeApplicationRepository{}
	service := NewApplicationService(&repository)

	input := validApplicationInput("mi-car-global")
	input.Environments[0].Environment = "unknown"

	_, err := service.CreateApplication(context.Background(), input)
	if !errors.Is(err, domain.ErrInvalidApplicationInput) {
		t.Fatalf("expected invalid application input, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.createCalls)
	}
}

func TestApplicationServiceRejectsSlugChangeOnUpdate(t *testing.T) {
	repository := fakeApplicationRepository{}
	service := NewApplicationService(&repository)

	input := validApplicationInput("")
	input.Slug = "changed"

	_, err := service.UpdateApplication(context.Background(), "mi-car", input)
	if !errors.Is(err, domain.ErrInvalidApplicationInput) {
		t.Fatalf("expected invalid application input, got %v", err)
	}
	if repository.updateCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.updateCalls)
	}
}

func TestApplicationServiceAddsOwnerToMembers(t *testing.T) {
	repository := fakeApplicationRepository{}
	service := NewApplicationService(&repository)

	input := validApplicationInput("mi-car-global")
	input.Members = nil

	_, err := service.CreateApplication(context.Background(), input)
	if err != nil {
		t.Fatalf("create application: %v", err)
	}

	if len(repository.lastCreateInput.Members) != 1 {
		t.Fatalf("expected owner member, got %#v", repository.lastCreateInput.Members)
	}
	member := repository.lastCreateInput.Members[0]
	if member.Name != "li.ming" || member.Role != domain.ApplicationMemberRoleOwner {
		t.Fatalf("unexpected owner member: %#v", member)
	}
}

func TestApplicationServiceReturnsSummary(t *testing.T) {
	repository := fakeApplicationRepository{
		applications: []domain.Application{
			testApplication("mi-car", domain.ApplicationStatusRunning),
			testApplication("mi-pay", domain.ApplicationStatusMaintenance),
			testApplication("mi-tv", domain.ApplicationStatusOffline),
		},
	}
	service := NewApplicationService(&repository)

	_, summary, err := service.ListApplications(context.Background())
	if err != nil {
		t.Fatalf("list applications: %v", err)
	}

	if summary.Total != 3 || summary.Running != 1 || summary.Maintenance != 1 || summary.Offline != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestApplicationServiceReturnsApplicationOptions(t *testing.T) {
	service := NewApplicationService(&fakeApplicationRepository{})

	options, err := service.GetApplicationOptions(context.Background())
	if err != nil {
		t.Fatalf("get application options: %v", err)
	}

	if len(options.Platforms) != 3 {
		t.Fatalf("expected 3 platforms, got %d", len(options.Platforms))
	}
	if len(options.Owners) != 1 || options.Owners[0].Value != "li.ming" {
		t.Fatalf("expected owner options, got %#v", options.Owners)
	}
	removedPlatform := "Android" + " TV"
	for _, platform := range options.Platforms {
		if platform.Value == removedPlatform {
			t.Fatalf("did not expect removed platform option")
		}
	}
}

func TestApplicationServiceDeleteMarksOfflineThroughRepository(t *testing.T) {
	repository := fakeApplicationRepository{
		detail: testApplicationDetail("mi-car", domain.ApplicationStatusOffline),
	}
	service := NewApplicationService(&repository)

	detail, err := service.DeleteApplication(context.Background(), "mi-car")
	if err != nil {
		t.Fatalf("delete application: %v", err)
	}

	if detail.Application.Status != domain.ApplicationStatusOffline {
		t.Fatalf("expected offline status, got %q", detail.Application.Status)
	}
	if repository.deleteCalls != 1 {
		t.Fatalf("expected repository to be called once, got %d calls", repository.deleteCalls)
	}
}

type fakeApplicationRepository struct {
	applications    []domain.Application
	detail          domain.ApplicationDetail
	lastCreateInput domain.ApplicationInput
	createCalls     int
	updateCalls     int
	deleteCalls     int
}

func (r *fakeApplicationRepository) ListApplications(context.Context) ([]domain.Application, error) {
	return r.applications, nil
}

func (r *fakeApplicationRepository) GetApplicationDetail(context.Context, string) (domain.ApplicationDetail, error) {
	return r.detail, nil
}

func (r *fakeApplicationRepository) ListApplicationOwners(context.Context) ([]domain.ApplicationOptionItem, error) {
	return []domain.ApplicationOptionItem{{Label: "li.ming", Value: "li.ming"}}, nil
}

func (r *fakeApplicationRepository) CreateApplication(_ context.Context, input domain.ApplicationInput) (domain.ApplicationDetail, error) {
	r.createCalls++
	r.lastCreateInput = input
	return r.detail, nil
}

func (r *fakeApplicationRepository) UpdateApplication(context.Context, string, domain.ApplicationInput) (domain.ApplicationDetail, error) {
	r.updateCalls++
	return r.detail, nil
}

func (r *fakeApplicationRepository) DeleteApplication(context.Context, string) (domain.ApplicationDetail, error) {
	r.deleteCalls++
	return r.detail, nil
}

func validApplicationInput(slug string) domain.ApplicationInput {
	return domain.ApplicationInput{
		Slug:               slug,
		Name:               "小米汽车海外版",
		Platform:           "iOS / Android",
		Owner:              "li.ming",
		OwnerEmail:         "li.ming@xiaomi.com",
		OwnerAvatarURL:     "https://example.com/li.ming.png",
		OwnerDepartmentIDs: []string{"od-rn-platform"},
		Description:        "海外版宿主应用",
		Status:             domain.ApplicationStatusRunning,
		Environments: []domain.ApplicationEnvironmentInput{
			{
				Environment: "dev",
			},
			{
				Environment: "product",
			},
		},
		Members: []domain.ApplicationMemberInput{
			{
				Name: "li.ming",
				Role: domain.ApplicationMemberRoleOwner,
				Team: "小米汽车 - RN 平台组",
			},
		},
	}
}

func testApplication(slug string, status domain.ApplicationStatus) domain.Application {
	return domain.Application{
		ID:                 "app-" + slug,
		Name:               slug,
		Slug:               slug,
		Platform:           "iOS / Android",
		Owner:              "li.ming",
		OwnerEmail:         "li.ming@xiaomi.com",
		OwnerAvatarURL:     "https://example.com/li.ming.png",
		OwnerDepartmentIDs: []string{"od-rn-platform"},
		ModuleCount:        12,
		EnvironmentCount:   5,
		CreatedAt:          time.Date(2026, 6, 9, 14, 32, 18, 0, time.UTC),
		Status:             status,
	}
}

func testApplicationDetail(slug string, status domain.ApplicationStatus) domain.ApplicationDetail {
	return domain.ApplicationDetail{
		Application:        testApplication(slug, status),
		Description:        "应用详情",
		OwnerDepartmentIDs: []string{"od-rn-platform"},
		Environments: []domain.ApplicationEnvironmentBinding{
			{
				ID:          "dev",
				Environment: "dev",
				Status:      domain.ApplicationEnvironmentStatusHealthy,
			},
		},
	}
}
