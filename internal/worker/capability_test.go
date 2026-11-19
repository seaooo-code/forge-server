package worker

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"forge-server/internal/domain"
)

type capabilityRunner struct {
	calls   []string
	gitErr  error
	dockerE error
	// dockerOutput 是 docker version 的输出，默认返回一个版本号。
	dockerOutput *string
}

func (r *capabilityRunner) run(_ context.Context, cmd command) (string, error) {
	r.calls = append(r.calls, cmd.name)
	switch cmd.name {
	case "git":
		return "", r.gitErr
	case "docker":
		if r.dockerE != nil {
			return "", r.dockerE
		}
		if r.dockerOutput != nil {
			return *r.dockerOutput, nil
		}
		return "27.3.1\n", nil
	}
	return "", errors.New("unexpected command " + cmd.name)
}

func TestDetectBuildModesKeepsAllModesWhenDockerWorks(t *testing.T) {
	runner := &capabilityRunner{}

	got := DetectBuildModes(context.Background(), domain.BuildModes, runner.run)
	if !slices.Equal(got.Enabled, domain.BuildModes) || len(got.Disabled) != 0 {
		t.Fatalf("expected every mode enabled, got %#v", got)
	}
	if !slices.Equal(runner.calls, []string{"git", "docker"}) {
		t.Fatalf("expected docker support to be probed once, got %v", runner.calls)
	}
}

func TestDetectBuildModesDisablesDockerModesWithoutDocker(t *testing.T) {
	runner := &capabilityRunner{dockerE: errors.New("docker version 执行失败（exec: \"docker\": executable file not found in $PATH）")}

	got := DetectBuildModes(context.Background(), domain.BuildModes, runner.run)
	if !slices.Equal(got.Enabled, []domain.BuildMode{domain.BuildModeGitLabCI}) {
		t.Fatalf("expected only gitlab_ci to stay enabled, got %v", got.Enabled)
	}
	for _, mode := range []domain.BuildMode{domain.BuildModeDockerfile, domain.BuildModeAdapter} {
		if reason := got.Disabled[mode]; !strings.Contains(reason, "docker") {
			t.Fatalf("expected a docker reason for %s, got %q", mode, reason)
		}
	}
}

func TestDetectBuildModesDisablesDockerModesWithoutGit(t *testing.T) {
	runner := &capabilityRunner{gitErr: errors.New("not found")}

	got := DetectBuildModes(context.Background(), domain.BuildModes, runner.run)
	if !slices.Equal(got.Enabled, []domain.BuildMode{domain.BuildModeGitLabCI}) || !strings.Contains(got.Disabled[domain.BuildModeDockerfile], "git") {
		t.Fatalf("expected docker modes disabled because git is missing, got %#v", got)
	}
	if slices.Contains(runner.calls, "docker") {
		t.Fatalf("docker must not be probed once git is known to be missing, got %v", runner.calls)
	}
}

func TestDetectBuildModesTreatsEmptyServerVersionAsUnreachable(t *testing.T) {
	empty := "  \n"
	runner := &capabilityRunner{dockerOutput: &empty}

	got := DetectBuildModes(context.Background(), []domain.BuildMode{domain.BuildModeDockerfile}, runner.run)
	if len(got.Enabled) != 0 || !strings.Contains(got.Disabled[domain.BuildModeDockerfile], "daemon") {
		t.Fatalf("expected an unreachable daemon to disable docker builds, got %#v", got)
	}
}

func TestDetectBuildModesSkipsProbeWithoutDockerModes(t *testing.T) {
	runner := &capabilityRunner{dockerE: errors.New("must not be called")}

	got := DetectBuildModes(context.Background(), []domain.BuildMode{domain.BuildModeGitLabCI, domain.BuildModeGitLabCI}, runner.run)
	if !slices.Equal(got.Enabled, []domain.BuildMode{domain.BuildModeGitLabCI}) || len(runner.calls) != 0 {
		t.Fatalf("expected no probes and one enabled mode, got %#v calls=%v", got, runner.calls)
	}
}

func TestDetectBuildModesReportsWhenNothingIsLeft(t *testing.T) {
	runner := &capabilityRunner{dockerE: errors.New("down")}

	got := DetectBuildModes(context.Background(), []domain.BuildMode{domain.BuildModeDockerfile, domain.BuildModeAdapter}, runner.run)
	if len(got.Enabled) != 0 || len(got.Disabled) != 2 {
		t.Fatalf("expected both docker modes disabled and none enabled, got %#v", got)
	}
}
