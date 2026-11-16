package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"text/template/parse"
	"unicode/utf8"

	"forge-server/internal/domain"
)

// 构建适配器渲染器放在 service 层：API（保存校验、渲染预览）和 worker（实际构建）都要用同一套规则，
// worker 已依赖 service；渲染只用标准库，不依赖 HTTP 或数据库，不需要为此新建目录。

const (
	buildAdapterTemplateName   = "tpl"
	buildAdapterDockerfileName = "Dockerfile"
	// maxBuildAdapterRangeDepth 限制 range 嵌套层数：不产生输出的嵌套循环不受输出上限约束，
	// 参数最多 50 个，3 层最多 12.5 万次迭代。
	maxBuildAdapterRangeDepth = 3
)

// buildAdapterTemplateFuncs 是开放给模板的函数白名单，都是纯字符串处理，不读文件、环境变量或网络。
var buildAdapterTemplateFuncs = template.FuncMap{
	// default 用法：{{ .Params.x | default "fallback" }}，值为空时返回 fallback。
	"default": func(fallback any, value any) any {
		if value == nil || fmt.Sprint(value) == "" {
			return fallback
		}
		return value
	},
	// quote 输出 JSON 双引号字符串，可直接用于 Dockerfile 的 JSON 数组形式。
	"quote": func(value any) (string, error) {
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(fmt.Sprint(value)); err != nil {
			return "", err
		}
		return strings.TrimSuffix(buffer.String(), "\n"), nil
	},
	// join 用法：{{ join "," .Params.a .Params.b }}。
	"join": func(separator string, values ...any) string {
		items := make([]string, 0, len(values))
		for _, value := range values {
			if list, ok := value.([]string); ok {
				items = append(items, list...)
				continue
			}
			items = append(items, fmt.Sprint(value))
		}
		return strings.Join(items, separator)
	},
	"lower": strings.ToLower,
	"upper": strings.ToUpper,
	"trim":  strings.TrimSpace,
	// replace 用法：{{ .Params.x | replace "old" "new" }}。
	"replace": func(old string, replacement string, value string) string {
		return strings.ReplaceAll(value, old, replacement)
	},
}

// buildAdapterAllowedBuiltins 是允许使用的 text/template 内置函数：只做逻辑与比较，结果有界。
// call、index、slice、len、print*、html、js、urlquery 等其余内置函数一律拒绝。
var buildAdapterAllowedBuiltins = map[string]bool{
	"and": true, "or": true, "not": true,
	"eq": true, "ne": true, "lt": true, "le": true, "gt": true, "ge": true,
}

var (
	templateErrorPattern    = regexp.MustCompile(`^template: ` + buildAdapterTemplateName + `:(\d+)(?::\d+)?: (.*)$`)
	templateLocationPattern = regexp.MustCompile(`^` + buildAdapterTemplateName + `:(\d+)(?::\d+)?`)
)

// ValidateBuildAdapterDefinition 规范化并校验适配器定义：名称、大小、辅助文件路径、参数定义，并预解析全部模板。
// 模板错误返回 *domain.BuildAdapterRenderError（带行号），其余返回 domain.ErrInvalidBuildAdapterInput。
func ValidateBuildAdapterDefinition(input domain.BuildAdapterInput) (domain.BuildAdapterInput, error) {
	normalized := domain.BuildAdapterInput{
		Name:               strings.TrimSpace(input.Name),
		Description:        strings.TrimSpace(input.Description),
		DockerfileTemplate: input.DockerfileTemplate,
		Files:              map[string]string{},
		Parameters:         []domain.BuildAdapterParameter{},
		UserID:             strings.TrimSpace(input.UserID),
	}
	if normalized.Name == "" || utf8.RuneCountInString(normalized.Name) > domain.MaxBuildAdapterNameLength ||
		utf8.RuneCountInString(normalized.Description) > domain.MaxBuildAdapterDescriptionLength {
		return domain.BuildAdapterInput{}, fmt.Errorf("%w: 名称不能为空且不超过 %d 字，描述不超过 %d 字", domain.ErrInvalidBuildAdapterInput, domain.MaxBuildAdapterNameLength, domain.MaxBuildAdapterDescriptionLength)
	}
	if strings.TrimSpace(normalized.DockerfileTemplate) == "" {
		return domain.BuildAdapterInput{}, fmt.Errorf("%w: Dockerfile 模板不能为空", domain.ErrInvalidBuildAdapterInput)
	}
	if len(normalized.DockerfileTemplate) > domain.MaxBuildAdapterTemplateBytes {
		return domain.BuildAdapterInput{}, fmt.Errorf("%w: Dockerfile 模板超过 %d 字节", domain.ErrInvalidBuildAdapterInput, domain.MaxBuildAdapterTemplateBytes)
	}
	if len(input.Files) > domain.MaxBuildAdapterFiles {
		return domain.BuildAdapterInput{}, fmt.Errorf("%w: 辅助文件不能超过 %d 个", domain.ErrInvalidBuildAdapterInput, domain.MaxBuildAdapterFiles)
	}
	for filePath, content := range input.Files {
		if !domain.IsValidBuildAdapterFilePath(filePath) {
			return domain.BuildAdapterInput{}, fmt.Errorf("%w: 辅助文件路径 %q 必须是不含 .. 的相对路径", domain.ErrInvalidBuildAdapterInput, filePath)
		}
		if len(content) > domain.MaxBuildAdapterFileBytes {
			return domain.BuildAdapterInput{}, fmt.Errorf("%w: 辅助文件 %s 超过 %d 字节", domain.ErrInvalidBuildAdapterInput, filePath, domain.MaxBuildAdapterFileBytes)
		}
		normalized.Files[filePath] = content
	}
	// 不同路径不能互为父子目录，例如同时存在 a 和 a/b。
	for filePath := range normalized.Files {
		for other := range normalized.Files {
			if strings.HasPrefix(other, filePath+"/") {
				return domain.BuildAdapterInput{}, fmt.Errorf("%w: 辅助文件 %s 与 %s 路径冲突", domain.ErrInvalidBuildAdapterInput, filePath, other)
			}
		}
	}

	if len(input.Parameters) > domain.MaxBuildAdapterParameters {
		return domain.BuildAdapterInput{}, fmt.Errorf("%w: 参数不能超过 %d 个", domain.ErrInvalidBuildAdapterInput, domain.MaxBuildAdapterParameters)
	}
	seen := map[string]bool{}
	for _, parameter := range input.Parameters {
		parameter = domain.BuildAdapterParameter{
			Key:         strings.TrimSpace(parameter.Key),
			Label:       strings.TrimSpace(parameter.Label),
			Description: strings.TrimSpace(parameter.Description),
			Required:    parameter.Required,
			Default:     strings.TrimSpace(parameter.Default),
		}
		if !domain.IsValidBuildAdapterParameterKey(parameter.Key) {
			return domain.BuildAdapterInput{}, fmt.Errorf("%w: 参数 key %q 必须匹配 ^[a-zA-Z][a-zA-Z0-9_]{0,63}$", domain.ErrInvalidBuildAdapterInput, parameter.Key)
		}
		if seen[parameter.Key] {
			return domain.BuildAdapterInput{}, fmt.Errorf("%w: 参数 key %s 重复", domain.ErrInvalidBuildAdapterInput, parameter.Key)
		}
		seen[parameter.Key] = true
		if parameter.Label == "" {
			parameter.Label = parameter.Key
		}
		if err := validateBuildAdapterParamValue(parameter.Key, parameter.Default); err != nil {
			return domain.BuildAdapterInput{}, err
		}
		normalized.Parameters = append(normalized.Parameters, parameter)
	}

	if _, err := parseBuildAdapterTemplate(buildAdapterDockerfileName, normalized.DockerfileTemplate); err != nil {
		return domain.BuildAdapterInput{}, err
	}
	for _, filePath := range sortedKeys(normalized.Files) {
		if _, err := parseBuildAdapterTemplate(filePath, normalized.Files[filePath]); err != nil {
			return domain.BuildAdapterInput{}, err
		}
	}
	return normalized, nil
}

// ResolveBuildAdapterParams 把模块填写的参数与适配器参数定义合并：没填时取默认值，required 参数最终必须有值。
// allowUndeclared 为 false 时（保存模块、预览）未声明的参数报错；为 true 时（worker 构建）忽略，
// 避免适配器删除某个参数后，所有填过该参数的模块都无法构建。
func ResolveBuildAdapterParams(parameters []domain.BuildAdapterParameter, values map[string]string, allowUndeclared bool) (map[string]string, error) {
	declared := make(map[string]bool, len(parameters))
	for _, parameter := range parameters {
		declared[parameter.Key] = true
	}
	for key := range values {
		if !domain.IsValidBuildAdapterParameterKey(key) {
			return nil, fmt.Errorf("%w: 参数 key %q 必须匹配 ^[a-zA-Z][a-zA-Z0-9_]{0,63}$", domain.ErrInvalidBuildAdapterInput, key)
		}
		if !declared[key] && !allowUndeclared {
			return nil, fmt.Errorf("%w: 参数 %s 未在适配器中声明", domain.ErrInvalidBuildAdapterInput, key)
		}
	}

	resolved := make(map[string]string, len(parameters))
	for _, parameter := range parameters {
		value := strings.TrimSpace(values[parameter.Key])
		if value == "" {
			value = parameter.Default
		}
		if err := validateBuildAdapterParamValue(parameter.Key, value); err != nil {
			return nil, err
		}
		if parameter.Required && value == "" {
			return nil, fmt.Errorf("%w: 必填参数 %s 没有值", domain.ErrInvalidBuildAdapterInput, parameter.Key)
		}
		resolved[parameter.Key] = value
	}
	return resolved, nil
}

// NormalizeBuildAdapterParamValues 只保留非空参数值并去掉首尾空白，用于保存模块参数。
func NormalizeBuildAdapterParamValues(values map[string]string) map[string]string {
	normalized := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value != "" {
			normalized[key] = value
		}
	}
	return normalized
}

// RenderBuildAdapter 渲染 Dockerfile 与全部辅助文件。context.Params 应先经过 ResolveBuildAdapterParams。
func RenderBuildAdapter(adapter domain.BuildAdapter, context domain.BuildAdapterRenderContext) (domain.BuildAdapterRenderResult, error) {
	if len(adapter.DockerfileTemplate) > domain.MaxBuildAdapterTemplateBytes || len(adapter.Files) > domain.MaxBuildAdapterFiles {
		return domain.BuildAdapterRenderResult{}, fmt.Errorf("%w: 适配器超过大小限制", domain.ErrInvalidBuildAdapterInput)
	}
	if context.Params == nil {
		context.Params = map[string]string{}
	}

	dockerfile, err := renderBuildAdapterTemplate(buildAdapterDockerfileName, adapter.DockerfileTemplate, context)
	if err != nil {
		return domain.BuildAdapterRenderResult{}, err
	}
	result := domain.BuildAdapterRenderResult{
		Dockerfile: dockerfile,
		Files:      make(map[string]string, len(adapter.Files)),
	}
	for _, filePath := range sortedKeys(adapter.Files) {
		content := adapter.Files[filePath]
		if !domain.IsValidBuildAdapterFilePath(filePath) {
			return domain.BuildAdapterRenderResult{}, fmt.Errorf("%w: 辅助文件路径 %q 必须是不含 .. 的相对路径", domain.ErrInvalidBuildAdapterInput, filePath)
		}
		if len(content) > domain.MaxBuildAdapterFileBytes {
			return domain.BuildAdapterRenderResult{}, fmt.Errorf("%w: 辅助文件 %s 超过 %d 字节", domain.ErrInvalidBuildAdapterInput, filePath, domain.MaxBuildAdapterFileBytes)
		}
		rendered, err := renderBuildAdapterTemplate(filePath, content, context)
		if err != nil {
			return domain.BuildAdapterRenderResult{}, err
		}
		result.Files[filePath] = rendered
	}
	sum := sha256.Sum256([]byte(dockerfile))
	result.DockerfileSHA256 = hex.EncodeToString(sum[:])
	return result, nil
}

func renderBuildAdapterTemplate(name string, content string, context domain.BuildAdapterRenderContext) (string, error) {
	tmpl, err := parseBuildAdapterTemplate(name, content)
	if err != nil {
		return "", err
	}
	output := &limitedBuffer{limit: domain.MaxBuildAdapterRenderedBytes}
	if err := tmpl.Execute(output, context); err != nil {
		if errors.Is(err, errRenderedOutputTooLarge) {
			return "", &domain.BuildAdapterRenderError{Template: name, Message: err.Error()}
		}
		return "", newBuildAdapterRenderError(name, err)
	}
	return output.String(), nil
}

// parseBuildAdapterTemplate 解析模板并做静态检查：只允许白名单函数，禁止 define/template/block，
// range 只能遍历上下文字段或变量，避免递归和超大循环。
func parseBuildAdapterTemplate(name string, content string) (*template.Template, error) {
	tmpl, err := template.New(buildAdapterTemplateName).
		Option("missingkey=error").
		Funcs(buildAdapterTemplateFuncs).
		Parse(content)
	if err != nil {
		return nil, newBuildAdapterRenderError(name, err)
	}
	if len(tmpl.Templates()) > 1 {
		return nil, &domain.BuildAdapterRenderError{Template: name, Message: "不支持 define、block 定义子模板"}
	}
	if tmpl.Tree == nil || tmpl.Tree.Root == nil {
		return tmpl, nil
	}
	if err := checkBuildAdapterTemplateNode(tmpl.Tree, tmpl.Tree.Root, 0); err != nil {
		return nil, &domain.BuildAdapterRenderError{Template: name, Line: err.line, Message: err.message}
	}
	return tmpl, nil
}

type templateCheckError struct {
	line    int
	message string
}

func checkBuildAdapterTemplateNode(tree *parse.Tree, node parse.Node, rangeDepth int) *templateCheckError {
	fail := func(node parse.Node, message string) *templateCheckError {
		location, _ := tree.ErrorContext(node)
		line := 0
		if match := templateLocationPattern.FindStringSubmatch(location); match != nil {
			line, _ = strconv.Atoi(match[1])
		}
		return &templateCheckError{line: line, message: message}
	}

	switch typed := node.(type) {
	case *parse.ListNode:
		if typed == nil {
			return nil
		}
		for _, child := range typed.Nodes {
			if err := checkBuildAdapterTemplateNode(tree, child, rangeDepth); err != nil {
				return err
			}
		}
	case *parse.ActionNode:
		return checkBuildAdapterTemplateNode(tree, typed.Pipe, rangeDepth)
	case *parse.PipeNode:
		if typed == nil {
			return nil
		}
		for _, command := range typed.Cmds {
			if err := checkBuildAdapterTemplateNode(tree, command, rangeDepth); err != nil {
				return err
			}
		}
	case *parse.CommandNode:
		for _, arg := range typed.Args {
			if err := checkBuildAdapterTemplateNode(tree, arg, rangeDepth); err != nil {
				return err
			}
		}
	case *parse.ChainNode:
		return checkBuildAdapterTemplateNode(tree, typed.Node, rangeDepth)
	case *parse.IdentifierNode:
		if _, ok := buildAdapterTemplateFuncs[typed.Ident]; !ok && !buildAdapterAllowedBuiltins[typed.Ident] {
			return fail(typed, fmt.Sprintf("函数 %q 不在白名单内，只能使用 default、quote、join、lower、upper、trim、replace 以及 and、or、not、eq、ne、lt、le、gt、ge", typed.Ident))
		}
	case *parse.IfNode:
		return checkBuildAdapterBranch(tree, &typed.BranchNode, rangeDepth)
	case *parse.WithNode:
		return checkBuildAdapterBranch(tree, &typed.BranchNode, rangeDepth)
	case *parse.RangeNode:
		pipe := typed.Pipe
		if pipe == nil || len(pipe.Cmds) != 1 || len(pipe.Cmds[0].Args) != 1 {
			return fail(typed, "range 只能遍历上下文字段或变量")
		}
		switch pipe.Cmds[0].Args[0].(type) {
		case *parse.FieldNode, *parse.VariableNode, *parse.DotNode, *parse.ChainNode:
		default:
			return fail(typed, "range 只能遍历上下文字段或变量")
		}
		if rangeDepth+1 > maxBuildAdapterRangeDepth {
			return fail(typed, fmt.Sprintf("range 最多嵌套 %d 层", maxBuildAdapterRangeDepth))
		}
		return checkBuildAdapterBranch(tree, &typed.BranchNode, rangeDepth+1)
	case *parse.TemplateNode:
		return fail(typed, "不支持 template、block 引用子模板")
	}
	return nil
}

func checkBuildAdapterBranch(tree *parse.Tree, branch *parse.BranchNode, rangeDepth int) *templateCheckError {
	if err := checkBuildAdapterTemplateNode(tree, branch.Pipe, rangeDepth); err != nil {
		return err
	}
	if err := checkBuildAdapterTemplateNode(tree, branch.List, rangeDepth); err != nil {
		return err
	}
	if branch.ElseList != nil {
		return checkBuildAdapterTemplateNode(tree, branch.ElseList, rangeDepth)
	}
	return nil
}

// newBuildAdapterRenderError 从 text/template 的错误文本里提取行号，例如
// `template: tpl:3:12: executing "tpl" at <.Params.x>: map has no entry for key "x"`。
func newBuildAdapterRenderError(name string, err error) *domain.BuildAdapterRenderError {
	message := err.Error()
	match := templateErrorPattern.FindStringSubmatch(message)
	if match == nil {
		return &domain.BuildAdapterRenderError{Template: name, Message: message}
	}
	line, _ := strconv.Atoi(match[1])
	detail := strings.TrimPrefix(match[2], `executing "`+buildAdapterTemplateName+`" `)
	return &domain.BuildAdapterRenderError{Template: name, Line: line, Message: detail}
}

func validateBuildAdapterParamValue(key string, value string) error {
	if len(value) > domain.MaxBuildAdapterParamValueBytes {
		return fmt.Errorf("%w: 参数 %s 超过 %d 字节", domain.ErrInvalidBuildAdapterInput, key, domain.MaxBuildAdapterParamValueBytes)
	}
	// 参数值不允许换行等控制字符，避免模块通过参数向渲染后的 Dockerfile 注入额外指令。
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return fmt.Errorf("%w: 参数 %s 不能包含换行或控制字符", domain.ErrInvalidBuildAdapterInput, key)
		}
	}
	return nil
}

var errRenderedOutputTooLarge = fmt.Errorf("渲染结果超过 %d 字节", domain.MaxBuildAdapterRenderedBytes)

// limitedBuffer 在输出超过上限时返回错误，text/template 会立即停止执行。
type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errRenderedOutputTooLarge
	}
	return b.Buffer.Write(p)
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
