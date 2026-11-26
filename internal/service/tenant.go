package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"forge-server/internal/domain"
)

type TenantStore interface {
	CreateTenant(ctx context.Context, tenant domain.Tenant) (domain.Tenant, error)
	GetTenant(ctx context.Context, id string) (domain.Tenant, error)
	ListTenants(ctx context.Context) ([]domain.Tenant, error)
	CreateFirstTenant(ctx context.Context, tenant domain.Tenant) (bool, error)
	UpdateTenant(ctx context.Context, tenant domain.Tenant) (domain.Tenant, error)
}

// TenantService 管理租户和租户的飞书登录配置，并为登录流程解析飞书应用凭证。App Secret 加密落库，返回给调用方前清空。
type TenantService struct {
	store   TenantStore
	secrets SecretBox

	// setupToken 是首次初始化令牌，只在启动时租户表为空时生成，保存在进程内存里，用过一次即清空。
	setupMu    sync.Mutex
	setupToken string
}

func NewTenantService(store TenantStore, secrets SecretBox) *TenantService {
	return &TenantService{store: store, secrets: secrets}
}

// ListTenants 返回全部租户，AppSecret 已清空。
func (s *TenantService) ListTenants(ctx context.Context) ([]domain.Tenant, error) {
	tenants, err := s.store.ListTenants(ctx)
	if err != nil {
		return nil, err
	}
	for i := range tenants {
		tenants[i].FeishuAppSecret = ""
	}
	return tenants, nil
}

// ListActiveTenants 返回登录页可选的租户，AppSecret 已清空。
func (s *TenantService) ListActiveTenants(ctx context.Context) ([]domain.Tenant, error) {
	tenants, err := s.ListTenants(ctx)
	if err != nil {
		return nil, err
	}
	active := make([]domain.Tenant, 0, len(tenants))
	for _, tenant := range tenants {
		if tenant.Status == domain.TenantStatusActive {
			active = append(active, tenant)
		}
	}
	return active, nil
}

func (s *TenantService) CreateTenant(ctx context.Context, input domain.TenantInput) (domain.Tenant, error) {
	input = normalizeTenantInput(input)
	if !domain.IsValidTenantID(input.ID) || !domain.IsValidFeishuAppID(input.FeishuAppID) {
		return domain.Tenant{}, domain.ErrInvalidTenantInput
	}
	if input.Status == "" {
		input.Status = domain.TenantStatusActive
	}
	if err := validateTenantFields(input, true); err != nil {
		return domain.Tenant{}, err
	}
	ciphertext, err := s.encrypt(input.FeishuAppSecret)
	if err != nil {
		return domain.Tenant{}, err
	}
	created, err := s.store.CreateTenant(ctx, domain.Tenant{
		ID:              input.ID,
		Name:            input.Name,
		FeishuAppID:     input.FeishuAppID,
		FeishuAppSecret: ciphertext,
		Status:          input.Status,
		SortOrder:       input.SortOrder,
	})
	if err != nil {
		return domain.Tenant{}, err
	}
	created.FeishuAppSecret = ""
	return created, nil
}

// UpdateTenant 修改名称、状态、排序和 App Secret。App ID 不能修改：同一用户在不同应用下的 open_id 不同，
// 换应用等于换一批用户，应新建租户。
func (s *TenantService) UpdateTenant(ctx context.Context, id string, input domain.TenantInput) (domain.Tenant, error) {
	input = normalizeTenantInput(input)
	input.ID = strings.TrimSpace(id)
	existing, err := s.store.GetTenant(ctx, input.ID)
	if err != nil {
		return domain.Tenant{}, err
	}
	if input.FeishuAppID != "" && input.FeishuAppID != existing.FeishuAppID {
		return domain.Tenant{}, domain.ErrInvalidTenantInput
	}
	if err := validateTenantFields(input, false); err != nil {
		return domain.Tenant{}, err
	}
	ciphertext := ""
	if input.FeishuAppSecret != "" {
		if ciphertext, err = s.encrypt(input.FeishuAppSecret); err != nil {
			return domain.Tenant{}, err
		}
	}
	updated, err := s.store.UpdateTenant(ctx, domain.Tenant{
		ID:              input.ID,
		Name:            input.Name,
		FeishuAppSecret: ciphertext,
		Status:          input.Status,
		SortOrder:       input.SortOrder,
	})
	if err != nil {
		return domain.Tenant{}, err
	}
	updated.FeishuAppSecret = ""
	return updated, nil
}

// ResolveLoginTenant 确定登录使用的租户：指定了 ID 就用它（必须启用中）；没指定时只有一个启用租户才能自动选中。
func (s *TenantService) ResolveLoginTenant(ctx context.Context, id string) (domain.Tenant, domain.FeishuApp, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		active, err := s.ListActiveTenants(ctx)
		if err != nil {
			return domain.Tenant{}, domain.FeishuApp{}, err
		}
		switch len(active) {
		case 0:
			return domain.Tenant{}, domain.FeishuApp{}, domain.ErrFeishuAuthNotConfigured
		case 1:
			id = active[0].ID
		default:
			return domain.Tenant{}, domain.FeishuApp{}, domain.ErrTenantRequired
		}
	}
	return s.ResolveApp(ctx, id)
}

// ResolveApp 读取启用中租户的应用凭证。租户不存在或已停用时返回 ErrFeishuTenantNotFound。
func (s *TenantService) ResolveApp(ctx context.Context, id string) (domain.Tenant, domain.FeishuApp, error) {
	tenant, err := s.store.GetTenant(ctx, strings.TrimSpace(id))
	if err != nil {
		return domain.Tenant{}, domain.FeishuApp{}, err
	}
	if tenant.Status != domain.TenantStatusActive {
		return domain.Tenant{}, domain.FeishuApp{}, domain.ErrTenantNotFound
	}
	if s.secrets == nil {
		return domain.Tenant{}, domain.FeishuApp{}, domain.ErrFeishuAuthNotConfigured
	}
	secret, err := s.secrets.Decrypt(tenant.FeishuAppSecret)
	if err != nil {
		return domain.Tenant{}, domain.FeishuApp{}, fmt.Errorf("decrypt feishu app secret: %w", err)
	}
	tenant.FeishuAppSecret = ""
	return tenant, domain.FeishuApp{AppID: tenant.FeishuAppID, AppSecret: secret}, nil
}

// PrepareSetup 在租户表为空时生成首次初始化令牌并返回，由调用方打到启动日志里；已有租户时返回空字符串。
// 令牌只在当前进程内有效，所以首次部署只能运行一个实例。
func (s *TenantService) PrepareSetup(ctx context.Context) (string, error) {
	required, err := s.SetupRequired(ctx)
	if err != nil || !required {
		return "", err
	}
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate setup token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(value[:])
	s.setupMu.Lock()
	s.setupToken = token
	s.setupMu.Unlock()
	return token, nil
}

// SetupRequired 表示平台还没有任何租户，需要首次初始化。
func (s *TenantService) SetupRequired(ctx context.Context) (bool, error) {
	tenants, err := s.store.ListTenants(ctx)
	if err != nil {
		return false, err
	}
	return len(tenants) == 0, nil
}

// SetupFirstTenant 用初始化令牌创建平台的第一个租户。之后第一个登录的用户会成为 SuperAdmin。
// 已有租户时返回 ErrPlatformSetupCompleted；令牌不对时返回 ErrInvalidSetupToken。
func (s *TenantService) SetupFirstTenant(ctx context.Context, token string, input domain.TenantInput) (domain.Tenant, error) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.setupToken == "" {
		return domain.Tenant{}, domain.ErrPlatformSetupCompleted
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(s.setupToken)) != 1 {
		return domain.Tenant{}, domain.ErrInvalidSetupToken
	}

	input = normalizeTenantInput(input)
	input.Status = domain.TenantStatusActive
	input.SortOrder = 0
	if !domain.IsValidTenantID(input.ID) || !domain.IsValidFeishuAppID(input.FeishuAppID) {
		return domain.Tenant{}, domain.ErrInvalidTenantInput
	}
	if err := validateTenantFields(input, true); err != nil {
		return domain.Tenant{}, err
	}
	ciphertext, err := s.encrypt(input.FeishuAppSecret)
	if err != nil {
		return domain.Tenant{}, err
	}
	tenant := domain.Tenant{
		ID:              input.ID,
		Name:            input.Name,
		FeishuAppID:     input.FeishuAppID,
		FeishuAppSecret: ciphertext,
		Status:          input.Status,
	}
	created, err := s.store.CreateFirstTenant(ctx, tenant)
	if err != nil {
		return domain.Tenant{}, err
	}
	s.setupToken = ""
	if !created {
		return domain.Tenant{}, domain.ErrPlatformSetupCompleted
	}
	tenant.FeishuAppSecret = ""
	return tenant, nil
}

func (s *TenantService) encrypt(plaintext string) (string, error) {
	if s.secrets == nil {
		return "", domain.ErrFeishuAuthNotConfigured
	}
	ciphertext, err := s.secrets.Encrypt(plaintext)
	if err != nil {
		return "", fmt.Errorf("encrypt feishu app secret: %w", err)
	}
	return ciphertext, nil
}

func normalizeTenantInput(input domain.TenantInput) domain.TenantInput {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.FeishuAppID = strings.TrimSpace(input.FeishuAppID)
	input.FeishuAppSecret = strings.TrimSpace(input.FeishuAppSecret)
	return input
}

func validateTenantFields(input domain.TenantInput, secretRequired bool) error {
	nameLength := utf8.RuneCountInString(input.Name)
	if nameLength == 0 || nameLength > domain.MaxTenantNameLength {
		return domain.ErrInvalidTenantInput
	}
	if !domain.IsValidTenantStatus(input.Status) {
		return domain.ErrInvalidTenantInput
	}
	if (secretRequired && input.FeishuAppSecret == "") || len(input.FeishuAppSecret) > domain.MaxFeishuAppSecretLength {
		return domain.ErrInvalidTenantInput
	}
	if input.SortOrder < 0 || input.SortOrder > 10000 {
		return domain.ErrInvalidTenantInput
	}
	return nil
}
