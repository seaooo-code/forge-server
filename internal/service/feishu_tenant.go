package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"forge-server/internal/domain"
)

type FeishuTenantStore interface {
	CreateFeishuTenant(ctx context.Context, tenant domain.FeishuTenant) (domain.FeishuTenant, error)
	GetFeishuTenant(ctx context.Context, id string) (domain.FeishuTenant, error)
	ListFeishuTenants(ctx context.Context) ([]domain.FeishuTenant, error)
	SeedFeishuTenant(ctx context.Context, tenant domain.FeishuTenant) (bool, error)
	UpdateFeishuTenant(ctx context.Context, tenant domain.FeishuTenant) (domain.FeishuTenant, error)
}

// FeishuTenantService 管理飞书登录租户，并为登录流程解析租户的应用凭证。App Secret 加密落库，返回给调用方前清空。
type FeishuTenantService struct {
	store   FeishuTenantStore
	secrets SecretBox
}

func NewFeishuTenantService(store FeishuTenantStore, secrets SecretBox) *FeishuTenantService {
	return &FeishuTenantService{store: store, secrets: secrets}
}

// ListFeishuTenants 返回全部租户，AppSecret 已清空。
func (s *FeishuTenantService) ListFeishuTenants(ctx context.Context) ([]domain.FeishuTenant, error) {
	tenants, err := s.store.ListFeishuTenants(ctx)
	if err != nil {
		return nil, err
	}
	for i := range tenants {
		tenants[i].AppSecret = ""
	}
	return tenants, nil
}

// ListActiveFeishuTenants 返回登录页可选的租户，AppSecret 已清空。
func (s *FeishuTenantService) ListActiveFeishuTenants(ctx context.Context) ([]domain.FeishuTenant, error) {
	tenants, err := s.ListFeishuTenants(ctx)
	if err != nil {
		return nil, err
	}
	active := make([]domain.FeishuTenant, 0, len(tenants))
	for _, tenant := range tenants {
		if tenant.Status == domain.FeishuTenantStatusActive {
			active = append(active, tenant)
		}
	}
	return active, nil
}

func (s *FeishuTenantService) CreateFeishuTenant(ctx context.Context, input domain.FeishuTenantInput) (domain.FeishuTenant, error) {
	input = normalizeFeishuTenantInput(input)
	if !domain.IsValidFeishuTenantID(input.ID) || !domain.IsValidFeishuTenantAppID(input.AppID) {
		return domain.FeishuTenant{}, domain.ErrInvalidFeishuTenantInput
	}
	if input.Status == "" {
		input.Status = domain.FeishuTenantStatusActive
	}
	if err := validateFeishuTenantFields(input, true); err != nil {
		return domain.FeishuTenant{}, err
	}
	ciphertext, err := s.encrypt(input.AppSecret)
	if err != nil {
		return domain.FeishuTenant{}, err
	}
	created, err := s.store.CreateFeishuTenant(ctx, domain.FeishuTenant{
		ID:        input.ID,
		Name:      input.Name,
		AppID:     input.AppID,
		AppSecret: ciphertext,
		Status:    input.Status,
		SortOrder: input.SortOrder,
	})
	if err != nil {
		return domain.FeishuTenant{}, err
	}
	created.AppSecret = ""
	return created, nil
}

// UpdateFeishuTenant 修改名称、状态、排序和 App Secret。App ID 不能修改：同一用户在不同应用下的 open_id 不同，
// 换应用等于换一批用户，应新建租户。
func (s *FeishuTenantService) UpdateFeishuTenant(ctx context.Context, id string, input domain.FeishuTenantInput) (domain.FeishuTenant, error) {
	input = normalizeFeishuTenantInput(input)
	input.ID = strings.TrimSpace(id)
	existing, err := s.store.GetFeishuTenant(ctx, input.ID)
	if err != nil {
		return domain.FeishuTenant{}, err
	}
	if input.AppID != "" && input.AppID != existing.AppID {
		return domain.FeishuTenant{}, domain.ErrInvalidFeishuTenantInput
	}
	if err := validateFeishuTenantFields(input, false); err != nil {
		return domain.FeishuTenant{}, err
	}
	ciphertext := ""
	if input.AppSecret != "" {
		if ciphertext, err = s.encrypt(input.AppSecret); err != nil {
			return domain.FeishuTenant{}, err
		}
	}
	updated, err := s.store.UpdateFeishuTenant(ctx, domain.FeishuTenant{
		ID:        input.ID,
		Name:      input.Name,
		AppSecret: ciphertext,
		Status:    input.Status,
		SortOrder: input.SortOrder,
	})
	if err != nil {
		return domain.FeishuTenant{}, err
	}
	updated.AppSecret = ""
	return updated, nil
}

// ResolveLoginTenant 确定登录使用的租户：指定了 ID 就用它（必须启用中）；没指定时只有一个启用租户才能自动选中。
func (s *FeishuTenantService) ResolveLoginTenant(ctx context.Context, id string) (domain.FeishuTenant, domain.FeishuApp, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		active, err := s.ListActiveFeishuTenants(ctx)
		if err != nil {
			return domain.FeishuTenant{}, domain.FeishuApp{}, err
		}
		switch len(active) {
		case 0:
			return domain.FeishuTenant{}, domain.FeishuApp{}, domain.ErrFeishuAuthNotConfigured
		case 1:
			id = active[0].ID
		default:
			return domain.FeishuTenant{}, domain.FeishuApp{}, domain.ErrFeishuTenantRequired
		}
	}
	return s.ResolveApp(ctx, id)
}

// ResolveApp 读取启用中租户的应用凭证。租户不存在或已停用时返回 ErrFeishuTenantNotFound。
func (s *FeishuTenantService) ResolveApp(ctx context.Context, id string) (domain.FeishuTenant, domain.FeishuApp, error) {
	tenant, err := s.store.GetFeishuTenant(ctx, strings.TrimSpace(id))
	if err != nil {
		return domain.FeishuTenant{}, domain.FeishuApp{}, err
	}
	if tenant.Status != domain.FeishuTenantStatusActive {
		return domain.FeishuTenant{}, domain.FeishuApp{}, domain.ErrFeishuTenantNotFound
	}
	if s.secrets == nil {
		return domain.FeishuTenant{}, domain.FeishuApp{}, domain.ErrFeishuAuthNotConfigured
	}
	secret, err := s.secrets.Decrypt(tenant.AppSecret)
	if err != nil {
		return domain.FeishuTenant{}, domain.FeishuApp{}, fmt.Errorf("decrypt feishu app secret: %w", err)
	}
	tenant.AppSecret = ""
	return tenant, domain.FeishuApp{AppID: tenant.AppID, AppSecret: secret}, nil
}

// SeedFromConfig 在租户表为空时，把 FEISHU_APP_ID / FEISHU_APP_SECRET 写成默认租户，返回是否写入。
// 之后租户只在控制台维护，这两个环境变量不再生效。
func (s *FeishuTenantService) SeedFromConfig(ctx context.Context, appID string, appSecret string) (bool, error) {
	appID, appSecret = strings.TrimSpace(appID), strings.TrimSpace(appSecret)
	if appID == "" || appSecret == "" {
		return false, nil
	}
	if !domain.IsValidFeishuTenantAppID(appID) {
		return false, fmt.Errorf("%w: FEISHU_APP_ID 格式不正确", domain.ErrInvalidFeishuTenantInput)
	}
	ciphertext, err := s.encrypt(appSecret)
	if err != nil {
		return false, err
	}
	return s.store.SeedFeishuTenant(ctx, domain.FeishuTenant{
		ID:        domain.DefaultFeishuTenantID,
		Name:      "默认租户",
		AppID:     appID,
		AppSecret: ciphertext,
		Status:    domain.FeishuTenantStatusActive,
	})
}

func (s *FeishuTenantService) encrypt(plaintext string) (string, error) {
	if s.secrets == nil {
		return "", domain.ErrFeishuAuthNotConfigured
	}
	ciphertext, err := s.secrets.Encrypt(plaintext)
	if err != nil {
		return "", fmt.Errorf("encrypt feishu app secret: %w", err)
	}
	return ciphertext, nil
}

func normalizeFeishuTenantInput(input domain.FeishuTenantInput) domain.FeishuTenantInput {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.AppID = strings.TrimSpace(input.AppID)
	input.AppSecret = strings.TrimSpace(input.AppSecret)
	return input
}

func validateFeishuTenantFields(input domain.FeishuTenantInput, secretRequired bool) error {
	nameLength := utf8.RuneCountInString(input.Name)
	if nameLength == 0 || nameLength > domain.MaxFeishuTenantNameLength {
		return domain.ErrInvalidFeishuTenantInput
	}
	if !domain.IsValidFeishuTenantStatus(input.Status) {
		return domain.ErrInvalidFeishuTenantInput
	}
	if (secretRequired && input.AppSecret == "") || len(input.AppSecret) > domain.MaxFeishuTenantSecretLength {
		return domain.ErrInvalidFeishuTenantInput
	}
	if input.SortOrder < 0 || input.SortOrder > 10000 {
		return domain.ErrInvalidFeishuTenantInput
	}
	return nil
}
