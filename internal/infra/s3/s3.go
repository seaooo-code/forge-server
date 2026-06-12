package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

const defaultPresignTTL = 15 * time.Minute

var ErrInvalidKey = errors.New("s3 object key is required")

type Options struct {
	Region          string
	Bucket          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	ForcePathStyle  bool
	PresignTTL      time.Duration
}

type Client struct {
	bucket     string
	presignTTL time.Duration
	client     *awss3.Client
	presigner  *awss3.PresignClient
}

type Object struct {
	Body          io.ReadCloser
	ContentType   string
	ContentLength int64
	ETag          string
}

func NewClient(opts Options) (*Client, error) {
	bucket := strings.TrimSpace(opts.Bucket)
	if bucket == "" {
		return nil, errors.New("s3 bucket is required")
	}

	region := strings.TrimSpace(opts.Region)
	if region == "" {
		return nil, errors.New("s3 region is required")
	}

	endpoint := strings.TrimSpace(opts.Endpoint)
	if endpoint != "" {
		parsedEndpoint, err := url.Parse(endpoint)
		if err != nil || parsedEndpoint.Scheme == "" || parsedEndpoint.Host == "" {
			return nil, fmt.Errorf("parse s3 endpoint: expected absolute URL")
		}
	}

	accessKeyID := strings.TrimSpace(opts.AccessKeyID)
	secretAccessKey := strings.TrimSpace(opts.SecretAccessKey)
	sessionToken := strings.TrimSpace(opts.SessionToken)
	if accessKeyID == "" || secretAccessKey == "" {
		return nil, errors.New("s3 access key and secret access key are required")
	}

	options := awss3.Options{
		Region:       region,
		Credentials:  credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, sessionToken),
		UsePathStyle: opts.ForcePathStyle,
	}
	if endpoint != "" {
		options.BaseEndpoint = aws.String(endpoint)
	}
	client := awss3.New(options)

	presignTTL := opts.PresignTTL
	if presignTTL == 0 {
		presignTTL = defaultPresignTTL
	}
	if presignTTL < 0 {
		return nil, errors.New("s3 presign ttl must not be negative")
	}

	return &Client{
		bucket:     bucket,
		presignTTL: presignTTL,
		client:     client,
		presigner:  awss3.NewPresignClient(client),
	}, nil
}

func (c *Client) Bucket() string {
	return c.bucket
}

func (c *Client) HeadBucket(ctx context.Context) error {
	if _, err := c.client.HeadBucket(ctx, &awss3.HeadBucketInput{
		Bucket: aws.String(c.bucket),
	}); err != nil {
		return fmt.Errorf("head s3 bucket %s: %w", c.bucket, err)
	}
	return nil
}

func (c *Client) PutObject(ctx context.Context, key string, body io.Reader, contentType string) error {
	if strings.TrimSpace(key) == "" {
		return ErrInvalidKey
	}

	input := &awss3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   body,
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}

	if _, err := c.client.PutObject(ctx, input); err != nil {
		return fmt.Errorf("put s3 object %s/%s: %w", c.bucket, key, err)
	}
	return nil
}

func (c *Client) GetObject(ctx context.Context, key string) (Object, error) {
	if strings.TrimSpace(key) == "" {
		return Object{}, ErrInvalidKey
	}

	output, err := c.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return Object{}, fmt.Errorf("get s3 object %s/%s: %w", c.bucket, key, err)
	}

	return Object{
		Body:          output.Body,
		ContentType:   aws.ToString(output.ContentType),
		ContentLength: aws.ToInt64(output.ContentLength),
		ETag:          aws.ToString(output.ETag),
	}, nil
}

func (c *Client) DeleteObject(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return ErrInvalidKey
	}

	if _, err := c.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("delete s3 object %s/%s: %w", c.bucket, key, err)
	}
	return nil
}

func (c *Client) PresignGetObject(ctx context.Context, key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", ErrInvalidKey
	}

	request, err := c.presigner.PresignGetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, func(options *awss3.PresignOptions) {
		options.Expires = c.presignTTL
	})
	if err != nil {
		return "", fmt.Errorf("presign get s3 object %s/%s: %w", c.bucket, key, err)
	}
	return request.URL, nil
}

func (c *Client) PresignPutObject(ctx context.Context, key string, contentType string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", ErrInvalidKey
	}

	input := &awss3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}

	request, err := c.presigner.PresignPutObject(ctx, input, func(options *awss3.PresignOptions) {
		options.Expires = c.presignTTL
	})
	if err != nil {
		return "", fmt.Errorf("presign put s3 object %s/%s: %w", c.bucket, key, err)
	}
	return request.URL, nil
}
