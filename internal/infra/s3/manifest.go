package s3

import (
	"context"
	"fmt"

	"forge-server/internal/domain"
	"forge-server/internal/service"
)

type ManifestReader struct{}

type ObjectReader struct{}

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

func clientFromBuildRecord(record domain.BuildRecord) (*Client, error) {
	return NewClient(Options{
		Region:          record.StorageRegion,
		Bucket:          record.StorageBucket,
		Endpoint:        record.StorageEndpoint,
		AccessKeyID:     record.StorageAccessKeyID,
		SecretAccessKey: record.StorageSecretAccessKey,
		ForcePathStyle:  record.StorageForcePathStyle,
	})
}
