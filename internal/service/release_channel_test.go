package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"forge-server/internal/domain"
)

type fakeReleaseChannelRepository struct {
	created domain.ReleaseChannelInput
	updated domain.ReleaseChannelInput
}

func (r *fakeReleaseChannelRepository) ListReleaseChannels(context.Context, string) ([]domain.ReleaseChannel, error) {
	return nil, nil
}

func (r *fakeReleaseChannelRepository) CreateReleaseChannel(_ context.Context, _ string, input domain.ReleaseChannelInput) (domain.ReleaseChannel, error) {
	r.created = input
	return domain.ReleaseChannel{Code: input.Code, Name: input.Name}, nil
}

func (r *fakeReleaseChannelRepository) UpdateReleaseChannel(_ context.Context, _ string, code string, input domain.ReleaseChannelInput) (domain.ReleaseChannel, error) {
	r.updated = input
	return domain.ReleaseChannel{Code: code, Name: input.Name}, nil
}

func (r *fakeReleaseChannelRepository) DeleteReleaseChannel(context.Context, string, string) error {
	return nil
}

func TestReleaseChannelServiceValidatesInput(t *testing.T) {
	repository := &fakeReleaseChannelRepository{}
	service := NewReleaseChannelService(repository)
	ctx := context.Background()

	if _, err := service.CreateReleaseChannel(ctx, "mi-car", domain.ReleaseChannelInput{Code: " beta ", Name: " 内测 ", Description: " 内测 App ", CreatedByUserID: " ou-1 "}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if repository.created != (domain.ReleaseChannelInput{Code: "beta", Name: "内测", Description: "内测 App", CreatedByUserID: "ou-1"}) {
		t.Fatalf("unexpected normalized input: %+v", repository.created)
	}

	for _, input := range []domain.ReleaseChannelInput{
		{Code: "", Name: "x"},
		{Code: "Beta", Name: "x"},
		{Code: "beta", Name: " "},
		{Code: "beta", Name: strings.Repeat("名", domain.MaxReleaseChannelNameLength+1)},
		{Code: "beta", Name: "x", Description: strings.Repeat("说", domain.MaxReleaseChannelDescriptionLength+1)},
	} {
		if _, err := service.CreateReleaseChannel(ctx, "mi-car", input); !errors.Is(err, domain.ErrInvalidReleaseChannelInput) {
			t.Fatalf("expected invalid input for %+v, got %v", input, err)
		}
	}
	if _, err := service.UpdateReleaseChannel(ctx, "mi-car", "Bad", domain.ReleaseChannelInput{Name: "x"}); !errors.Is(err, domain.ErrInvalidReleaseChannelInput) {
		t.Fatalf("expected invalid code on update, got %v", err)
	}
	if err := service.DeleteReleaseChannel(ctx, "mi-car", ""); !errors.Is(err, domain.ErrInvalidReleaseChannelInput) {
		t.Fatalf("expected invalid code on delete, got %v", err)
	}
	if _, err := service.UpdateReleaseChannel(ctx, "mi-car", "beta", domain.ReleaseChannelInput{Code: "ignored", Name: "新名"}); err != nil || repository.updated.Code != "" {
		t.Fatalf("update should ignore code, got %+v %v", repository.updated, err)
	}
}
