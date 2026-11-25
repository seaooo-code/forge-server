package s3

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"forge-server/internal/domain"
	"forge-server/internal/service"
)

type ManifestReader struct{}

type ObjectReader struct{}

// ObjectPresigner 按构建记录自己的存储配置写入对象，并生成预签名的 GET 地址。
type ObjectPresigner struct{}

func NewManifestReader() ManifestReader {
	return ManifestReader{}
}

func NewObjectReader() ObjectReader {
	return ObjectReader{}
}

func (ManifestReader) ReadArtifactManifest(ctx context.Context, record domain.BuildRecord) (domain.ArtifactManifest, error) {
	client, err := clientFromBuildRecord(record)
	if err != nil {
		return domain.ArtifactManifest{}, err
	}

	object, err := client.GetObject(ctx, record.ManifestKey)
	if err != nil {
		return domain.ArtifactManifest{}, err
	}
	defer object.Body.Close()

	manifest, err := service.DecodeArtifactManifest(object.Body)
	if err != nil {
		return domain.ArtifactManifest{}, fmt.Errorf("decode artifact manifest: %w", err)
	}
	return manifest, nil
}

func (ObjectReader) GetArtifactObject(ctx context.Context, record domain.BuildRecord, objectKey string) (domain.ArtifactObject, error) {
	client, err := clientFromBuildRecord(record)
	if err != nil {
		return domain.ArtifactObject{}, err
	}

	object, err := client.GetObject(ctx, objectKey)
	if err != nil {
		return domain.ArtifactObject{}, err
	}
	return domain.ArtifactObject{
		Body:          object.Body,
		ContentType:   object.ContentType,
		ContentLength: object.ContentLength,
		ETag:          object.ETag,
	}, nil
}

// PutArtifactObject 把 body 写到构建记录所用存储的 objectKey。
func (ObjectPresigner) PutArtifactObject(ctx context.Context, record domain.BuildRecord, objectKey string, body []byte, contentType string) error {
	client, err := clientFromBuildRecord(record)
	if err != nil {
		return err
	}
	return client.PutObject(ctx, objectKey, bytes.NewReader(body), contentType)
}

// PresignArtifactObject 生成 objectKey 的预签名 GET 地址，ttl 为有效期。
func (ObjectPresigner) PresignArtifactObject(ctx context.Context, record domain.BuildRecord, objectKey string, ttl time.Duration) (string, error) {
	client, err := clientFromBuildRecordWithTTL(record, ttl)
	if err != nil {
		return "", err
	}
	return client.PresignGetObject(ctx, objectKey)
}

func clientFromBuildRecord(record domain.BuildRecord) (*Client, error) {
	return clientFromBuildRecordWithTTL(record, 0)
}

func clientFromBuildRecordWithTTL(record domain.BuildRecord, presignTTL time.Duration) (*Client, error) {
	return NewClient(Options{
		Region:          record.StorageRegion,
		Bucket:          record.StorageBucket,
		Endpoint:        record.StorageEndpoint,
		AccessKeyID:     record.StorageAccessKeyID,
		SecretAccessKey: record.StorageSecretAccessKey,
		ForcePathStyle:  record.StorageForcePathStyle,
		PresignTTL:      presignTTL,
	})
}
