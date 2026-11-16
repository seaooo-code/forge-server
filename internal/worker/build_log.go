package worker

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"forge-server/internal/domain"
)

// BuildLogStore 持久化构建日志分片，由 service.BuildLogService 实现。
type BuildLogStore interface {
	AppendBuildLogChunk(ctx context.Context, chunk domain.BuildLogChunk) error
	NextBuildLogSeq(ctx context.Context, buildID string) (int, error)
}

const (
	buildLogChunkBytes    = 64 * 1024
	buildLogFlushInterval = time.Second
	buildLogTailBytes     = 1024 * 1024
	buildLogWriteTimeout  = 10 * time.Second
	buildLogRedaction     = "[redacted]"
)

// buildLogStream 把 git、docker 的 stdout/stderr 合并成一路日志：先对整条流做脱敏，再按 64KB 或 1 秒切成分片写库。
// 超过上限时保留开头和最后一段，中间插入截断提示；尾部在 Close 时才写入。写库失败只记录告警，不影响构建结果。
type buildLogStream struct {
	mu      sync.Mutex
	ctx     context.Context
	store   BuildLogStore
	logger  *slog.Logger
	buildID string
	seq     int

	chunkBytes int
	secrets    [][]byte
	firstBytes [256]bool
	// held 是可能构成密钥前缀、还不能判断是否需要替换的原始字节。
	held []byte
	// pending 是已经脱敏、等待写成分片的字节。
	pending []byte

	headLimit int64
	headBytes int64
	tail      []byte
	tailLimit int
	dropped   int64

	closed   bool
	storeErr error
	stop     chan struct{}
	done     chan struct{}
}

type buildLogOptions struct {
	chunkBytes    int
	flushInterval time.Duration
	maxBytes      int64
}

func newBuildLogStream(ctx context.Context, store BuildLogStore, logger *slog.Logger, buildID string, opts buildLogOptions) *buildLogStream {
	if opts.chunkBytes <= 0 {
		opts.chunkBytes = buildLogChunkBytes
	}
	if opts.maxBytes <= 0 {
		opts.maxBytes = 5 * 1024 * 1024
	}
	tailLimit := int64(buildLogTailBytes)
	if tailLimit > opts.maxBytes/2 {
		tailLimit = opts.maxBytes / 2
	}

	stream := &buildLogStream{
		ctx:        context.WithoutCancel(ctx),
		store:      store,
		logger:     logger,
		buildID:    buildID,
		chunkBytes: opts.chunkBytes,
		headLimit:  opts.maxBytes - tailLimit,
		tailLimit:  int(tailLimit),
	}
	if store != nil {
		seqCtx, cancel := context.WithTimeout(stream.ctx, buildLogWriteTimeout)
		seq, err := store.NextBuildLogSeq(seqCtx, buildID)
		cancel()
		if err != nil {
			// 拿不到起始 seq 时不再写库，避免和已有分片主键冲突；构建本身继续执行。
			stream.warn("query build log seq failed, build log disabled", err)
			stream.store = nil
		}
		stream.seq = seq
	}
	if opts.flushInterval > 0 {
		stream.stop = make(chan struct{})
		stream.done = make(chan struct{})
		go stream.flushLoop(opts.flushInterval)
	}
	return stream
}

// AddSecrets 登记需要脱敏的值。必须在对应值可能出现在输出之前调用。
func (s *buildLogStream) AddSecrets(values ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, value := range values {
		if value == "" {
			continue
		}
		duplicate := false
		for _, existing := range s.secrets {
			if string(existing) == value {
				duplicate = true
				break
			}
		}
		if !duplicate {
			s.secrets = append(s.secrets, []byte(value))
			s.firstBytes[value[0]] = true
		}
	}
	// 长的密钥优先匹配，避免一个密钥是另一个密钥的子串时只替换掉一部分。
	sort.SliceStable(s.secrets, func(i, j int) bool { return len(s.secrets[i]) > len(s.secrets[j]) })
}

func (s *buildLogStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return len(p), nil
	}
	s.held = append(s.held, p...)
	s.releaseLocked(false)
	if len(s.pending) >= s.chunkBytes {
		s.flushLocked(false, true)
	}
	return len(p), nil
}

// Linef 写一行平台自身的提示，统一加 [forge] 前缀，和命令输出区分开。
func (s *buildLogStream) Linef(format string, args ...any) {
	_, _ = fmt.Fprintf(s, "[forge] "+format+"\n", args...)
}

// Close 写出剩余内容（包括截断提示和尾部），返回第一次写库失败的错误。
func (s *buildLogStream) Close() error {
	if s.stop != nil {
		close(s.stop)
		<-s.done
		s.stop = nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.storeErr
	}
	s.closed = true
	s.releaseLocked(true)
	s.flushLocked(true, false)

	if len(s.tail) > s.tailLimit {
		drop := len(s.tail) - s.tailLimit
		s.dropped += int64(drop)
		s.tail = s.tail[drop:]
	}
	if s.dropped > 0 {
		// 截断位置可能落在多字节字符中间，丢掉开头不完整的字节。
		for len(s.tail) > 0 && !utf8.RuneStart(s.tail[0]) {
			s.tail = s.tail[1:]
			s.dropped++
		}
		s.pending = append(s.pending, fmt.Sprintf("\n[forge] 日志超过 %d 字节上限，已省略中间 %d 字节\n", s.headLimit+int64(s.tailLimit), s.dropped)...)
	}
	s.pending = append(s.pending, s.tail...)
	s.tail = nil
	s.flushLocked(true, false)
	return s.storeErr
}

func (s *buildLogStream) flushLoop(interval time.Duration) {
	defer close(s.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			if !s.closed {
				s.flushLocked(false, false)
			}
			s.mu.Unlock()
		}
	}
}

// releaseLocked 对 held 做脱敏，把能确定结果的部分交给 accept；末尾可能是某个密钥前缀的字节继续保留，等后续输出再判断。
func (s *buildLogStream) releaseLocked(final bool) {
	out, rest := redactLogBytes(s.held, s.secrets, &s.firstBytes, final)
	s.held = append(s.held[:0], rest...)
	s.acceptLocked(out)
}

func (s *buildLogStream) acceptLocked(data []byte) {
	if room := s.headLimit - s.headBytes; room > 0 {
		n := int64(len(data))
		if n > room {
			n = room
		}
		s.pending = append(s.pending, data[:n]...)
		s.headBytes += n
		data = data[n:]
	}
	if len(data) == 0 {
		return
	}
	s.tail = append(s.tail, data...)
	// 尾部缓冲按两倍上限才压缩一次，避免每次写入都搬移数据。
	if len(s.tail) > 2*s.tailLimit {
		drop := len(s.tail) - s.tailLimit
		s.dropped += int64(drop)
		s.tail = append([]byte(nil), s.tail[drop:]...)
	}
}

// flushLocked 把 pending 写成分片。fullOnly 时只写满 chunkBytes 的分片，剩余部分等定时器或 Close。
func (s *buildLogStream) flushLocked(final bool, fullOnly bool) {
	for len(s.pending) > 0 && (!fullOnly || len(s.pending) >= s.chunkBytes) {
		n := len(s.pending)
		if n > s.chunkBytes {
			n = s.chunkBytes
		}
		if n < len(s.pending) || !final {
			// 分片边界不能切开多字节字符；只剩不完整字符时等后续数据。
			if cut := completeUTF8Length(s.pending[:n]); cut > 0 || n < utf8.UTFMax {
				n = cut
			}
		}
		if n == 0 {
			return
		}
		s.appendChunkLocked(s.pending[:n])
		s.pending = s.pending[n:]
	}
	if len(s.pending) == 0 {
		s.pending = nil
	}
}

func (s *buildLogStream) appendChunkLocked(data []byte) {
	if s.store == nil {
		return
	}
	// PostgreSQL text 不能包含 NUL 或非法 UTF-8。
	content := strings.ToValidUTF8(strings.ReplaceAll(string(data), "\x00", ""), "�")
	if content == "" {
		return
	}
	writeCtx, cancel := context.WithTimeout(s.ctx, buildLogWriteTimeout)
	err := s.store.AppendBuildLogChunk(writeCtx, domain.BuildLogChunk{BuildRecordID: s.buildID, Seq: s.seq, Content: content})
	cancel()
	if err != nil {
		if s.storeErr == nil {
			s.storeErr = err
			s.warn("append build log chunk failed", err)
		}
		return
	}
	s.seq++
}

func (s *buildLogStream) warn(message string, err error) {
	if s.logger != nil {
		s.logger.Warn(message, "build", s.buildID, "error", err)
	}
}

// redactLogBytes 把 data 中完整出现的密钥替换为占位符。非 final 时，如果末尾剩余内容是某个密钥的前缀，就把它作为 rest 返回，
// 等下一段输出到达后再判断，保证跨写入边界的密钥也能被替换。secrets 需按长度降序排列。
func redactLogBytes(data []byte, secrets [][]byte, firstBytes *[256]bool, final bool) ([]byte, []byte) {
	if len(secrets) == 0 {
		return data, nil
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		if !firstBytes[data[i]] {
			out = append(out, data[i])
			i++
			continue
		}
		matched := false
		for _, secret := range secrets {
			remaining := data[i:]
			if len(remaining) >= len(secret) {
				if bytes.HasPrefix(remaining, secret) {
					out = append(out, buildLogRedaction...)
					i += len(secret)
					matched = true
					break
				}
			} else if !final && bytes.HasPrefix(secret, remaining) {
				return out, data[i:]
			}
		}
		if !matched {
			out = append(out, data[i])
			i++
		}
	}
	return out, nil
}

// completeUTF8Length 返回去掉末尾不完整 UTF-8 字符后的长度。
func completeUTF8Length(data []byte) int {
	for i := len(data) - 1; i >= 0 && i >= len(data)-utf8.UTFMax; i-- {
		if utf8.RuneStart(data[i]) {
			if utf8.FullRune(data[i:]) {
				return len(data)
			}
			return i
		}
	}
	return len(data)
}
