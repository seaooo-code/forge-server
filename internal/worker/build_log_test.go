package worker

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"forge-server/internal/domain"
)

type fakeBuildLogStore struct {
	mu      sync.Mutex
	nextSeq int
	chunks  []domain.BuildLogChunk
}

func (s *fakeBuildLogStore) AppendBuildLogChunk(_ context.Context, chunk domain.BuildLogChunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks = append(s.chunks, chunk)
	return nil
}

func (s *fakeBuildLogStore) NextBuildLogSeq(context.Context, string) (int, error) {
	return s.nextSeq, nil
}

func (s *fakeBuildLogStore) content() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var builder strings.Builder
	for _, chunk := range s.chunks {
		builder.WriteString(chunk.Content)
	}
	return builder.String()
}

func (s *fakeBuildLogStore) chunkCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.chunks)
}

func TestBuildLogStreamRedactsSecretsAcrossWrites(t *testing.T) {
	store := &fakeBuildLogStore{}
	stream := newBuildLogStream(context.Background(), store, nil, "build-1", buildLogOptions{maxBytes: 1 << 20})
	stream.AddSecrets("glpat-abcdef", "abc", "")

	for _, part := range []string{"token=glpat-ab", "cdef ok\n", "short abc and glpat-", "x end"} {
		if _, err := stream.Write([]byte(part)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	got := store.content()
	want := "token=[redacted] ok\nshort [redacted] and glpat-x end"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBuildLogStreamSplitsChunksOnSizeAndContinuesSeq(t *testing.T) {
	store := &fakeBuildLogStore{nextSeq: 7}
	stream := newBuildLogStream(context.Background(), store, nil, "build-1", buildLogOptions{chunkBytes: 10, maxBytes: 1 << 20})

	_, _ = stream.Write([]byte(strings.Repeat("a", 25)))
	if store.chunkCount() != 2 {
		t.Fatalf("expected two full chunks before close, got %d", store.chunkCount())
	}
	_ = stream.Close()

	if store.chunkCount() != 3 || store.content() != strings.Repeat("a", 25) {
		t.Fatalf("unexpected chunks: %+v", store.chunks)
	}
	for index, chunk := range store.chunks {
		if chunk.Seq != 7+index || chunk.BuildRecordID != "build-1" || len(chunk.Content) > 10 {
			t.Fatalf("unexpected chunk %d: %+v", index, chunk)
		}
	}
}

func TestBuildLogStreamFlushesOnInterval(t *testing.T) {
	store := &fakeBuildLogStore{}
	stream := newBuildLogStream(context.Background(), store, nil, "build-1", buildLogOptions{flushInterval: 10 * time.Millisecond, maxBytes: 1 << 20})
	defer stream.Close()

	_, _ = stream.Write([]byte("step 1\n"))
	deadline := time.Now().Add(2 * time.Second)
	for store.chunkCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if store.content() != "step 1\n" {
		t.Fatalf("expected interval flush, got %q", store.content())
	}
}

func TestBuildLogStreamTruncatesMiddle(t *testing.T) {
	store := &fakeBuildLogStore{}
	stream := newBuildLogStream(context.Background(), store, nil, "build-1", buildLogOptions{chunkBytes: 16, maxBytes: 40})

	_, _ = stream.Write([]byte(strings.Repeat("H", 20)))
	for index := 0; index < 10; index++ {
		_, _ = stream.Write([]byte(strings.Repeat("m", 10)))
	}
	_, _ = stream.Write([]byte(strings.Repeat("T", 20)))
	_ = stream.Close()

	got := store.content()
	if !strings.HasPrefix(got, strings.Repeat("H", 20)) || !strings.HasSuffix(got, strings.Repeat("T", 20)) {
		t.Fatalf("expected head and tail kept, got %q", got)
	}
	if !strings.Contains(got, "日志超过 40 字节上限，已省略中间 100 字节") || strings.Contains(got, "m") {
		t.Fatalf("expected truncation notice without middle, got %q", got)
	}
}

func TestBuildLogStreamKeepsShortLogWithoutNotice(t *testing.T) {
	store := &fakeBuildLogStore{}
	stream := newBuildLogStream(context.Background(), store, nil, "build-1", buildLogOptions{chunkBytes: 16, maxBytes: 40})
	_, _ = stream.Write([]byte(strings.Repeat("x", 35)))
	_ = stream.Close()

	if got := store.content(); got != strings.Repeat("x", 35) {
		t.Fatalf("expected full log, got %q", got)
	}
}

func TestBuildLogStreamKeepsChunksValidUTF8(t *testing.T) {
	store := &fakeBuildLogStore{}
	stream := newBuildLogStream(context.Background(), store, nil, "build-1", buildLogOptions{chunkBytes: 4, maxBytes: 1 << 20})
	_, _ = stream.Write([]byte("构建日志\x00ok\xff"))
	_ = stream.Close()

	for _, chunk := range store.chunks {
		if !utf8.ValidString(chunk.Content) || strings.Contains(chunk.Content, "\x00") {
			t.Fatalf("invalid chunk content %q", chunk.Content)
		}
	}
	if got := store.content(); got != "构建日志ok�" {
		t.Fatalf("unexpected content %q", got)
	}
}
