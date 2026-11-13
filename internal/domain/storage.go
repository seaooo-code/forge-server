package domain

import (
	"errors"
	"time"
)

var (
	ErrStorageConfigurationNotFound     = errors.New("storage configuration not found")
	ErrInvalidStorageConfigurationInput = errors.New("invalid storage configuration request")
	ErrStorageConfigurationTestFailed   = errors.New("storage configuration test failed")
	ErrStorageConfigurationInUse        = errors.New("storage configuration is referenced by environments")
)

type StorageAdapter string

const (
	StorageAdapterS3 StorageAdapter = "S3"
)

type StorageStatus string

const (
	StorageStatusHealthy  StorageStatus = "healthy"
	StorageStatusError    StorageStatus = "error"
	StorageStatusDisabled StorageStatus = "disabled"
)

type StorageConfiguration struct {
	ID             string
	Name           string
	Adapter        StorageAdapter
	Bucket         string
	Endpoint       string
	Region         string
	OwnerUserID    string
	Owner          string
	OwnerEmail     string
	OwnerAvatarURL string
	PublicBaseURL  string
	Status         StorageStatus
	LastVerifiedAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// StorageCredentials 是已保存的对象存储访问密钥，只在服务端内部使用，不通过读接口返回。
type StorageCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
}

type StorageConfigurationInput struct {
	Name               string
	Adapter            StorageAdapter
	Bucket             string
	Endpoint           string
	Region             string
	OwnerUserID        string
	Owner              string
	OwnerEmail         string
	OwnerAvatarURL     string
	OwnerFeishuUnionID string
	OwnerFeishuOpenID  string
	OwnerFeishuUserID  string
	PublicBaseURL      string
	AccessKeyID        string
	SecretAccessKey    string
	Status             StorageStatus
}

type StorageConfigurationTestResult struct {
	OK       bool
	TestedAt time.Time
}

func (s StorageStatus) Label() string {
	switch s {
	case StorageStatusHealthy:
		return "正常"
	case StorageStatusError:
		return "异常"
	case StorageStatusDisabled:
		return "已停用"
	default:
		return string(s)
	}
}

func IsValidStorageStatus(status StorageStatus) bool {
	switch status {
	case StorageStatusHealthy, StorageStatusError, StorageStatusDisabled:
		return true
	default:
		return false
	}
}

func IsValidStorageAdapter(adapter StorageAdapter) bool {
	return adapter == StorageAdapterS3
}
