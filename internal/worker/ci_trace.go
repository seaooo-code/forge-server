package worker

import (
	"regexp"
	"strings"
)

var (
	// GitLab 新版日志每行带时间戳和流标记，例如「2026-11-19T04:49:47.158951Z 01O 」，「+」表示接着上一行。
	ciTracePrefixPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T[\d:.]+Z [0-9a-fA-F]{2}[OE]\+? ?`)
	// runner 用 section_start / section_end 标记折叠区，控制台里没有折叠，直接去掉。
	ciTraceSectionPattern = regexp.MustCompile(`section_(?:start|end):\d+:\S*`)
	ciTraceANSIPattern    = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
)

// ciTraceFormatter 把 GitLab job 日志整理成适合构建日志展示的纯文本：去掉时间戳前缀、折叠区标记和 ANSI 控制码。
// 日志按字节增量读取，一次读到的内容可能停在一行中间，所以只处理完整的行，剩余部分留到下一次。
// 日志本身没有这些装饰时（旧版 GitLab）原样输出。
type ciTraceFormatter struct {
	pending string
}

// Format 接收新增的日志内容，返回可以写入构建日志的完整行（含换行）。
func (f *ciTraceFormatter) Format(data []byte) string {
	f.pending += string(data)
	index := strings.LastIndexByte(f.pending, '\n')
	if index < 0 {
		return ""
	}
	complete := f.pending[:index+1]
	f.pending = f.pending[index+1:]
	return formatCITraceLines(complete)
}

// Flush 输出剩下的不完整行，job 结束后调用。
func (f *ciTraceFormatter) Flush() string {
	rest := f.pending
	f.pending = ""
	if rest == "" {
		return ""
	}
	return formatCITraceLines(rest + "\n")
}

func formatCITraceLines(text string) string {
	var builder strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		line = ciTracePrefixPattern.ReplaceAllString(line, "")
		hadSection := ciTraceSectionPattern.MatchString(line)
		line = ciTraceSectionPattern.ReplaceAllString(line, "")
		line = ciTraceANSIPattern.ReplaceAllString(line, "")
		line = strings.ReplaceAll(line, "\r", "")
		// 整行只有折叠区标记时丢弃，不留下空行。
		if hadSection && strings.TrimSpace(line) == "" {
			continue
		}
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	return builder.String()
}
