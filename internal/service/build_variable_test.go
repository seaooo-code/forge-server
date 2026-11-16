package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"forge-server/internal/domain"
)

func TestResolveBuildVariablesPrefersEnvironmentValue(t *testing.T) {
	repository := &fakeBuildVariableRepository{applicable: []domain.ModuleBuildVariable{
		{Key: "API_HOST", ValueEncrypted: "box:common.example.com"},
		{Key: "API_HOST", ValueEncrypted: "box:qa.example.com", EnvironmentCode: "qa"},
		{Key: "NPM_TOKEN", ValueEncrypted: "box:qa-token", IsSecret: true, EnvironmentCode: "qa"},
		{Key: "NPM_TOKEN", ValueEncrypted: "box:common-token", IsSecret: false},
		{Key: "SENTRY_DSN", ValueEncrypted: "box:dsn", IsSecret: true},
		{Key: "OTHER_ENV", ValueEncrypted: "box:ignored", EnvironmentCode: "prod"},
	}}
	service := NewBuildVariableService(repository, testBuildVariableSecretBox{})

	got, err := service.ResolveBuildVariables(context.Background(), "module-1", "qa")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 variables, got %+v", got)
	}
	want := []struct {
		key    string
		value  string
		secret bool
	}{
		{key: "API_HOST", value: "qa.example.com"},
		{key: "NPM_TOKEN", value: "qa-token", secret: true},
		{key: "SENTRY_DSN", value: "dsn", secret: true},
	}
	for index, item := range want {
		if got[index].Key != item.key || got[index].Value != item.value || got[index].IsSecret != item.secret || got[index].ValueEncrypted != "" {
			t.Fatalf("unexpected variable %d: %+v", index, got[index])
		}
	}
}

func TestListBuildVariablesMasksSecrets(t *testing.T) {
	repository := &fakeBuildVariableRepository{all: []domain.ModuleBuildVariable{
		{Key: "API_HOST", ValueEncrypted: "box:qa.example.com", UpdatedBy: "海鸥"},
		{Key: "NPM_TOKEN", ValueEncrypted: "box:token-value", IsSecret: true, UpdatedBy: "海鸥"},
	}}
	service := NewBuildVariableService(repository, testBuildVariableSecretBox{})

	got, err := service.ListBuildVariables(context.Background(), "module-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got[0].Value != "qa.example.com" || got[1].Value != BuildVariableSecretMask || got[1].UpdatedBy != "海鸥" {
		t.Fatalf("unexpected list: %+v", got)
	}
	for _, variable := range got {
		if variable.ValueEncrypted != "" || strings.Contains(variable.Value, "token-value") {
			t.Fatalf("secret leaked: %+v", variable)
		}
	}
}

func TestUpsertBuildVariableEncryptsAndMasks(t *testing.T) {
	repository := &fakeBuildVariableRepository{}
	service := NewBuildVariableService(repository, testBuildVariableSecretBox{})

	got, err := service.UpsertBuildVariable(context.Background(), domain.ModuleBuildVariableInput{
		ModuleID:        " module-1 ",
		Key:             "NPM_TOKEN",
		Value:           "token-value",
		IsSecret:        true,
		EnvironmentCode: " qa ",
		UpdatedByUserID: "ou-user-1",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if repository.saved.ValueEncrypted != "box:token-value" || repository.saved.ModuleID != "module-1" || repository.saved.EnvironmentCode != "qa" {
		t.Fatalf("expected ciphertext to be stored, got %+v", repository.saved)
	}
	if got.Value != BuildVariableSecretMask || got.ValueEncrypted != "" {
		t.Fatalf("expected masked response, got %+v", got)
	}
}

func TestUpsertBuildVariableRejectsInvalidInput(t *testing.T) {
	service := NewBuildVariableService(&fakeBuildVariableRepository{}, testBuildVariableSecretBox{})
	base := domain.ModuleBuildVariableInput{ModuleID: "module-1", Key: "API_HOST", Value: "x", UpdatedByUserID: "ou-user-1"}

	tests := map[string]func(input *domain.ModuleBuildVariableInput){
		"lowercase key":   func(input *domain.ModuleBuildVariableInput) { input.Key = "api_host" },
		"reserved prefix": func(input *domain.ModuleBuildVariableInput) { input.Key = "FORGE_BRANCH" },
		"empty secret":    func(input *domain.ModuleBuildVariableInput) { input.IsSecret = true; input.Value = "" },
		"missing user":    func(input *domain.ModuleBuildVariableInput) { input.UpdatedByUserID = "" },
		"too long": func(input *domain.ModuleBuildVariableInput) {
			input.Value = strings.Repeat("a", maxBuildVariableValueLength+1)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := base
			mutate(&input)
			if _, err := service.UpsertBuildVariable(context.Background(), input); !errors.Is(err, domain.ErrInvalidBuildVariableInput) {
				t.Fatalf("expected invalid input, got %v", err)
			}
		})
	}
}

type fakeBuildVariableRepository struct {
	all        []domain.ModuleBuildVariable
	applicable []domain.ModuleBuildVariable
	saved      domain.ModuleBuildVariable
}

func (r *fakeBuildVariableRepository) DeleteModuleBuildVariable(context.Context, string, string, string) error {
	return nil
}

func (r *fakeBuildVariableRepository) ListModuleBuildVariables(context.Context, string) ([]domain.ModuleBuildVariable, error) {
	return append([]domain.ModuleBuildVariable(nil), r.all...), nil
}

func (r *fakeBuildVariableRepository) ListApplicableBuildVariables(context.Context, string, string) ([]domain.ModuleBuildVariable, error) {
	return append([]domain.ModuleBuildVariable(nil), r.applicable...), nil
}

func (r *fakeBuildVariableRepository) UpsertModuleBuildVariable(_ context.Context, variable domain.ModuleBuildVariable) (domain.ModuleBuildVariable, error) {
	r.saved = variable
	return variable, nil
}

type testBuildVariableSecretBox struct{}

func (testBuildVariableSecretBox) Encrypt(plaintext string) (string, error) {
	return "box:" + plaintext, nil
}

func (testBuildVariableSecretBox) Decrypt(ciphertext string) (string, error) {
	plaintext, ok := strings.CutPrefix(ciphertext, "box:")
	if !ok {
		return "", errors.New("invalid ciphertext")
	}
	return plaintext, nil
}
