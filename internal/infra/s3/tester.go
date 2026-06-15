package s3

import (
	"context"

	"forge-server/internal/domain"
)

type Tester struct{}

func NewTester() *Tester {
	return &Tester{}
}

func (Tester) TestStorageConfiguration(ctx context.Context, input domain.StorageConfigurationInput) error {
	client, err := NewClient(optionsFromStorageConfigurationInput(input))
	if err != nil {
		return err
	}
	return client.HeadBucket(ctx)
}

func optionsFromStorageConfigurationInput(input domain.StorageConfigurationInput) Options {
	return Options{
		Region:          input.Region,
		Bucket:          input.Bucket,
		Endpoint:        input.Endpoint,
		AccessKeyID:     input.AccessKeyID,
		SecretAccessKey: input.SecretAccessKey,
	}
}
