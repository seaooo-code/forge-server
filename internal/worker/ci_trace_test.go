package worker

import (
	"testing"
	"time"
)

// 样例取自 GitLab 19.x 的真实 job 日志。
const ciTraceSample = "2026-11-19T04:50:29.736488Z 00O Running with gitlab-runner 19.5.3 (33227fd4)\n" +
	"2026-11-19T04:50:29.736534Z 00O section_start:1795063829:prepare_executor\r\x1b[0K\n" +
	"2026-11-19T04:50:29.736536Z 00O+\x1b[0K\x1b[36;1mPreparing the \"shell\" executor\x1b[0;m\x1b[0;m\n" +
	"2026-11-19T04:50:29.736618Z 00O section_end:1795063829:prepare_executor\x1b[0K\n" +
	"2026-11-19T04:50:29.876686Z 01O \x1b[32;1m$ echo hi\x1b[0;m\n" +
	"2026-11-19T04:50:29.876688Z 01O hi\n" +
	"2026-11-19T04:50:29.876690Z 01E warning: something\n"

func TestCITraceFormatterStripsTimestampsSectionsAndColors(t *testing.T) {
	var formatter ciTraceFormatter
	got := formatter.Format([]byte(ciTraceSample))
	want := "Running with gitlab-runner 19.5.3 (33227fd4)\nPreparing the \"shell\" executor\n$ echo hi\nhi\nwarning: something\n"
	if got != want {
		t.Fatalf("unexpected formatted trace:\n%q\nwant\n%q", got, want)
	}
}

func TestCITraceFormatterKeepsPartialLinesUntilComplete(t *testing.T) {
	var formatter ciTraceFormatter
	first := formatter.Format([]byte("2026-11-19T04:50:29.876688Z 01O hel"))
	second := formatter.Format([]byte("lo\n2026-11-19T04:50:30.000000Z 01O wor"))
	if first != "" || second != "hello\n" {
		t.Fatalf("unexpected output: %q %q", first, second)
	}
	if rest := formatter.Flush(); rest != "wor\n" {
		t.Fatalf("expected the unfinished line to be flushed, got %q", rest)
	}
	if again := formatter.Flush(); again != "" {
		t.Fatalf("flush must be idempotent, got %q", again)
	}
}

func TestCITraceFormatterLeavesPlainLogsUntouched(t *testing.T) {
	var formatter ciTraceFormatter
	got := formatter.Format([]byte("yarn install\nbuild done\n"))
	if got != "yarn install\nbuild done\n" {
		t.Fatalf("expected a plain log to pass through, got %q", got)
	}
}

func TestCITraceFormatterKeepsIntentionalBlankLines(t *testing.T) {
	var formatter ciTraceFormatter
	if got := formatter.Format([]byte("a\n\nb\n")); got != "a\n\nb\n" {
		t.Fatalf("expected blank lines to be kept, got %q", got)
	}
}

func TestCITraceIntervalSlowsDownAsTheLogGrows(t *testing.T) {
	base := 5 * time.Second
	for _, tt := range []struct {
		offset int64
		want   time.Duration
	}{
		{0, 5 * time.Second},
		{256<<10 - 1, 5 * time.Second},
		{1 << 20, 9 * time.Second},
		{100 << 20, 30 * time.Second},
	} {
		if got := ciTraceInterval(base, tt.offset); got != tt.want {
			t.Fatalf("offset %d: got %v, want %v", tt.offset, got, tt.want)
		}
	}
}
