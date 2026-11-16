package worker

import (
	"context"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"forge-server/internal/domain"
)

type recordedCommand struct {
	dir  string
	args []string
	env  []string
}

// fakeGitRunner 模拟 git：fetch 可配置为失败，rev-parse 返回指定 HEAD。
type fakeGitRunner struct {
	commands  []recordedCommand
	fetchErr  error
	head      string
	failOnArg string
}

func (r *fakeGitRunner) run(_ context.Context, cmd command) (string, error) {
	r.commands = append(r.commands, recordedCommand{dir: cmd.dir, args: append([]string{cmd.name}, cmd.args...), env: cmd.env})
	if r.failOnArg != "" && len(cmd.args) > 0 && cmd.args[0] == r.failOnArg {
		return "", errors.New(r.failOnArg + " failed")
	}
	if len(cmd.args) > 0 && cmd.args[0] == "fetch" && r.fetchErr != nil {
		return "", r.fetchErr
	}
	if len(cmd.args) > 0 && cmd.args[0] == "rev-parse" {
		return r.head, nil
	}
	return "", nil
}

func (r *fakeGitRunner) subcommands() []string {
	values := make([]string, 0, len(r.commands))
	for _, cmd := range r.commands {
		values = append(values, cmd.args[1])
	}
	return values
}

func checkoutTestRecord(commitSHA string) domain.BuildRecord {
	return domain.BuildRecord{
		ID:         "build-1",
		Branch:     "feature/login",
		CommitSHA:  commitSHA,
		Repository: domain.ModuleRepository{ConnectionID: "conn-1", CloneURL: "https://gitlab.example.com/rn/app.git"},
	}
}

const testCommitSHA = "0123456789abcdef0123456789abcdef01234567"

func TestCheckoutRepositoryFetchesCommitSHA(t *testing.T) {
	runner := &fakeGitRunner{head: testCommitSHA}
	executor := &Executor{runner: runner.run}
	repoDir := t.TempDir() + "/repo"

	got, err := executor.checkoutRepository(context.Background(), checkoutTestRecord(strings.ToUpper(testCommitSHA)), domain.GitCredentials{Username: "oauth2", Password: "glpat-secret"}, repoDir, io.Discard)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if got != testCommitSHA {
		t.Fatalf("expected commit %s, got %s", testCommitSHA, got)
	}
	want := [][]string{
		{"git", "init", "--quiet", repoDir},
		{"git", "remote", "add", "origin", "https://gitlab.example.com/rn/app.git"},
		{"git", "fetch", "--depth", "1", "origin", testCommitSHA},
		{"git", "checkout", "--detach", "FETCH_HEAD"},
		{"git", "rev-parse", "HEAD"},
	}
	for index, cmd := range runner.commands {
		if !reflect.DeepEqual(cmd.args, want[index]) {
			t.Fatalf("command %d: expected %#v, got %#v", index, want[index], cmd.args)
		}
	}
	if len(runner.commands) != len(want) {
		t.Fatalf("expected %d commands, got %v", len(want), runner.subcommands())
	}
	for _, cmd := range runner.commands {
		if strings.Contains(strings.Join(cmd.args, " "), "glpat-secret") {
			t.Fatalf("credential leaked into args: %#v", cmd.args)
		}
		if !slices.Contains(cmd.env, "GIT_PASSWORD=glpat-secret") || !slices.Contains(cmd.env, "GIT_TERMINAL_PROMPT=0") {
			t.Fatalf("expected credentials via env, got %#v", cmd.env)
		}
	}
}

func TestCheckoutRepositoryFallsBackToBranchClone(t *testing.T) {
	runner := &fakeGitRunner{fetchErr: errors.New("server does not allow request for unadvertised object"), head: testCommitSHA}
	executor := &Executor{runner: runner.run}
	repoDir := t.TempDir() + "/repo"

	got, err := executor.checkoutRepository(context.Background(), checkoutTestRecord(testCommitSHA), domain.GitCredentials{}, repoDir, io.Discard)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if got != testCommitSHA {
		t.Fatalf("expected commit %s, got %s", testCommitSHA, got)
	}
	if want := []string{"init", "remote", "fetch", "clone", "rev-parse"}; !reflect.DeepEqual(runner.subcommands(), want) {
		t.Fatalf("expected %v, got %v", want, runner.subcommands())
	}
	clone := runner.commands[3].args
	if want := []string{"git", "clone", "--depth", "1", "--branch", "feature/login", "--", "https://gitlab.example.com/rn/app.git", repoDir}; !reflect.DeepEqual(clone, want) {
		t.Fatalf("expected clone %#v, got %#v", want, clone)
	}
}

func TestCheckoutRepositoryFailsWhenBranchMoved(t *testing.T) {
	runner := &fakeGitRunner{fetchErr: errors.New("fetch failed"), head: "fedcba9876543210fedcba9876543210fedcba98"}
	executor := &Executor{runner: runner.run}

	_, err := executor.checkoutRepository(context.Background(), checkoutTestRecord(testCommitSHA), domain.GitCredentials{}, t.TempDir()+"/repo", io.Discard)
	if !errors.Is(err, errBranchMoved) || err.Error() != "分支已更新，请重新触发" {
		t.Fatalf("expected branch moved error, got %v", err)
	}
}

func TestCheckoutRepositoryWithoutCommitReadsHead(t *testing.T) {
	runner := &fakeGitRunner{head: strings.ToUpper(testCommitSHA)}
	executor := &Executor{runner: runner.run}

	got, err := executor.checkoutRepository(context.Background(), checkoutTestRecord(""), domain.GitCredentials{}, t.TempDir()+"/repo", io.Discard)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if got != testCommitSHA {
		t.Fatalf("expected head %s, got %s", testCommitSHA, got)
	}
	if want := []string{"clone", "rev-parse"}; !reflect.DeepEqual(runner.subcommands(), want) {
		t.Fatalf("expected %v, got %v", want, runner.subcommands())
	}
}

func TestCheckoutRepositoryDoesNotFallBackWhenCheckoutFails(t *testing.T) {
	runner := &fakeGitRunner{head: testCommitSHA, failOnArg: "checkout"}
	executor := &Executor{runner: runner.run}

	if _, err := executor.checkoutRepository(context.Background(), checkoutTestRecord(testCommitSHA), domain.GitCredentials{}, t.TempDir()+"/repo", io.Discard); err == nil {
		t.Fatal("expected checkout error")
	}
	if want := []string{"init", "remote", "fetch", "checkout"}; !reflect.DeepEqual(runner.subcommands(), want) {
		t.Fatalf("expected %v, got %v", want, runner.subcommands())
	}
}

func TestCheckoutRepositoryRejectsUnsafeInput(t *testing.T) {
	for name, record := range map[string]domain.BuildRecord{
		"option injection sha": checkoutTestRecord("--upload-pack=touch /tmp/pwned"),
		"non hex sha":          checkoutTestRecord("main"),
		"option clone url": func() domain.BuildRecord {
			record := checkoutTestRecord("")
			record.Repository.CloneURL = "--upload-pack=x"
			return record
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeGitRunner{}
			executor := &Executor{runner: runner.run}
			if _, err := executor.checkoutRepository(context.Background(), record, domain.GitCredentials{}, t.TempDir()+"/repo", io.Discard); err == nil {
				t.Fatal("expected error")
			}
			if len(runner.commands) != 0 {
				t.Fatalf("expected no git command, got %v", runner.subcommands())
			}
		})
	}
}

func TestExecCommandStreamsOutputAndKeepsShortError(t *testing.T) {
	var log strings.Builder
	_, err := execCommand(context.Background(), command{
		name: "sh",
		args: []string{"-c", "echo out; echo err >&2; exit 3"},
		log:  &log,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(log.String(), "out") || !strings.Contains(log.String(), "err") {
		t.Fatalf("expected merged output in log, got %q", log.String())
	}
	if strings.Contains(err.Error(), "out") || !strings.Contains(err.Error(), "构建日志") {
		t.Fatalf("expected short error, got %v", err)
	}

	stdout, err := execCommand(context.Background(), command{name: "sh", args: []string{"-c", "echo abc; echo noise >&2"}, captureStdout: true})
	if err != nil || stdout != "abc" {
		t.Fatalf("expected captured stdout, got %q %v", stdout, err)
	}
}
