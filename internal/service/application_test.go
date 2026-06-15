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

func TestApplicationServiceRejectsBlankEnvironment(t *testing.T) {
	repository := fakeApplicationRepository{}
	service := NewApplicationService(&repository)

	input := validApplicationInput("mi-car-global")
	input.Environments[0].Environment = " "

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

func TestApplicationServiceDoesNotCreateImplicitOwnerMember(t *testing.T) {
	repository := fakeApplicationRepository{}
	service := NewApplicationService(&repository)

	input := validApplicationInput("mi-car-global")
	input.Members = nil

	_, err := service.CreateApplication(context.Background(), input)
	if err != nil {
		t.Fatalf("create application: %v", err)
	}

	if len(repository.lastCreateInput.Members) != 0 {
		t.Fatalf("expected no implicit members, got %#v", repository.lastCreateInput.Members)
	}
	if repository.lastCreateInput.OwnerUserID != "ou-li-ming" || repository.lastCreateInput.OwnerEmail == "" || repository.lastCreateInput.OwnerAvatarURL == "" {
		t.Fatalf("expected owner profile, got %#v", repository.lastCreateInput)
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

	_, summary, err := service.ListApplications(context.Background(), domain.ApplicationListFilters{})
	if err != nil {
		t.Fatalf("list applications: %v", err)
	}

	if summary.Total != 3 || summary.Running != 1 || summary.Maintenance != 1 || summary.Offline != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestApplicationServiceForwardsNormalizedListFilters(t *testing.T) {
	repository := fakeApplicationRepository{
		applications: []domain.Application{
			testApplication("mi-car", domain.ApplicationStatusRunning),
		},
	}
	service := NewApplicationService(&repository)

	_, _, err := service.ListApplications(context.Background(), domain.ApplicationListFilters{
		Platform: " iOS ",
		Status:   domain.ApplicationStatusRunning,
		Owner:    " ou-li-ming ",
		Keyword:  " 小米 ",
	})
	if err != nil {
		t.Fatalf("list applications: %v", err)
	}

	expected := domain.ApplicationListFilters{
		Platform: "iOS",
		Status:   domain.ApplicationStatusRunning,
		Owner:    "ou-li-ming",
		Keyword:  "小米",
	}
	if repository.lastListFilters != expected {
		t.Fatalf("unexpected filters: %#v", repository.lastListFilters)
	}
}

func TestApplicationServiceRejectsInvalidListStatus(t *testing.T) {
	repository := fakeApplicationRepository{}
	service := NewApplicationService(&repository)

	_, _, err := service.ListApplications(context.Background(), domain.ApplicationListFilters{
		Status: domain.ApplicationStatus("unknown"),
	})
	if !errors.Is(err, domain.ErrInvalidApplicationInput) {
		t.Fatalf("expected invalid application input, got %v", err)
	}
	if repository.listCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.listCalls)
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
	if len(options.Owners) != 1 || options.Owners[0].Value != "ou-li-ming" {
		t.Fatalf("expected owner options, got %#v", options.Owners)
	}
	if len(options.Environments) != 1 || options.Environments[0].Value != "qa" {
		t.Fatalf("expected dynamic environment options, got %#v", options.Environments)
	}
	removedPlatform := "Android" + " TV"
	for _, platform := range options.Platforms {
		if platform.Value == removedPlatform {
			t.Fatalf("did not expect removed platform option")
		}
	}
}

func TestApplicationServiceUpdateStatusThroughRepository(t *testing.T) {
	repository := fakeApplicationRepository{
		detail: testApplicationDetail("mi-car", domain.ApplicationStatusRunning),
	}
	service := NewApplicationService(&repository)

	detail, err := service.UpdateApplicationStatus(context.Background(), "mi-car", domain.ApplicationStatusRunning)
	if err != nil {
		t.Fatalf("update application status: %v", err)
	}

	if detail.Application.Status != domain.ApplicationStatusRunning {
		t.Fatalf("expected running status, got %q", detail.Application.Status)
	}
	if repository.updateStatusCalls != 1 {
		t.Fatalf("expected repository to be called once, got %d calls", repository.updateStatusCalls)
	}
}

func TestApplicationServiceRejectsInvalidStatusUpdate(t *testing.T) {
	repository := fakeApplicationRepository{}
	service := NewApplicationService(&repository)

	_, err := service.UpdateApplicationStatus(context.Background(), "mi-car", domain.ApplicationStatus("unknown"))
	if !errors.Is(err, domain.ErrInvalidApplicationInput) {
		t.Fatalf("expected invalid application input, got %v", err)
	}
	if repository.updateStatusCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repository.updateStatusCalls)
	}
}

func TestApplicationServiceReturnsDeleteCheck(t *testing.T) {
	repository := fakeApplicationRepository{
		deleteCheck: domain.ApplicationDeleteCheck{
			Deletable: true,
			Message:   "ok",
		},
	}
	service := NewApplicationService(&repository)

	check, err := service.CheckApplicationDelete(context.Background(), "mi-car")
	if err != nil {
		t.Fatalf("check application delete: %v", err)
	}
	if !check.Deletable || repository.deleteCheckCalls != 1 {
		t.Fatalf("expected deletable check, got check=%+v calls=%d", check, repository.deleteCheckCalls)
	}
}

func TestApplicationServiceRejectsDeleteWhenDependenciesExist(t *testing.T) {
	repository := fakeApplicationRepository{
		deleteCheck: domain.ApplicationDeleteCheck{
			Deletable: false,
			Dependencies: []domain.ApplicationDeleteDependency{
				{ID: "app-mi-car:modules", Name: "接入模块", Type: "module"},
			},
		},
	}
	service := NewApplicationService(&repository)

	err := service.DeleteApplication(context.Background(), "mi-car")
	if !errors.Is(err, domain.ErrApplicationInUse) {
		t.Fatalf("expected application in use, got %v", err)
	}
	if repository.deleteCalls != 0 {
		t.Fatalf("expected delete not to be called, got %d calls", repository.deleteCalls)
	}
}

func TestApplicationServiceDeletesAfterDependencyCheck(t *testing.T) {
	repository := fakeApplicationRepository{
		deleteCheck: domain.ApplicationDeleteCheck{
			Deletable: true,
		},
	}
	service := NewApplicationService(&repository)

	if err := service.DeleteApplication(context.Background(), "mi-car"); err != nil {
		t.Fatalf("delete application: %v", err)
	}
	if repository.deleteCheckCalls != 1 || repository.deleteCalls != 1 {
		t.Fatalf("expected check and delete calls, got check=%d delete=%d", repository.deleteCheckCalls, repository.deleteCalls)
	}
}

type fakeApplicationRepository struct {
	applications      []domain.Application
	deleteCheck       domain.ApplicationDeleteCheck
	detail            domain.ApplicationDetail
	lastCreateInput   domain.ApplicationInput
	lastListFilters   domain.ApplicationListFilters
	listCalls         int
	createCalls       int
	updateCalls       int
	updateStatusCalls int
	deleteCheckCalls  int
	deleteCalls       int
}

func (r *fakeApplicationRepository) ListApplications(_ context.Context, filters domain.ApplicationListFilters) ([]domain.Application, error) {
	r.listCalls++
	r.lastListFilters = filters
	return r.applications, nil
}

func (r *fakeApplicationRepository) GetApplicationDetail(context.Context, string) (domain.ApplicationDetail, error) {
	return r.detail, nil
}

func (r *fakeApplicationRepository) ListApplicationOwners(context.Context) ([]domain.ApplicationOptionItem, error) {
	return []domain.ApplicationOptionItem{{Label: "li.ming", Value: "ou-li-ming"}}, nil
}

func (r *fakeApplicationRepository) ListApplicationEnvironments(context.Context) ([]domain.ApplicationOptionItem, error) {
	return []domain.ApplicationOptionItem{{Label: "QA 环境", Value: "qa"}}, nil
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

func (r *fakeApplicationRepository) UpdateApplicationStatus(context.Context, string, domain.ApplicationStatus) (domain.ApplicationDetail, error) {
	r.updateStatusCalls++
	return r.detail, nil
}

func (r *fakeApplicationRepository) CheckApplicationDelete(context.Context, string) (domain.ApplicationDeleteCheck, error) {
	r.deleteCheckCalls++
	return r.deleteCheck, nil
}

func (r *fakeApplicationRepository) DeleteApplication(context.Context, string) error {
	r.deleteCalls++
	return nil
}

func validApplicationInput(slug string) domain.ApplicationInput {
	return domain.ApplicationInput{
		Slug:               slug,
		Name:               "小米汽车海外版",
		Platform:           "iOS / Android",
		OwnerUserID:        "ou-li-ming",
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
		OwnerUserID:        "ou-li-ming",
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
