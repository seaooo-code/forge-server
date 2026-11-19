package service

import (
	"context"
	"io"

	"forge-server/internal/domain"
)

// CIClient 是绑定了代码源连接和项目的 CI 客户端，worker 用它把构建转交给代码源的 CI。
type CIClient interface {
	// CreatePipeline 在 ref（分支）上创建 pipeline 并传入变量。
	CreatePipeline(ctx context.Context, ref string, variables []domain.CIVariable) (domain.CIPipeline, error)
	// GetPipeline 返回 pipeline 的最新状态；pipeline 不存在时返回 domain.ErrCIPipelineNotFound。
	GetPipeline(ctx context.Context, pipelineID int64) (domain.CIPipeline, error)
	// ListPipelineJobs 返回 pipeline 的 job，同名 job 重试后只保留最新一次。
	ListPipelineJobs(ctx context.Context, pipelineID int64) ([]domain.CIJob, error)
	// JobTrace 返回 job 日志从 offset 字节开始的新增内容，没有新增内容时返回空。
	JobTrace(ctx context.Context, jobID int64, offset int64) ([]byte, error)
	// DownloadJobArtifacts 把 job 的 artifacts 压缩包写入 dst，超过 maxBytes 时返回错误。
	DownloadJobArtifacts(ctx context.Context, jobID int64, dst io.Writer, maxBytes int64) error
	// CancelPipeline 取消 pipeline；pipeline 已结束时不报错。
	CancelPipeline(ctx context.Context, pipelineID int64) error
}

// CIProvider 是代码源适配器的可选能力：支持把构建转交给代码源的 CI。
type CIProvider interface {
	CIClient(ctx context.Context, conn domain.CodeSourceConnection, repo domain.ModuleRepository) (CIClient, error)
}

// CIClient 供 worker 获取仓库所在代码源的 CI 客户端；代码源不支持 CI 时返回 domain.ErrCINotSupported。
func (s *CodeSourceService) CIClient(ctx context.Context, repository domain.ModuleRepository) (CIClient, error) {
	conn, provider, err := s.openConnection(ctx, repository.ConnectionID, true)
	if err != nil {
		return nil, err
	}
	ciProvider, ok := provider.(CIProvider)
	if !ok {
		return nil, domain.ErrCINotSupported
	}
	return ciProvider.CIClient(ctx, conn, repository)
}
