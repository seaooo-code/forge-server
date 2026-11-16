package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"forge-server/internal/domain"
)

// BuildVariableSecretMask 是接口返回密钥类构建变量时使用的占位值，明文只在 worker 构建时解密。
const BuildVariableSecretMask = "******"

const (
	maxBuildVariableValueLength = 32 * 1024
	// reservedBuildVariablePrefix 留给平台固定注入的构建变量（FORGE_ENVIRONMENT 等），用户变量不能占用。
	reservedBuildVariablePrefix = "FORGE_"
)

type BuildVariableRepository interface {
	DeleteModuleBuildVariable(ctx context.Context, moduleID string, key string, environmentCode string) error
	// ListModuleBuildVariables 返回模块的全部变量，模块不存在时返回 domain.ErrModuleNotFound。
	ListModuleBuildVariables(ctx context.Context, moduleID string) ([]domain.ModuleBuildVariable, error)
	// ListApplicableBuildVariables 返回通用变量和指定环境的专属变量，覆盖规则由 service 处理。
	ListApplicableBuildVariables(ctx context.Context, moduleID string, environmentCode string) ([]domain.ModuleBuildVariable, error)
	UpsertModuleBuildVariable(ctx context.Context, variable domain.ModuleBuildVariable) (domain.ModuleBuildVariable, error)
}

type BuildVariableService struct {
	repository BuildVariableRepository
	secrets    SecretBox
}

func NewBuildVariableService(repository BuildVariableRepository, secrets SecretBox) *BuildVariableService {
	return &BuildVariableService{repository: repository, secrets: secrets}
}

// ListBuildVariables 返回给控制台展示的变量列表：普通变量解密后返回原值，密钥变量只返回占位值。
func (s *BuildVariableService) ListBuildVariables(ctx context.Context, moduleID string) ([]domain.ModuleBuildVariable, error) {
	moduleID = strings.TrimSpace(moduleID)
	if moduleID == "" {
		return nil, domain.ErrModuleNotFound
	}
	variables, err := s.repository.ListModuleBuildVariables(ctx, moduleID)
	if err != nil {
		return nil, err
	}
	for index := range variables {
		if err := s.revealForConsole(&variables[index]); err != nil {
			return nil, err
		}
	}
	return variables, nil
}

func (s *BuildVariableService) UpsertBuildVariable(ctx context.Context, input domain.ModuleBuildVariableInput) (domain.ModuleBuildVariable, error) {
	normalized := domain.ModuleBuildVariableInput{
		ModuleID:        strings.TrimSpace(input.ModuleID),
		Key:             strings.TrimSpace(input.Key),
		Value:           input.Value,
		IsSecret:        input.IsSecret,
		EnvironmentCode: strings.TrimSpace(input.EnvironmentCode),
		UpdatedByUserID: strings.TrimSpace(input.UpdatedByUserID),
	}
	if normalized.ModuleID == "" {
		return domain.ModuleBuildVariable{}, domain.ErrModuleNotFound
	}
	if !domain.IsValidBuildVariableKey(normalized.Key) || strings.HasPrefix(normalized.Key, reservedBuildVariablePrefix) ||
		normalized.UpdatedByUserID == "" || len(normalized.Value) > maxBuildVariableValueLength ||
		(normalized.IsSecret && normalized.Value == "") {
		return domain.ModuleBuildVariable{}, domain.ErrInvalidBuildVariableInput
	}

	encrypted, err := s.encrypt(normalized.Value)
	if err != nil {
		return domain.ModuleBuildVariable{}, err
	}
	saved, err := s.repository.UpsertModuleBuildVariable(ctx, domain.ModuleBuildVariable{
		ModuleID:        normalized.ModuleID,
		Key:             normalized.Key,
		ValueEncrypted:  encrypted,
		IsSecret:        normalized.IsSecret,
		EnvironmentCode: normalized.EnvironmentCode,
		UpdatedByUserID: normalized.UpdatedByUserID,
	})
	if err != nil {
		return domain.ModuleBuildVariable{}, err
	}
	if err := s.revealForConsole(&saved); err != nil {
		return domain.ModuleBuildVariable{}, err
	}
	return saved, nil
}

func (s *BuildVariableService) DeleteBuildVariable(ctx context.Context, moduleID string, key string, environmentCode string) error {
	moduleID = strings.TrimSpace(moduleID)
	key = strings.TrimSpace(key)
	if moduleID == "" {
		return domain.ErrModuleNotFound
	}
	if !domain.IsValidBuildVariableKey(key) {
		return domain.ErrInvalidBuildVariableInput
	}
	return s.repository.DeleteModuleBuildVariable(ctx, moduleID, key, strings.TrimSpace(environmentCode))
}

// ResolveBuildVariables 供 worker 构建时使用：解密全部变量，同一个 key 以环境专属值覆盖通用值，结果按 key 排序。
// 返回值包含密钥明文，调用方不得写入日志或接口响应。
func (s *BuildVariableService) ResolveBuildVariables(ctx context.Context, moduleID string, environmentCode string) ([]domain.ModuleBuildVariable, error) {
	moduleID = strings.TrimSpace(moduleID)
	environmentCode = strings.TrimSpace(environmentCode)
	if moduleID == "" || environmentCode == "" {
		return nil, domain.ErrInvalidBuildInput
	}
	variables, err := s.repository.ListApplicableBuildVariables(ctx, moduleID, environmentCode)
	if err != nil {
		return nil, err
	}

	resolved := map[string]domain.ModuleBuildVariable{}
	for _, variable := range variables {
		if variable.EnvironmentCode != "" && variable.EnvironmentCode != environmentCode {
			continue
		}
		if current, ok := resolved[variable.Key]; ok && current.EnvironmentCode != "" {
			continue
		}
		value, err := s.decrypt(variable.ValueEncrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt build variable %s: %w", variable.Key, err)
		}
		variable.Value = value
		variable.ValueEncrypted = ""
		resolved[variable.Key] = variable
	}

	result := make([]domain.ModuleBuildVariable, 0, len(resolved))
	for _, variable := range resolved {
		result = append(result, variable)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result, nil
}

func (s *BuildVariableService) revealForConsole(variable *domain.ModuleBuildVariable) error {
	if variable.IsSecret {
		variable.Value = BuildVariableSecretMask
	} else {
		value, err := s.decrypt(variable.ValueEncrypted)
		if err != nil {
			return fmt.Errorf("decrypt build variable %s: %w", variable.Key, err)
		}
		variable.Value = value
	}
	variable.ValueEncrypted = ""
	return nil
}

func (s *BuildVariableService) encrypt(plaintext string) (string, error) {
	if s.secrets == nil {
		return "", domain.ErrCodeSourceAuthNotConfigured
	}
	ciphertext, err := s.secrets.Encrypt(plaintext)
	if err != nil {
		return "", fmt.Errorf("encrypt build variable: %w", err)
	}
	return ciphertext, nil
}

func (s *BuildVariableService) decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if s.secrets == nil {
		return "", domain.ErrCodeSourceAuthNotConfigured
	}
	return s.secrets.Decrypt(ciphertext)
}
