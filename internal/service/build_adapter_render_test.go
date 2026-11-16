package service

import (
	"errors"
	"strings"
	"testing"

	"forge-server/internal/domain"
)

func sampleRenderContext(params map[string]string) domain.BuildAdapterRenderContext {
	return domain.BuildAdapterRenderContext{
		Module:      domain.BuildAdapterRenderModule{ID: "demo-module", Name: "Demo Module"},
		Environment: "qa",
		Branch:      "main",
		CommitSHA:   "0123456789abcdef0123456789abcdef01234567",
		Build:       domain.BuildAdapterRenderBuild{ID: "build-1", Number: 7, Time: "2026-01-02T03:04:05Z"},
		Params:      params,
	}
}

func TestRenderBuildAdapterSuccess(t *testing.T) {
	adapter := domain.BuildAdapter{
		DockerfileTemplate: "FROM node:20 AS bundle\n" +
			"ARG ENTRY={{ .Params.entry | default \"index.js\" }}\n" +
			"RUN [\"node\", \".forge/build.js\", {{ quote .Params.entry }}, {{ .Build.Number }}]\n" +
			"{{ if eq .Environment \"qa\" }}ENV MODE={{ upper .Environment }}{{ end }}\n" +
			"LABEL commit={{ .CommitSHA }} module={{ .Module.ID | lower }} name={{ .Module.Name | replace \" \" \"-\" | lower }}\n" +
			"LABEL tags={{ join \",\" .Branch .Build.Time }} trimmed={{ trim \"  x \" }}\n",
		Files: map[string]string{
			"build.js":         "console.log({{ quote .Params.entry }})\n",
			"config/env.json":  `{"env": {{ quote .Environment }}, "build": {{ quote .Build.ID }}}`,
			"config/empty.txt": "",
		},
	}

	result, err := RenderBuildAdapter(adapter, sampleRenderContext(map[string]string{"entry": `src/main "x".js`}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		`ARG ENTRY=src/main "x".js`,
		`RUN ["node", ".forge/build.js", "src/main \"x\".js", 7]`,
		"ENV MODE=QA",
		"module=demo-module name=demo-module",
		"tags=main,2026-01-02T03:04:05Z trimmed=x",
	} {
		if !strings.Contains(result.Dockerfile, want) {
			t.Fatalf("rendered Dockerfile missing %q:\n%s", want, result.Dockerfile)
		}
	}
	if result.Files["config/env.json"] != `{"env": "qa", "build": "build-1"}` || result.Files["config/empty.txt"] != "" {
		t.Fatalf("unexpected rendered files: %#v", result.Files)
	}
	if len(result.DockerfileSHA256) != 64 {
		t.Fatalf("expected sha256 hex, got %q", result.DockerfileSHA256)
	}
}

func TestRenderBuildAdapterMissingKeyReportsLine(t *testing.T) {
	adapter := domain.BuildAdapter{DockerfileTemplate: "FROM node\n\nRUN echo {{ .Params.missing }}\n"}
	_, err := RenderBuildAdapter(adapter, sampleRenderContext(map[string]string{}))
	var renderErr *domain.BuildAdapterRenderError
	if !errors.As(err, &renderErr) {
		t.Fatalf("expected render error, got %v", err)
	}
	if renderErr.Template != "Dockerfile" || renderErr.Line != 3 || !strings.Contains(renderErr.Message, `"missing"`) {
		t.Fatalf("unexpected render error: %#v", renderErr)
	}

	// 结构体上不存在的字段同样报错，并带辅助文件路径与行号。
	adapter = domain.BuildAdapter{DockerfileTemplate: "FROM node\n", Files: map[string]string{"a.sh": "#!/bin/sh\n{{ .Module.Secret }}\n"}}
	_, err = RenderBuildAdapter(adapter, sampleRenderContext(nil))
	if !errors.As(err, &renderErr) || renderErr.Template != "a.sh" || renderErr.Line != 2 {
		t.Fatalf("expected a.sh line 2 error, got %#v", err)
	}
}

func TestRenderBuildAdapterRejectsFunctionsOutsideWhitelist(t *testing.T) {
	cases := map[string]string{
		"undefined env": "FROM node\nRUN {{ env \"HOME\" }}\n",
		"printf":        "FROM node\n\nRUN {{ printf \"%s\" .Branch }}\n",
		"call":          "FROM node\nRUN {{ call .Branch }}\n",
		"index":         "FROM node\nRUN {{ index .Params \"a\" }}\n",
		"len":           "FROM node\nRUN {{ len .Branch }}\n",
		"define":        "{{ define \"x\" }}y{{ end }}FROM node\n",
		"template":      "FROM node\n{{ template \"tpl\" . }}\n",
		"range number":  "FROM node\n{{ range 100000000 }}x{{ end }}\n",
		"range nesting": "FROM node\n{{ range .Params }}{{ range $.Params }}{{ range $.Params }}{{ range $.Params }}{{ end }}{{ end }}{{ end }}{{ end }}\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateBuildAdapterDefinition(domain.BuildAdapterInput{Name: "demo", DockerfileTemplate: content})
			var renderErr *domain.BuildAdapterRenderError
			if !errors.As(err, &renderErr) || renderErr.Line == 0 && name != "define" {
				t.Fatalf("expected render error with line, got %#v", err)
			}
			if _, err := RenderBuildAdapter(domain.BuildAdapter{DockerfileTemplate: content}, sampleRenderContext(nil)); err == nil {
				t.Fatal("expected render to fail")
			}
		})
	}

	var renderErr *domain.BuildAdapterRenderError
	_, err := ValidateBuildAdapterDefinition(domain.BuildAdapterInput{Name: "demo", DockerfileTemplate: "FROM node\n\nRUN {{ printf \"%s\" .Branch }}\n"})
	if !errors.As(err, &renderErr) || renderErr.Line != 3 || !strings.Contains(renderErr.Message, "printf") {
		t.Fatalf("expected printf rejected at line 3, got %#v", err)
	}
}

func TestRenderBuildAdapterAllowsRangeOverParams(t *testing.T) {
	adapter := domain.BuildAdapter{DockerfileTemplate: "FROM node\n{{ range $key, $value := .Params }}ENV P_{{ upper $key }}={{ $value }}\n{{ end }}"}
	result, err := RenderBuildAdapter(adapter, sampleRenderContext(map[string]string{"a": "1", "b": "2"}))
	if err != nil || !strings.Contains(result.Dockerfile, "ENV P_A=1\nENV P_B=2\n") {
		t.Fatalf("unexpected result %q, %v", result.Dockerfile, err)
	}
}

func TestResolveBuildAdapterParams(t *testing.T) {
	parameters := []domain.BuildAdapterParameter{
		{Key: "entry", Required: true},
		{Key: "configPath", Default: "config/app.json"},
		{Key: "flavor"},
	}

	resolved, err := ResolveBuildAdapterParams(parameters, map[string]string{"entry": " index.js "}, false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved["entry"] != "index.js" || resolved["configPath"] != "config/app.json" || resolved["flavor"] != "" {
		t.Fatalf("unexpected resolved params: %#v", resolved)
	}

	for name, values := range map[string]map[string]string{
		"missing required": {"configPath": "x"},
		"undeclared":       {"entry": "a", "other": "b"},
		"invalid key":      {"entry": "a", "1bad": "b"},
		"newline value":    {"entry": "a\nRUN rm -rf /"},
	} {
		if _, err := ResolveBuildAdapterParams(parameters, values, false); !errors.Is(err, domain.ErrInvalidBuildAdapterInput) {
			t.Fatalf("%s: expected invalid input, got %v", name, err)
		}
	}

	// worker 构建时忽略适配器已删除的参数，但 required 仍然必须有值。
	resolved, err = ResolveBuildAdapterParams(parameters, map[string]string{"entry": "a", "removed": "b"}, true)
	if err != nil || resolved["removed"] != "" {
		t.Fatalf("expected undeclared param ignored, got %#v %v", resolved, err)
	}
	if _, err := ResolveBuildAdapterParams(parameters, map[string]string{}, true); !errors.Is(err, domain.ErrInvalidBuildAdapterInput) {
		t.Fatalf("expected missing required to fail, got %v", err)
	}
}

func TestValidateBuildAdapterDefinitionParameters(t *testing.T) {
	base := domain.BuildAdapterInput{Name: "demo", DockerfileTemplate: "FROM node\n"}
	for name, parameters := range map[string][]domain.BuildAdapterParameter{
		"invalid key":    {{Key: "bad-key"}},
		"leading digit":  {{Key: "1key"}},
		"too long key":   {{Key: "a" + strings.Repeat("b", 64)}},
		"duplicated key": {{Key: "entry"}, {Key: "entry"}},
	} {
		input := base
		input.Parameters = parameters
		if _, err := ValidateBuildAdapterDefinition(input); !errors.Is(err, domain.ErrInvalidBuildAdapterInput) {
			t.Fatalf("%s: expected invalid input, got %v", name, err)
		}
	}

	input := base
	input.Parameters = []domain.BuildAdapterParameter{{Key: "entry_File1", Required: true}}
	normalized, err := ValidateBuildAdapterDefinition(input)
	if err != nil || normalized.Parameters[0].Label != "entry_File1" {
		t.Fatalf("expected valid parameter with default label, got %#v %v", normalized.Parameters, err)
	}
}

func TestValidateBuildAdapterDefinitionFilePaths(t *testing.T) {
	for _, filePath := range []string{"", "/abs.sh", "../escape.sh", "a/../../b", "a/../b", "./a", "a//b", "a\\b", ".", "a/", "a\nb"} {
		input := domain.BuildAdapterInput{Name: "demo", DockerfileTemplate: "FROM node\n", Files: map[string]string{filePath: "x"}}
		if _, err := ValidateBuildAdapterDefinition(input); !errors.Is(err, domain.ErrInvalidBuildAdapterInput) {
			t.Fatalf("path %q: expected invalid input, got %v", filePath, err)
		}
	}

	input := domain.BuildAdapterInput{Name: "demo", DockerfileTemplate: "FROM node\n", Files: map[string]string{"scripts": "x", "scripts/build.sh": "y"}}
	if _, err := ValidateBuildAdapterDefinition(input); !errors.Is(err, domain.ErrInvalidBuildAdapterInput) {
		t.Fatalf("expected parent/child path conflict, got %v", err)
	}

	input.Files = map[string]string{"scripts/build.sh": "y", "config/a.json": "{}"}
	if _, err := ValidateBuildAdapterDefinition(input); err != nil {
		t.Fatalf("expected valid nested paths, got %v", err)
	}
}

func TestValidateBuildAdapterDefinitionSizeLimits(t *testing.T) {
	tooLarge := strings.Repeat("a", domain.MaxBuildAdapterTemplateBytes+1)
	if _, err := ValidateBuildAdapterDefinition(domain.BuildAdapterInput{Name: "demo", DockerfileTemplate: tooLarge}); !errors.Is(err, domain.ErrInvalidBuildAdapterInput) {
		t.Fatalf("expected template size error, got %v", err)
	}

	input := domain.BuildAdapterInput{Name: "demo", DockerfileTemplate: "FROM node\n", Files: map[string]string{"big.txt": strings.Repeat("a", domain.MaxBuildAdapterFileBytes+1)}}
	if _, err := ValidateBuildAdapterDefinition(input); !errors.Is(err, domain.ErrInvalidBuildAdapterInput) {
		t.Fatalf("expected file size error, got %v", err)
	}

	files := map[string]string{}
	for index := 0; index <= domain.MaxBuildAdapterFiles; index++ {
		files[strings.Repeat("f", index+1)] = "x"
	}
	input.Files = files
	if _, err := ValidateBuildAdapterDefinition(input); !errors.Is(err, domain.ErrInvalidBuildAdapterInput) {
		t.Fatalf("expected file count error, got %v", err)
	}

	// 渲染结果也有上限：range 嵌套放大输出时中止。
	adapter := domain.BuildAdapter{DockerfileTemplate: "{{ range .Params }}{{ range $.Params }}" + strings.Repeat("x", 60000) + "{{ end }}{{ end }}"}
	params := map[string]string{}
	for index := 0; index < 10; index++ {
		params["p"+strings.Repeat("a", index)] = "v"
	}
	_, err := RenderBuildAdapter(adapter, sampleRenderContext(params))
	var renderErr *domain.BuildAdapterRenderError
	if !errors.As(err, &renderErr) || !strings.Contains(renderErr.Message, "超过") {
		t.Fatalf("expected rendered size error, got %v", err)
	}
}
