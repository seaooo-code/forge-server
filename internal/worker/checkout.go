package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"forge-server/internal/domain"
)

// errBranchMoved 是按 SHA 拉取失败、回退 clone 分支后发现 HEAD 已不是目标 commit 时的失败原因。
var errBranchMoved = errors.New("分支已更新，请重新触发")

var commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// command 描述一次外部命令调用。log 接收合并后的 stdout/stderr，为 nil 时丢弃输出；captureStdout 时额外返回 stdout。
type command struct {
	dir           string
	env           []string
	name          string
	args          []string
	log           io.Writer
	captureStdout bool
}

type commandRunner func(ctx context.Context, cmd command) (string, error)

// execCommand 是默认的命令执行器。完整输出只进入构建日志，返回的错误只保留简短原因，写入 build_records.error_message。
func execCommand(ctx context.Context, c command) (string, error) {
	cmd := exec.CommandContext(ctx, c.name, c.args...)
	cmd.Dir = c.dir
	if len(c.env) > 0 {
		cmd.Env = append(os.Environ(), c.env...)
	}
	// docker 等子进程可能继承管道导致 Wait 一直不返回，超时取消后最多再等 10 秒。
	cmd.WaitDelay = 10 * time.Second

	logWriter := c.log
	if logWriter == nil {
		logWriter = io.Discard
	}
	var stdout bytes.Buffer
	cmd.Stdout = logWriter
	if c.captureStdout {
		cmd.Stdout = io.MultiWriter(logWriter, &stdout)
	}
	cmd.Stderr = logWriter

	if err := cmd.Run(); err != nil {
		label := c.name
		if len(c.args) > 0 {
			label += " " + c.args[0]
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("%s 被中止：%w", label, ctxErr)
		}
		return "", fmt.Errorf("%s 执行失败（%v），完整输出见构建日志", label, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// runCommand 先把命令行写入日志再执行。命令行里只有 clone 地址、build-arg 等非敏感值，密钥通过环境变量传递。
func (e *Executor) runCommand(ctx context.Context, cmd command) (string, error) {
	if cmd.log != nil {
		_, _ = fmt.Fprintf(cmd.log, "$ %s %s\n", cmd.name, strings.Join(cmd.args, " "))
	}
	runner := e.runner
	if runner == nil {
		runner = execCommand
	}
	return runner(ctx, cmd)
}

// checkoutRepository 检出代码并返回实际构建的完整 commit SHA。
// 有 commit_sha 时按 SHA 浅拉取；托管方不支持时回退到 clone 分支并校验 HEAD。没有 commit_sha 时 clone 分支后读取 HEAD。
// 凭证只通过 GIT_ASKPASS 和环境变量传入，不出现在命令行参数或 URL 中。
func (e *Executor) checkoutRepository(ctx context.Context, record domain.BuildRecord, credentials domain.GitCredentials, repoDir string, logs io.Writer) (string, error) {
	cloneURL := strings.TrimSpace(record.Repository.CloneURL)
	if cloneURL == "" || strings.HasPrefix(cloneURL, "-") {
		return "", domain.ErrInvalidBuildInput
	}
	commitSHA := strings.ToLower(strings.TrimSpace(record.CommitSHA))
	if commitSHA != "" && !commitSHAPattern.MatchString(commitSHA) {
		return "", fmt.Errorf("commit_sha %q 格式不合法", record.CommitSHA)
	}

	askpassPath, err := writeGitAskPass(filepath.Dir(repoDir))
	if err != nil {
		return "", err
	}
	env := []string{
		"GIT_ASKPASS=" + askpassPath,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_USERNAME=" + credentials.Username,
		"GIT_PASSWORD=" + credentials.Password,
	}
	git := func(dir string, captureStdout bool, args ...string) (string, error) {
		return e.runCommand(ctx, command{dir: dir, env: env, name: "git", args: args, log: logs, captureStdout: captureStdout})
	}
	cloneBranchHead := func() (string, error) {
		if _, err := git("", false, gitCloneArgs(record.Branch, cloneURL, repoDir)...); err != nil {
			return "", err
		}
		head, err := git(repoDir, true, gitRevParseHeadArgs()...)
		return strings.ToLower(head), err
	}

	if commitSHA == "" {
		return cloneBranchHead()
	}

	if _, err := git("", false, gitInitArgs(repoDir)...); err != nil {
		return "", err
	}
	if _, err := git(repoDir, false, gitRemoteAddArgs(cloneURL)...); err != nil {
		return "", err
	}
	if _, fetchErr := git(repoDir, false, gitFetchCommitArgs(commitSHA)...); fetchErr == nil {
		if _, err := git(repoDir, false, gitCheckoutFetchHeadArgs()...); err != nil {
			return "", err
		}
		head, err := git(repoDir, true, gitRevParseHeadArgs()...)
		if err != nil {
			return "", err
		}
		head = strings.ToLower(head)
		if !strings.HasPrefix(head, commitSHA) {
			return "", fmt.Errorf("检出的 commit %s 与构建记录 %s 不一致", head, commitSHA)
		}
		return head, nil
	}

	_, _ = fmt.Fprintf(logs, "[forge] 托管方不支持按 commit 拉取，改为 clone 分支 %s 并校验 HEAD\n", record.Branch)
	if err := os.RemoveAll(repoDir); err != nil {
		return "", fmt.Errorf("reset repo dir: %w", err)
	}
	head, err := cloneBranchHead()
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(head, commitSHA) {
		_, _ = fmt.Fprintf(logs, "[forge] 分支 %s 当前 HEAD 为 %s，构建记录要求 %s\n", record.Branch, head, commitSHA)
		return "", errBranchMoved
	}
	return head, nil
}

func gitCloneArgs(branch string, cloneURL string, repoDir string) []string {
	args := []string{"clone", "--depth", "1"}
	if strings.TrimSpace(branch) != "" {
		args = append(args, "--branch", strings.TrimSpace(branch))
	}
	return append(args, "--", cloneURL, repoDir)
}

func gitInitArgs(repoDir string) []string {
	return []string{"init", "--quiet", repoDir}
}

func gitRemoteAddArgs(cloneURL string) []string {
	return []string{"remote", "add", "origin", cloneURL}
}

func gitFetchCommitArgs(commitSHA string) []string {
	return []string{"fetch", "--depth", "1", "origin", commitSHA}
}

func gitCheckoutFetchHeadArgs() []string {
	return []string{"checkout", "--detach", "FETCH_HEAD"}
}

func gitRevParseHeadArgs() []string {
	return []string{"rev-parse", "HEAD"}
}

func writeGitAskPass(dir string) (string, error) {
	path := filepath.Join(dir, "git-askpass.sh")
	script := `#!/bin/sh
case "$1" in
*Username*) printf '%s\n' "$GIT_USERNAME" ;;
*) printf '%s\n' "$GIT_PASSWORD" ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return "", fmt.Errorf("write git askpass helper: %w", err)
	}
	return path, nil
}
