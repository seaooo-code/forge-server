package s3

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"forge-server/internal/domain"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/smithy-go"
)

type Tester struct{}

func NewTester() *Tester {
	return &Tester{}
}

func (Tester) TestStorageConfiguration(ctx context.Context, input domain.StorageConfigurationInput) error {
	client, err := NewClient(optionsFromStorageConfigurationInput(input))
	if err != nil {
		return &domain.StorageConfigurationTestError{Detail: describeClientConfigError(err)}
	}
	if err := client.HeadBucket(ctx); err != nil {
		return &domain.StorageConfigurationTestError{Detail: describeRequestError(err)}
	}
	return nil
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

func describeClientConfigError(err error) string {
	if strings.Contains(err.Error(), "endpoint") {
		return "Endpoint 需要是带协议的完整地址，例如 https://s3.example.com"
	}
	return err.Error()
}

// describeRequestError 把 SDK 错误归类为可读原因。只输出状态码、错误码和网络错误类别，
// 不输出请求头或签名，避免泄露凭证。
func describeRequestError(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return fmt.Sprintf("无法解析 Endpoint 域名 %s", dnsErr.Name)
	}
	var urlErr *url.Error
	var opErr *net.OpError
	if errors.As(err, &opErr) || (errors.As(err, &urlErr) && urlErr.Timeout()) {
		return "无法连接 Endpoint，请检查地址、端口和网络"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "连接 Endpoint 超时"
	}

	status := 0
	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		status = respErr.HTTPStatusCode()
	}
	code := ""
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code = apiErr.ErrorCode()
	}

	switch {
	case status == 403:
		return fmt.Sprintf("HTTP 403：无权访问 Bucket，请检查 AK/SK 是否正确、所属用户组是否已授权该 Bucket（%s）", fallback(code, "Forbidden"))
	case status == 404:
		return fmt.Sprintf("HTTP 404：Bucket 不存在，请检查 Bucket 名称，以及 Endpoint 是否为 Bucket 所在集群（%s）", fallback(code, "NotFound"))
	case status == 400 || status == 301:
		return fmt.Sprintf("HTTP %d：请求被拒绝，常见原因是 AK/SK 无效（部分 S3 兼容服务对鉴权失败返回 400）、Endpoint 不是 S3 兼容地址，或 Region 与签名要求不一致（%s）", status, fallback(code, "BadRequest"))
	case status > 0:
		return fmt.Sprintf("HTTP %d（%s）", status, fallback(code, "未知错误"))
	case code != "":
		return code
	}
	return "请求失败，请检查连接参数"
}

func fallback(value string, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}
