package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"forge-server/internal/domain"
	"forge-server/internal/service"
)

const (
	// ciRequestTimeout 是普通 CI 接口调用的超时，下载 artifacts 不受它限制。
	ciRequestTimeout = 30 * time.Second
	// maxTraceChunkBytes 是单次读取 job 日志的上限，剩余部分下一次轮询继续读取。
	maxTraceChunkBytes = 4 << 20
	// maxJobPages 是列出 pipeline job 时最多翻的页数，防止异常分页导致死循环。
	maxJobPages = 20
)

// ciClient 是绑定了 GitLab 项目的 CI 客户端，实现 service.CIClient。
type ciClient struct {
	httpClient *http.Client
	baseURL    *url.URL
	projectID  string
	token      string
}

var (
	_ service.CIProvider = (*Provider)(nil)
	_ service.CIClient   = (*ciClient)(nil)
)

// CIClient 为连接和仓库创建 CI 客户端。令牌需要 api scope，并且在项目上至少是 Developer 角色才能创建 pipeline。
func (p *Provider) CIClient(_ context.Context, conn domain.CodeSourceConnection, repo domain.ModuleRepository) (service.CIClient, error) {
	if strings.TrimSpace(conn.Credential) == "" {
		return nil, fmt.Errorf("%w: gitlab access token is required", domain.ErrCodeSourceCredentialInvalid)
	}
	baseURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(conn.APIBaseURL), "/"))
	if err != nil || baseURL.Host == "" {
		return nil, fmt.Errorf("%w: invalid gitlab api base url", domain.ErrInvalidCodeSourceInput)
	}
	projectID := strings.TrimSpace(repo.ExternalRepositoryID)
	if id, err := strconv.ParseInt(projectID, 10, 64); err != nil || id <= 0 {
		return nil, fmt.Errorf("%w: invalid gitlab project id %q", domain.ErrInvalidCodeSourceInput, projectID)
	}
	return &ciClient{httpClient: p.ciHTTPClient, baseURL: baseURL, projectID: projectID, token: conn.Credential}, nil
}

type pipelineResponse struct {
	ID     int64  `json:"id"`
	SHA    string `json:"sha"`
	Status string `json:"status"`
	WebURL string `json:"web_url"`
}

func (r pipelineResponse) pipeline() domain.CIPipeline {
	return domain.CIPipeline{ID: r.ID, URL: r.WebURL, SHA: r.SHA, Status: domain.CIStatus(r.Status)}
}

type jobResponse struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	WebURL string `json:"web_url"`
}

type pipelineVariable struct {
	Key          string `json:"key"`
	Value        string `json:"value"`
	VariableType string `json:"variable_type"`
}

func (c *ciClient) CreatePipeline(ctx context.Context, ref string, variables []domain.CIVariable) (domain.CIPipeline, error) {
	body := struct {
		Ref       string             `json:"ref"`
		Variables []pipelineVariable `json:"variables"`
	}{Ref: ref, Variables: make([]pipelineVariable, 0, len(variables))}
	for _, variable := range variables {
		body.Variables = append(body.Variables, pipelineVariable{Key: variable.Key, Value: variable.Value, VariableType: "env_var"})
	}

	var response pipelineResponse
	if err := c.doJSON(ctx, http.MethodPost, "/projects/"+c.projectID+"/pipeline", nil, body, &response); err != nil {
		if errors.Is(err, domain.ErrCIPipelineNotFound) {
			return domain.CIPipeline{}, fmt.Errorf("%w: gitlab project %s not found or the token has no access", domain.ErrCodeSourceAuthorization, c.projectID)
		}
		return domain.CIPipeline{}, err
	}
	return response.pipeline(), nil
}

func (c *ciClient) GetPipeline(ctx context.Context, pipelineID int64) (domain.CIPipeline, error) {
	var response pipelineResponse
	if err := c.doJSON(ctx, http.MethodGet, c.pipelinePath(pipelineID), nil, nil, &response); err != nil {
		return domain.CIPipeline{}, err
	}
	return response.pipeline(), nil
}

func (c *ciClient) ListPipelineJobs(ctx context.Context, pipelineID int64) ([]domain.CIJob, error) {
	jobs := []domain.CIJob{}
	for page := 1; page <= maxJobPages; page++ {
		query := url.Values{}
		query.Set("per_page", "100")
		query.Set("page", strconv.Itoa(page))
		var response []jobResponse
		header, err := c.doJSONHeader(ctx, http.MethodGet, c.pipelinePath(pipelineID)+"/jobs", query, nil, &response)
		if err != nil {
			return nil, err
		}
		for _, job := range response {
			jobs = append(jobs, domain.CIJob{ID: job.ID, Name: job.Name, Status: domain.CIStatus(job.Status), URL: job.WebURL})
		}
		if strings.TrimSpace(header.Get("X-Next-Page")) == "" {
			break
		}
	}
	return jobs, nil
}

// JobTrace 优先用 Range 只读新增部分；服务端忽略 Range 返回整段日志时，跳过前 offset 字节。
func (c *ciClient) JobTrace(ctx context.Context, jobID int64, offset int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, ciRequestTimeout)
	defer cancel()

	header := map[string]string{}
	if offset > 0 {
		header["Range"] = "bytes=" + strconv.FormatInt(offset, 10) + "-"
	}
	resp, err := c.request(ctx, http.MethodGet, "/projects/"+c.projectID+"/jobs/"+strconv.FormatInt(jobID, 10)+"/trace", nil, nil, header)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusRequestedRangeNotSatisfiable, http.StatusNotFound:
		// 没有新增内容，或 job 还没有产生日志。
		return nil, nil
	case http.StatusPartialContent:
	case http.StatusOK:
		if offset > 0 {
			skipped, err := io.CopyN(io.Discard, resp.Body, offset)
			if err != nil {
				if errors.Is(err, io.EOF) && skipped < offset {
					return nil, nil
				}
				return nil, fmt.Errorf("%w: skip gitlab job trace: %w", domain.ErrCodeSourceAuthorization, err)
			}
		}
	default:
		return nil, ciStatusError(resp, "读取 job 日志")
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTraceChunkBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: read gitlab job trace: %w", domain.ErrCodeSourceAuthorization, err)
	}
	return data, nil
}

func (c *ciClient) DownloadJobArtifacts(ctx context.Context, jobID int64, dst io.Writer, maxBytes int64) error {
	resp, err := c.request(ctx, http.MethodGet, "/projects/"+c.projectID+"/jobs/"+strconv.FormatInt(jobID, 10)+"/artifacts", nil, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return domain.ErrCIArtifactsNotFound
	default:
		return ciStatusError(resp, "下载 job artifacts")
	}
	if maxBytes > 0 && resp.ContentLength > maxBytes {
		return fmt.Errorf("artifacts 大小 %d 字节超过上限 %d 字节", resp.ContentLength, maxBytes)
	}

	limit := maxBytes
	if limit <= 0 {
		limit = 1 << 62
	}
	written, err := io.CopyN(dst, resp.Body, limit+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: download gitlab job artifacts: %w", domain.ErrCodeSourceAuthorization, err)
	}
	if written > limit {
		return fmt.Errorf("artifacts 大小超过上限 %d 字节", maxBytes)
	}
	return nil
}

func (c *ciClient) CancelPipeline(ctx context.Context, pipelineID int64) error {
	err := c.doJSON(ctx, http.MethodPost, c.pipelinePath(pipelineID)+"/cancel", nil, nil, nil)
	if errors.Is(err, domain.ErrCIPipelineNotFound) {
		return nil
	}
	return err
}

func (c *ciClient) pipelinePath(pipelineID int64) string {
	return "/projects/" + c.projectID + "/pipelines/" + strconv.FormatInt(pipelineID, 10)
}

func (c *ciClient) doJSON(ctx context.Context, method string, path string, query url.Values, body any, target any) error {
	_, err := c.doJSONHeader(ctx, method, path, query, body, target)
	return err
}

// doJSONHeader 发送带超时的 JSON 请求；target 为空时忽略响应体。404 统一映射为 domain.ErrCIPipelineNotFound。
func (c *ciClient) doJSONHeader(ctx context.Context, method string, path string, query url.Values, body any, target any) (http.Header, error) {
	ctx, cancel := context.WithTimeout(ctx, ciRequestTimeout)
	defer cancel()

	resp, err := c.request(ctx, method, path, query, body, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, domain.ErrCIPipelineNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ciStatusError(resp, method+" "+path)
	}
	if target == nil {
		return resp.Header, nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(target); err != nil {
		return nil, fmt.Errorf("%w: decode gitlab api response: %w", domain.ErrCodeSourceAuthorization, err)
	}
	return resp.Header, nil
}

func (c *ciClient) request(ctx context.Context, method string, path string, query url.Values, body any, header map[string]string) (*http.Response, error) {
	target := *c.baseURL
	target.Path = strings.TrimRight(target.Path, "/") + apiPathPrefix + path
	target.RawQuery = query.Encode()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode gitlab api request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("%w: build gitlab api request: %w", domain.ErrCodeSourceAuthorization, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("PRIVATE-TOKEN", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range header {
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: send gitlab api request: %w", domain.ErrCodeSourceAuthorization, err)
	}
	return resp, nil
}

// ciStatusError 把非 2xx 响应转成错误。403 通常是令牌缺少 api 权限或项目角色不够，单独给出提示。
func ciStatusError(resp *http.Response, action string) error {
	var response errorResponse
	_ = json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&response)
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: gitlab rejected access token", domain.ErrCodeSourceCredentialInvalid)
	case http.StatusForbidden:
		return fmt.Errorf("%w: gitlab status=403 action=%s：代码源令牌需要 api 权限，并且在项目上至少是 Developer 角色：%s", domain.ErrCodeSourceAuthorization, action, response.message())
	default:
		return fmt.Errorf("%w: gitlab status=%d action=%s message=%s", domain.ErrCodeSourceAuthorization, resp.StatusCode, action, response.message())
	}
}
