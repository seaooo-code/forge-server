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

type FeishuTenantStore interface {
	CreateFeishuTenant(ctx context.Context, tenant domain.FeishuTenant) (domain.FeishuTenant, error)
	GetFeishuTenant(ctx context.Context, id string) (domain.FeishuTenant, error)
	ListFeishuTenants(ctx context.Context) ([]domain.FeishuTenant, error)
	CreateFirstFeishuTenant(ctx context.Context, tenant domain.FeishuTenant) (bool, error)
	UpdateFeishuTenant(ctx context.Context, tenant domain.FeishuTenant) (domain.FeishuTenant, error)
}

// FeishuTenantService 管理飞书登录租户，并为登录流程解析租户的应用凭证。App Secret 加密落库，返回给调用方前清空。
type FeishuTenantService struct {
	store   FeishuTenantStore
	secrets SecretBox

	// setupToken 是首次初始化令牌，只在启动时租户表为空时生成，保存在进程内存里，用过一次即清空。
	setupMu    sync.Mutex
	setupToken string
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

// PrepareSetup 在租户表为空时生成首次初始化令牌并返回，由调用方打到启动日志里；已有租户时返回空字符串。
// 令牌只在当前进程内有效，所以首次部署只能运行一个实例。
func (s *FeishuTenantService) PrepareSetup(ctx context.Context) (string, error) {
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
func (s *FeishuTenantService) SetupRequired(ctx context.Context) (bool, error) {
	tenants, err := s.store.ListFeishuTenants(ctx)
	if err != nil {
		return false, err
	}
	return len(tenants) == 0, nil
}

// SetupFirstTenant 用初始化令牌创建平台的第一个租户。之后第一个登录的用户会成为 SuperAdmin。
// 已有租户时返回 ErrPlatformSetupCompleted；令牌不对时返回 ErrInvalidSetupToken。
func (s *FeishuTenantService) SetupFirstTenant(ctx context.Context, token string, input domain.FeishuTenantInput) (domain.FeishuTenant, error) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.setupToken == "" {
		return domain.FeishuTenant{}, domain.ErrPlatformSetupCompleted
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(s.setupToken)) != 1 {
		return domain.FeishuTenant{}, domain.ErrInvalidSetupToken
	}

	input = normalizeFeishuTenantInput(input)
	input.Status = domain.FeishuTenantStatusActive
	input.SortOrder = 0
	if !domain.IsValidFeishuTenantID(input.ID) || !domain.IsValidFeishuTenantAppID(input.AppID) {
		return domain.FeishuTenant{}, domain.ErrInvalidFeishuTenantInput
	}
	if err := validateFeishuTenantFields(input, true); err != nil {
		return domain.FeishuTenant{}, err
	}
	ciphertext, err := s.encrypt(input.AppSecret)
	if err != nil {
		return domain.FeishuTenant{}, err
	}
	tenant := domain.FeishuTenant{
		ID:        input.ID,
		Name:      input.Name,
		AppID:     input.AppID,
		AppSecret: ciphertext,
		Status:    input.Status,
	}
	created, err := s.store.CreateFirstFeishuTenant(ctx, tenant)
	if err != nil {
		return domain.FeishuTenant{}, err
	}
	s.setupToken = ""
	if !created {
		return domain.FeishuTenant{}, domain.ErrPlatformSetupCompleted
	}
	tenant.AppSecret = ""
	return tenant, nil
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
