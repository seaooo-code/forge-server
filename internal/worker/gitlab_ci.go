package worker

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"forge-server/internal/domain"
	"forge-server/internal/service"
)

const (
	defaultCIPollInterval = 5 * time.Second
	// defaultCIArtifactMaxBytes 与 FORGE_WORKER_CI_ARTIFACT_MAX_BYTES 的默认值一致，测试等未配置上限的场景兜底。
	defaultCIArtifactMaxBytes int64 = 1 << 30
	// ciPollFailureLimit 是连续查询 pipeline 失败多少次后放弃，期间的暂时性错误（网络抖动、GitLab 升级）只记日志。
	ciPollFailureLimit = 5
	// ciCancelTimeout 是取消远端 pipeline 的超时，取消发生在构建上下文已结束之后，所以使用独立的 context。
	ciCancelTimeout = 15 * time.Second
	// maxCIArtifactFiles 是 artifacts 压缩包里文件数的上限。
	maxCIArtifactFiles = 50000
	// maxCITraceInterval 是运行中读取 job 日志的最大间隔。
	maxCITraceInterval = 30 * time.Second
	// maxCITraceDrainRounds 限制 pipeline 结束后补读日志的轮数，避免异常服务端导致死循环。
	maxCITraceDrainRounds = 200
	// maxCIMissingJobNames 是「找不到 job」错误里最多列出的 job 数。
	maxCIMissingJobNames = 10
)

// ciDependencies 是 FORGE_DEPS 变量的内容：worker 解析出的依赖构建，CI job 用它下载依赖产物。
type ciDependencies struct {
	ModuleID    string             `json:"moduleId"`
	Environment string             `json:"environment"`
	Items       []ciDependencyItem `json:"items"`
}

type ciDependencyItem struct {
	RuntimeVersion string   `json:"runtimeVersion"`
	BuildRecordID  string   `json:"buildRecordId"`
	BuildNumber    int64    `json:"buildNumber"`
	CommitSHA      string   `json:"commitSha"`
	Platforms      []string `json:"platforms"`
	// ManifestURL 是依赖构建产物清单的地址，BaseURL 是依赖构建所用存储的公开访问地址，清单里的 objectKey 拼在它后面就是文件地址。
	ManifestURL string `json:"manifestUrl"`
	BaseURL     string `json:"baseUrl"`
}

// executeGitLabCI 把构建转交给代码源的 CI：触发 pipeline、等待并转发 job 日志，成功后取回 job 的 artifacts，
// 再走与 docker 构建相同的校验、清单和上传。
func (e *Executor) executeGitLabCI(ctx context.Context, record domain.BuildRecord, logs *buildLogStream) (domain.BuildRecordCompletion, error) {
	if strings.TrimSpace(record.CIJob) == "" {
		return domain.BuildRecordCompletion{}, fmt.Errorf("模块没有配置 CI job：%w", domain.ErrInvalidBuildInput)
	}
	client, err := e.ci.CIClient(ctx, record.Repository)
	if err != nil {
		return domain.BuildRecordCompletion{}, fmt.Errorf("获取代码源 CI 客户端失败：%w", err)
	}

	// 无论是新建还是续接 pipeline，都要先注册密钥变量的脱敏规则：job 日志可能回显它们，续接时尤其容易漏掉。
	buildVariables, err := e.resolveCIBuildVariables(ctx, record, logs)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}

	pipeline, err := e.startCIPipeline(ctx, client, record, buildVariables, logs)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}
	if strings.TrimSpace(pipeline.SHA) == "" {
		return domain.BuildRecordCompletion{}, fmt.Errorf("GitLab 没有返回 pipeline #%d 使用的 commit", pipeline.ID)
	}
	if record.CommitSHA != "" && !strings.EqualFold(record.CommitSHA, pipeline.SHA) {
		e.cancelCIPipeline(client, pipeline.ID, logs)
		return domain.BuildRecordCompletion{}, fmt.Errorf("分支已前进：pipeline 使用 commit %s，构建指定的是 %s", pipeline.SHA, record.CommitSHA)
	}
	logs.Linef("pipeline #%d 使用 commit %s：%s", pipeline.ID, pipeline.SHA, pipeline.URL)

	job, err := e.waitCIPipeline(ctx, client, pipeline, record.CIJob, logs)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}

	workspace, cleanup, err := e.prepareBuildWorkspace(record)
	if err != nil {
		return domain.BuildRecordCompletion{}, err
	}
	defer cleanup()

	if err := e.fetchCIArtifacts(ctx, client, job, workspace, logs); err != nil {
		return domain.BuildRecordCompletion{}, err
	}
	artifactRoot, err := ciArtifactRoot(workspace.outputDir, record.CIArtifactPath)
	if err != nil {
		return domain.BuildRecordCompletion{}, fmt.Errorf("job %q 的 artifacts 里%w", record.CIJob, err)
	}
	return e.publishArtifact(ctx, record, artifactRoot, pipeline.SHA, logs)
}

// startCIPipeline 返回本次构建对应的 pipeline。构建记录里已有 pipeline（worker 被接管或退出后重新领取）时续接它，
// 不重复触发；否则创建新的 pipeline，并立即把 ID 写回构建记录。
func (e *Executor) startCIPipeline(ctx context.Context, client service.CIClient, record domain.BuildRecord, buildVariables []domain.ModuleBuildVariable, logs *buildLogStream) (domain.CIPipeline, error) {
	if record.CIPipelineID != 0 {
		pipeline, err := client.GetPipeline(ctx, record.CIPipelineID)
		switch {
		case err == nil:
			logs.Linef("续接已有的 GitLab pipeline #%d（状态 %s）", pipeline.ID, pipeline.Status)
			return pipeline, nil
		case errors.Is(err, domain.ErrCIPipelineNotFound):
			logs.Linef("原 pipeline #%d 已不存在，重新触发", record.CIPipelineID)
		default:
			return domain.CIPipeline{}, fmt.Errorf("查询已有 pipeline #%d 失败：%w", record.CIPipelineID, err)
		}
	}

	variables, err := e.ciVariables(ctx, record, buildVariables, logs)
	if err != nil {
		return domain.CIPipeline{}, err
	}
	branch := strings.TrimSpace(record.Branch)
	if branch == "" {
		branch = strings.TrimSpace(record.DefaultBranch)
	}
	if branch == "" {
		return domain.CIPipeline{}, fmt.Errorf("构建没有指定分支，模块也没有默认分支：%w", domain.ErrInvalidBuildInput)
	}
	pipeline, err := client.CreatePipeline(ctx, branch, variables)
	if err != nil {
		return domain.CIPipeline{}, fmt.Errorf("在分支 %s 上创建 GitLab pipeline 失败：%w", branch, err)
	}
	logs.Linef("已创建 GitLab pipeline #%d：%s", pipeline.ID, pipeline.URL)

	if err := e.ciPipelines.RecordBuildCIPipeline(ctx, record.ID, record.WorkerID, pipeline.ID, pipeline.URL); err != nil {
		// 没能写回构建记录：别的 worker 接管后不会知道这个 pipeline，取消它避免重复构建。
		e.cancelCIPipeline(client, pipeline.ID, logs)
		return domain.CIPipeline{}, fmt.Errorf("记录 pipeline #%d 失败：%w", pipeline.ID, err)
	}
	return pipeline, nil
}

// resolveCIBuildVariables 读取模块在当前环境下生效的构建变量，并把密钥变量的值加入日志脱敏。
func (e *Executor) resolveCIBuildVariables(ctx context.Context, record domain.BuildRecord, logs *buildLogStream) ([]domain.ModuleBuildVariable, error) {
	if e.variables == nil {
		return nil, nil
	}
	resolved, err := e.variables.ResolveBuildVariables(ctx, record.ModuleID, record.Environment)
	if err != nil {
		return nil, fmt.Errorf("读取构建变量失败：%w", err)
	}
	for _, variable := range resolved {
		if variable.IsSecret {
			logs.AddSecrets(variable.Value)
		}
	}
	return resolved, nil
}

// ciVariables 组装触发 pipeline 的变量：模块构建变量（含密钥）在前，平台固定变量在后，用户变量不能覆盖。
func (e *Executor) ciVariables(ctx context.Context, record domain.BuildRecord, buildVariables []domain.ModuleBuildVariable, logs *buildLogStream) ([]domain.CIVariable, error) {
	userVariables := append([]domain.ModuleBuildVariable(nil), buildVariables...)
	sort.SliceStable(userVariables, func(i, j int) bool { return userVariables[i].Key < userVariables[j].Key })

	variables := make([]domain.CIVariable, 0, len(userVariables)+8)
	for _, variable := range userVariables {
		if !domain.IsValidBuildVariableKey(variable.Key) || strings.HasPrefix(variable.Key, "FORGE_") {
			continue
		}
		variables = append(variables, domain.CIVariable{Key: variable.Key, Value: variable.Value})
	}

	buildTime := time.Now().UTC()
	if record.StartedAt != nil {
		buildTime = record.StartedAt.UTC()
	}
	branch := strings.TrimSpace(record.Branch)
	if branch == "" {
		branch = strings.TrimSpace(record.DefaultBranch)
	}
	variables = append(variables,
		domain.CIVariable{Key: "FORGE_BUILD_ID", Value: record.ID},
		domain.CIVariable{Key: "FORGE_BUILD_NUMBER", Value: strconv.FormatInt(record.Number, 10)},
		domain.CIVariable{Key: "FORGE_BUILD_TIME", Value: buildTime.Format(time.RFC3339)},
		domain.CIVariable{Key: "FORGE_MODULE_ID", Value: record.ModuleID},
		domain.CIVariable{Key: "FORGE_ENVIRONMENT", Value: record.Environment},
		domain.CIVariable{Key: "FORGE_BRANCH", Value: branch},
	)

	deps, ok, err := e.ciDependencyVariable(ctx, record, logs)
	if err != nil {
		return nil, err
	}
	if ok {
		variables = append(variables, domain.CIVariable{Key: "FORGE_DEPS", Value: deps})
	}
	return variables, nil
}

// ciDependencyVariable 解析 Feature 模块需要的 Common 依赖构建，生成 FORGE_DEPS 的 JSON，并把依赖写回构建记录。
// 依赖文件由 CI job 按其中的地址自行下载，worker 不下载。
func (e *Executor) ciDependencyVariable(ctx context.Context, record domain.BuildRecord, logs *buildLogStream) (string, bool, error) {
	if e.dependencies == nil {
		return "", false, nil
	}
	plan, ok, err := e.dependencies.ResolveBuildDependencies(ctx, record)
	if err != nil {
		return "", false, fmt.Errorf("解析依赖构建失败：%w", err)
	}
	if !ok {
		return "", false, nil
	}

	payload := ciDependencies{ModuleID: plan.ModuleID, Environment: plan.Environment, Items: []ciDependencyItem{}}
	dependencies := make([]domain.BuildDependency, 0, len(plan.Builds))
	if len(plan.Builds) == 0 {
		logs.Linef("警告：所属 Common 模块 %s 在环境 %s 下没有生效中的默认路由，FORGE_DEPS 的 items 为空", plan.ModuleID, plan.Environment)
	}
	for _, build := range plan.Builds {
		baseURL := strings.TrimRight(strings.TrimSpace(build.StoragePublicBaseURL), "/")
		manifestURL := artifactURL(build.StoragePublicBaseURL, build.ManifestKey)
		if baseURL == "" || manifestURL == "" {
			return "", false, fmt.Errorf("依赖构建 #%d 所用的存储配置没有公开访问地址，CI job 无法下载依赖产物", build.Number)
		}
		platforms := slices.Clone(build.Platforms)
		slices.Sort(platforms)
		payload.Items = append(payload.Items, ciDependencyItem{
			RuntimeVersion: build.RuntimeVersion,
			BuildRecordID:  build.ID,
			BuildNumber:    build.Number,
			CommitSHA:      build.CommitSHA,
			Platforms:      platforms,
			ManifestURL:    manifestURL,
			BaseURL:        baseURL,
		})
		dependencies = append(dependencies, domain.BuildDependency{ModuleID: plan.ModuleID, RuntimeVersion: build.RuntimeVersion, BuildRecordID: build.ID})
		logs.Linef("依赖：runtimeVersion %s，构建 #%d（commit %s），平台 %s", build.RuntimeVersion, build.Number, build.CommitSHA[:min(7, len(build.CommitSHA))], strings.Join(platforms, ","))
	}
	if err := e.dependencies.RecordBuildDependencies(ctx, record.ID, dependencies); err != nil {
		return "", false, fmt.Errorf("记录依赖构建失败：%w", err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", false, fmt.Errorf("编码 FORGE_DEPS 失败：%w", err)
	}
	return string(encoded), true, nil
}

// waitCIPipeline 轮询 pipeline 直到结束，期间把目标 job 的日志增量写入构建日志。pipeline 成功且目标 job 成功时返回该 job。
// 构建被取消时：超时会取消远端 pipeline；租约丢失和 worker 退出不取消，pipeline 留给下一个持有者续接。
func (e *Executor) waitCIPipeline(ctx context.Context, client service.CIClient, pipeline domain.CIPipeline, jobName string, logs *buildLogStream) (domain.CIJob, error) {
	interval := e.cfg.CIPollInterval
	if interval <= 0 {
		interval = defaultCIPollInterval
	}

	var traceOffset int64
	var lastTraceRead time.Time
	var formatter ciTraceFormatter
	failures := 0
	for {
		current, jobs, err := pollCIPipeline(ctx, client, pipeline.ID)
		switch {
		case err == nil:
			failures = 0
			target := findCIJob(jobs, jobName)
			// GitLab 的 job 日志接口会忽略 Range，每次都返回完整日志，日志越长读取越频繁代价越大，所以运行中按日志大小放慢读取；
			// pipeline 结束时一定读完。
			if target != nil && (current.Status.IsTerminal() || time.Since(lastTraceRead) >= ciTraceInterval(interval, traceOffset)) {
				traceOffset = e.streamCIJobTrace(ctx, client, target.ID, traceOffset, current.Status.IsTerminal(), &formatter, logs)
				lastTraceRead = time.Now()
			}
			if current.Status.IsTerminal() {
				return ciPipelineResult(current, jobs, jobName)
			}
		case ctx.Err() != nil:
			return domain.CIJob{}, e.ciInterrupted(ctx, client, pipeline, logs)
		default:
			failures++
			logs.Linef("查询 pipeline #%d 状态失败（%d/%d）：%v", pipeline.ID, failures, ciPollFailureLimit, err)
			if failures >= ciPollFailureLimit {
				return domain.CIJob{}, fmt.Errorf("连续 %d 次查询 pipeline #%d 失败：%w", failures, pipeline.ID, err)
			}
		}

		select {
		case <-ctx.Done():
			return domain.CIJob{}, e.ciInterrupted(ctx, client, pipeline, logs)
		case <-time.After(interval):
		}
	}
}

// ciTraceInterval 是读取 job 日志的最小间隔：基础轮询间隔加上每 256 KiB 已读日志多 1 秒，最多 30 秒。
func ciTraceInterval(base time.Duration, offset int64) time.Duration {
	return min(base+time.Duration(offset/(256<<10))*time.Second, maxCITraceInterval)
}

func pollCIPipeline(ctx context.Context, client service.CIClient, pipelineID int64) (domain.CIPipeline, []domain.CIJob, error) {
	pipeline, err := client.GetPipeline(ctx, pipelineID)
	if err != nil {
		return domain.CIPipeline{}, nil, err
	}
	jobs, err := client.ListPipelineJobs(ctx, pipelineID)
	if err != nil {
		return domain.CIPipeline{}, nil, err
	}
	return pipeline, jobs, nil
}

// streamCIJobTrace 把 job 日志从 offset 起的新增内容整理后写入构建日志，返回新的偏移。drain 为 true 时一直读到没有新增内容，
// 并输出最后不完整的一行。读日志失败只记警告，不影响构建结果。
func (e *Executor) streamCIJobTrace(ctx context.Context, client service.CIClient, jobID int64, offset int64, drain bool, formatter *ciTraceFormatter, logs *buildLogStream) int64 {
	for range maxCITraceDrainRounds {
		data, err := client.JobTrace(ctx, jobID, offset)
		if err != nil {
			if ctx.Err() == nil {
				logs.Linef("读取 job 日志失败：%v", err)
			}
			return offset
		}
		if len(data) == 0 {
			break
		}
		_, _ = logs.Write([]byte(formatter.Format(data)))
		offset += int64(len(data))
		if !drain {
			return offset
		}
	}
	if drain {
		_, _ = logs.Write([]byte(formatter.Flush()))
	}
	return offset
}

// ciInterrupted 处理构建上下文结束：只有超时才取消远端 pipeline，其余原因（租约丢失、worker 退出）都留给下一个持有者续接，
// 避免新旧持有者互相取消。
func (e *Executor) ciInterrupted(ctx context.Context, client service.CIClient, pipeline domain.CIPipeline, logs *buildLogStream) error {
	cause := context.Cause(ctx)
	if errors.Is(cause, context.DeadlineExceeded) {
		e.cancelCIPipeline(client, pipeline.ID, logs)
		return fmt.Errorf("构建超时，已取消 pipeline #%d：%w", pipeline.ID, cause)
	}
	return cause
}

// cancelCIPipeline 尽力取消远端 pipeline，失败只记日志。
func (e *Executor) cancelCIPipeline(client service.CIClient, pipelineID int64, logs *buildLogStream) {
	ctx, cancel := context.WithTimeout(context.Background(), ciCancelTimeout)
	defer cancel()
	if err := client.CancelPipeline(ctx, pipelineID); err != nil {
		logs.Linef("取消 pipeline #%d 失败：%v", pipelineID, err)
		return
	}
	logs.Linef("已取消 pipeline #%d", pipelineID)
}

func findCIJob(jobs []domain.CIJob, name string) *domain.CIJob {
	for i := range jobs {
		if jobs[i].Name == name {
			return &jobs[i]
		}
	}
	return nil
}

// ciPipelineResult 把已结束的 pipeline 转成结果：只有 pipeline 成功并且目标 job 成功才算成功。
func ciPipelineResult(pipeline domain.CIPipeline, jobs []domain.CIJob, jobName string) (domain.CIJob, error) {
	switch pipeline.Status {
	case domain.CIStatusSuccess:
		target := findCIJob(jobs, jobName)
		if target == nil {
			names := make([]string, 0, len(jobs))
			for _, job := range jobs {
				if len(names) == maxCIMissingJobNames {
					names = append(names, "…")
					break
				}
				names = append(names, job.Name)
			}
			return domain.CIJob{}, fmt.Errorf("pipeline #%d 成功，但没有找到 job %q，请检查模块配置的 job 名与 .gitlab-ci.yml 一致（pipeline 里的 job：%s）", pipeline.ID, jobName, strings.Join(names, "、"))
		}
		if target.Status != domain.CIStatusSuccess {
			return domain.CIJob{}, fmt.Errorf("pipeline #%d 成功，但 job %q 的状态是 %s：%s", pipeline.ID, jobName, target.Status, pipeline.URL)
		}
		return *target, nil
	case domain.CIStatusFailed:
		failed := []string{}
		for _, job := range jobs {
			if job.Status == domain.CIStatusFailed {
				failed = append(failed, job.Name)
			}
		}
		return domain.CIJob{}, fmt.Errorf("pipeline #%d 失败（失败的 job：%s）：%s", pipeline.ID, strings.Join(failed, "、"), pipeline.URL)
	case domain.CIStatusCanceled:
		return domain.CIJob{}, fmt.Errorf("pipeline #%d 已被取消：%s", pipeline.ID, pipeline.URL)
	default:
		return domain.CIJob{}, fmt.Errorf("pipeline #%d 没有运行任何 job（状态 %s），请检查 .gitlab-ci.yml 的 rules 是否匹配 FORGE_BUILD_ID：%s", pipeline.ID, pipeline.Status, pipeline.URL)
	}
}

// fetchCIArtifacts 下载目标 job 的 artifacts 并解压到构建输出目录。
func (e *Executor) fetchCIArtifacts(ctx context.Context, client service.CIClient, job domain.CIJob, workspace buildWorkspace, logs *buildLogStream) error {
	maxBytes := e.cfg.CIArtifactMaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultCIArtifactMaxBytes
	}

	zipPath := filepath.Join(workspace.rootDir, "ci-artifacts.zip")
	file, err := os.OpenFile(zipPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("创建 artifacts 临时文件失败：%w", err)
	}
	downloadErr := client.DownloadJobArtifacts(ctx, job.ID, file, maxBytes)
	closeErr := file.Close()
	switch {
	case errors.Is(downloadErr, domain.ErrCIArtifactsNotFound):
		return fmt.Errorf("job %q 没有 artifacts：请在 .gitlab-ci.yml 里为它配置 artifacts.paths，并确认还没有过期：%s", job.Name, job.URL)
	case downloadErr != nil:
		return fmt.Errorf("下载 job %q 的 artifacts 失败：%w", job.Name, downloadErr)
	case closeErr != nil:
		return fmt.Errorf("写入 artifacts 临时文件失败：%w", closeErr)
	}

	logs.Linef("已下载 job %q 的 artifacts，开始解压", job.Name)
	if err := extractArtifactsZip(zipPath, workspace.outputDir, maxBytes); err != nil {
		return fmt.Errorf("解压 job %q 的 artifacts 失败：%w", job.Name, err)
	}
	return nil
}

// ciArtifactRoot 返回 artifacts 解压目录里的产物根目录，ciArtifactPath 为空时用默认的 dist。
func ciArtifactRoot(outputDir string, ciArtifactPath string) (string, error) {
	artifactPath := strings.TrimSpace(ciArtifactPath)
	if artifactPath == "" {
		artifactPath = domain.DefaultModuleCIArtifactPath
	}
	root := outputDir
	if path.Clean(artifactPath) != "." {
		var err error
		root, err = artifactLocalPath(outputDir, artifactPath)
		if err != nil {
			return "", fmt.Errorf("产物路径 %q 无效", artifactPath)
		}
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("没有产物目录 %s", artifactPath)
	}
	return root, nil
}

// extractArtifactsZip 把 artifacts 压缩包解压到 dest。拒绝越界路径、软链、重复文件，并限制文件数和解压后的总大小。
func extractArtifactsZip(zipPath string, dest string, maxBytes int64) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开压缩包失败：%w", err)
	}
	defer reader.Close()

	if len(reader.File) > maxCIArtifactFiles {
		return fmt.Errorf("压缩包里有 %d 个条目，超过上限 %d", len(reader.File), maxCIArtifactFiles)
	}

	var total int64
	for _, entry := range reader.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if name == "" || name == "." {
			continue
		}
		target, err := artifactLocalPath(dest, name)
		if err != nil {
			return fmt.Errorf("压缩包条目 %q 的路径无效", entry.Name)
		}
		mode := entry.Mode()
		switch {
		case mode&os.ModeSymlink != 0:
			return fmt.Errorf("压缩包条目 %q 是软链，不支持", entry.Name)
		case entry.FileInfo().IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("创建目录 %q 失败：%w", entry.Name, err)
			}
			continue
		case !mode.IsRegular():
			return fmt.Errorf("压缩包条目 %q 不是普通文件", entry.Name)
		}

		if entry.UncompressedSize64 > uint64(maxBytes-total) {
			return fmt.Errorf("解压后的总大小超过上限 %d 字节", maxBytes)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("创建目录失败：%w", err)
		}
		written, err := extractArtifactFile(entry, target, maxBytes-total)
		if err != nil {
			return fmt.Errorf("解压 %q 失败：%w", entry.Name, err)
		}
		total += written
	}
	return nil
}

func extractArtifactFile(entry *zip.File, target string, remaining int64) (int64, error) {
	source, err := entry.Open()
	if err != nil {
		return 0, err
	}
	defer source.Close()

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(out, io.LimitReader(source, remaining+1))
	closeErr := out.Close()
	switch {
	case copyErr != nil:
		return written, copyErr
	case closeErr != nil:
		return written, closeErr
	case written > remaining:
		return written, fmt.Errorf("解压后的总大小超过上限")
	}
	return written, nil
}
