package s3

import (
	"context"
	"fmt"

	"forge-server/internal/domain"
	"forge-server/internal/service"
)

type ManifestReader struct{}

func NewManifestReader() ManifestReader {
	return ManifestReader{}
}

func (ManifestReader) ReadArtifactManifest(ctx context.Context, record domain.BuildRecord) (domain.ArtifactManifest, error) {
	client, err := NewClient(Options{
		Region:          record.StorageRegion,
		Bucket:          record.StorageBucket,
		Endpoint:        record.StorageEndpoint,
		AccessKeyID:     record.StorageAccessKeyID,
		SecretAccessKey: record.StorageSecretAccessKey,
	})
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
