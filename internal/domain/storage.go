package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrStorageConfigurationNotFound     = errors.New("storage configuration not found")
	ErrInvalidStorageConfigurationInput = errors.New("invalid storage configuration request")
	ErrStorageConfigurationTestFailed   = errors.New("storage configuration test failed")
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

type StorageConfigurationCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
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

func ParseStorageStatus(value string) (StorageStatus, bool) {
	switch strings.TrimSpace(value) {
	case "", string(StorageStatusHealthy), "正常":
		return StorageStatusHealthy, true
	case string(StorageStatusError), "异常":
		return StorageStatusError, true
	case string(StorageStatusDisabled), "已停用":
		return StorageStatusDisabled, true
	default:
		return "", false
	}
}

func IsValidStorageAdapter(adapter StorageAdapter) bool {
	return adapter == StorageAdapterS3
}
