package worker

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeArtifactFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", name, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

func TestValidateArtifactContractAcceptsPlatforms(t *testing.T) {
	root := writeArtifactFiles(t, map[string]string{
		"forge.json":               `{"runtimeVersion": "1.2.0-725"}`,
		"ios/main.jsbundle":        "ios",
		"android/assets/a.bundle":  "android",
		"android/assets/img/a.png": "png",
	})

	manifest, platforms, err := validateArtifactContract(root)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if manifest.RuntimeVersion != "1.2.0-725" || !slices.Equal(platforms, []string{"android", "ios"}) {
		t.Fatalf("unexpected result: %+v %v", manifest, platforms)
	}
}

func TestValidateArtifactContractRejectsInvalidArtifacts(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{name: "missing forge.json", files: map[string]string{"ios/a": "x"}, want: "缺少 forge.json"},
		{name: "no platform", files: map[string]string{"forge.json": `{"runtimeVersion":"1"}`}, want: "至少需要一个平台目录"},
		{name: "unknown platform", files: map[string]string{"forge.json": `{"runtimeVersion":"1"}`, "web/a": "x"}, want: `"web" 不是合法的平台目录`},
		{name: "extra root file", files: map[string]string{"forge.json": `{"runtimeVersion":"1"}`, "ios/a": "x", "index.bundle": "x"}, want: `发现 "index.bundle"`},
		{name: "empty platform", files: map[string]string{"forge.json": `{"runtimeVersion":"1"}`, "ios/": ""}, want: "没有任何文件"},
		{name: "forge.json directory", files: map[string]string{"forge.json/": "", "ios/a": "x"}, want: "必须是普通文件"},
		{name: "invalid json", files: map[string]string{"forge.json": `{"runtimeVersion":`, "ios/a": "x"}, want: "必须是 JSON 对象"},
		{name: "json array", files: map[string]string{"forge.json": `["1"]`, "ios/a": "x"}, want: "必须是 JSON 对象"},
		{name: "json null", files: map[string]string{"forge.json": `null`, "ios/a": "x"}, want: "必须是 JSON 对象"},
		{name: "trailing content", files: map[string]string{"forge.json": `{"runtimeVersion":"1"} {}`, "ios/a": "x"}, want: "多余内容"},
		{name: "unknown field", files: map[string]string{"forge.json": `{"runtimeVersion":"1","entry":"index"}`, "ios/a": "x"}, want: `未知字段 "entry"`},
		{name: "case variant field", files: map[string]string{"forge.json": `{"RuntimeVersion":"1"}`, "ios/a": "x"}, want: `未知字段 "RuntimeVersion"`},
		{name: "missing runtimeVersion", files: map[string]string{"forge.json": `{}`, "ios/a": "x"}, want: "缺少 runtimeVersion"},
		{name: "non-string runtimeVersion", files: map[string]string{"forge.json": `{"runtimeVersion":725}`, "ios/a": "x"}, want: "必须是字符串"},
		{name: "empty runtimeVersion", files: map[string]string{"forge.json": `{"runtimeVersion":""}`, "ios/a": "x"}, want: "不合法"},
		{name: "invalid runtimeVersion chars", files: map[string]string{"forge.json": `{"runtimeVersion":"1.0/725"}`, "ios/a": "x"}, want: "不合法"},
		{name: "too long runtimeVersion", files: map[string]string{"forge.json": `{"runtimeVersion":"` + strings.Repeat("a", 65) + `"}`, "ios/a": "x"}, want: "不合法"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeArtifactFiles(t, tt.files)
			_, _, err := validateArtifactContract(root)
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.HasPrefix(err.Error(), "产物校验失败") {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestValidateArtifactContractRejectsRootSymlink(t *testing.T) {
	root := writeArtifactFiles(t, map[string]string{"forge.json": `{"runtimeVersion":"1"}`, "ios/a": "x"})
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "android")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, _, err := validateArtifactContract(root); err == nil {
		t.Fatal("expected symlink platform to be rejected")
	}
}
