package s3

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"forge-server/internal/domain"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestNewClientRequiresBucket(t *testing.T) {
	_, err := NewClient(Options{
		Region: "us-east-1",
	})
	if err == nil || !strings.Contains(err.Error(), "bucket") {
		t.Fatalf("expected missing bucket error, got %v", err)
	}
}

func TestNewClientRequiresStaticCredentials(t *testing.T) {
	_, err := NewClient(Options{
		Region:      "us-east-1",
		Bucket:      "forge-assets",
		AccessKeyID: "access-key",
	})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected credential required error, got %v", err)
	}
}

func TestPutGetDeleteObjectUseConfiguredEndpointAndBucket(t *testing.T) {
	var sawPut bool
	var sawGet bool
	var sawDelete bool

	client := newTestClient(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/forge-assets/avatars/user.png":
			sawPut = true
			if r.Header.Get("Content-Type") != "image/png" {
				return nil, errors.New("expected image/png content type, got " + r.Header.Get("Content-Type"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			if string(body) != "object-body" {
				return nil, errors.New("expected put body, got " + string(body))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		case r.Method == http.MethodGet && r.URL.Path == "/forge-assets/avatars/user.png":
			sawGet = true
			header := make(http.Header)
			header.Set("Content-Type", "image/png")
			header.Set("Etag", `"etag-value"`)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader("object-body")),
			}, nil
		case r.Method == http.MethodDelete && r.URL.Path == "/forge-assets/avatars/user.png":
			sawDelete = true
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		default:
			return nil, errors.New("unexpected " + r.Method + " " + r.URL.String())
		}
	})

	if err := client.PutObject(context.Background(), "avatars/user.png", strings.NewReader("object-body"), "image/png"); err != nil {
		t.Fatalf("put object: %v", err)
	}

	object, err := client.GetObject(context.Background(), "avatars/user.png")
	if err != nil {
		t.Fatalf("get object: %v", err)
	}
	defer object.Body.Close()
	body, err := io.ReadAll(object.Body)
	if err != nil {
		t.Fatalf("read object body: %v", err)
	}
	if string(body) != "object-body" {
		t.Fatalf("expected object body, got %q", string(body))
	}
	if object.ContentType != "image/png" {
		t.Fatalf("expected object content type, got %q", object.ContentType)
	}
	if object.ETag != `"etag-value"` {
		t.Fatalf("expected object etag, got %q", object.ETag)
	}

	if err := client.DeleteObject(context.Background(), "avatars/user.png"); err != nil {
		t.Fatalf("delete object: %v", err)
	}

	if !sawPut || !sawGet || !sawDelete {
		t.Fatalf("expected put/get/delete requests, got put=%t get=%t delete=%t", sawPut, sawGet, sawDelete)
	}
}

func TestPresignGetObjectUsesConfiguredTTL(t *testing.T) {
	client, err := NewClient(Options{
		Region:          "us-east-1",
		Bucket:          "forge-assets",
		Endpoint:        "http://127.0.0.1:9000",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		ForcePathStyle:  true,
		PresignTTL:      10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	presignedURL, err := client.PresignGetObject(context.Background(), "avatars/user.png")
	if err != nil {
		t.Fatalf("presign get object: %v", err)
	}

	parsedURL, err := url.Parse(presignedURL)
	if err != nil {
		t.Fatalf("parse presigned url: %v", err)
	}
	if parsedURL.Path != "/forge-assets/avatars/user.png" {
		t.Fatalf("expected path-style presigned path, got %q", parsedURL.Path)
	}
	if parsedURL.Query().Get("X-Amz-Expires") != "600" {
		t.Fatalf("expected 600 second expiry, got %q", parsedURL.Query().Get("X-Amz-Expires"))
	}
}

func TestObjectMethodsRejectBlankKey(t *testing.T) {
	client, err := NewClient(Options{
		Region:          "us-east-1",
		Bucket:          "forge-assets",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	if err := client.PutObject(context.Background(), " ", strings.NewReader(""), ""); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected invalid key from put, got %v", err)
	}
	if _, err := client.GetObject(context.Background(), " "); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected invalid key from get, got %v", err)
	}
	if err := client.DeleteObject(context.Background(), " "); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected invalid key from delete, got %v", err)
	}
	if _, err := client.PresignGetObject(context.Background(), " "); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected invalid key from presign get, got %v", err)
	}
	if _, err := client.PresignPutObject(context.Background(), " ", ""); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected invalid key from presign put, got %v", err)
	}
}

func TestOptionsFromStorageConfigurationInputNormalizesEndpointAndRegion(t *testing.T) {
	options := optionsFromStorageConfigurationInput(domain.StorageConfigurationInput{
		Bucket:          "forge-assets",
		Endpoint:        "s3.example.com",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
	})

	if options.Endpoint != "https://s3.example.com" {
		t.Fatalf("expected https endpoint, got %q", options.Endpoint)
	}
	if options.Region != defaultStorageTestRegion {
		t.Fatalf("expected default region, got %q", options.Region)
	}
}

func newTestClient(do func(*http.Request) (*http.Response, error)) *Client {
	client := awss3.New(awss3.Options{
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("access-key", "secret-key", ""),
		BaseEndpoint: aws.String("http://s3.test"),
		UsePathStyle: true,
		HTTPClient:   roundTripHTTPClient{do: do},
	})

	return &Client{
		bucket:     "forge-assets",
		presignTTL: time.Minute,
		client:     client,
		presigner:  awss3.NewPresignClient(client),
	}
}

type roundTripHTTPClient struct {
	do func(*http.Request) (*http.Response, error)
}

func (c roundTripHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return c.do(req)
}
