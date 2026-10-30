package stream

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
)

// TransformMiddleware 对每个块只转换一次：用追加字符并计数的非幂等转换验证。
func TestTransformMiddlewareTransformsEachChunkOnce(t *testing.T) {
	var calls atomic.Int32
	mw := NewTransformMiddleware(func(chunk *core.LegacyStreamChunk) (*core.LegacyStreamChunk, error) {
		calls.Add(1)
		chunk.Text += "!"
		return chunk, nil
	})
	out, err := mw.Apply(context.Background(), &mockStreamOutput{chunks: textChunks("a", "b")})
	require.NoError(t, err)

	var texts []string
	for _, chunk := range rawChunks(t, out) {
		if !chunk.IsLast {
			texts = append(texts, chunk.Text)
		}
	}
	require.Equal(t, []string{"a!", "b!"}, texts)
	require.Equal(t, int32(2), calls.Load())
}

// eofSource 产出全部块后关闭 drained，再返回结束错误；TeeMiddleware 此时已分发完全部块。
type eofSource struct {
	mockStreamOutput
	drained chan struct{}
}

func (s *eofSource) Next() (*core.LegacyStreamChunk, error) {
	if s.index >= len(s.chunks) {
		close(s.drained)
	}
	return s.mockStreamOutput.Next()
}

// teePanicConsumer 第一个块关闭 entered 后阻塞到 release 关闭再 panic；其余调用若与第一个块并发或发生在 panic 之后，记录为违规。
type teePanicConsumer struct {
	entered  chan struct{}
	release  chan struct{}
	inFirst  atomic.Bool
	panicked atomic.Bool
	calls    atomic.Int32
	mu       sync.Mutex
	issues   []string
}

func (c *teePanicConsumer) violate(issue string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.issues = append(c.issues, issue)
}

func (c *teePanicConsumer) OnStart() error { return nil }
func (c *teePanicConsumer) OnChunk(chunk *core.LegacyStreamChunk) error {
	if c.calls.Add(1) == 1 {
		c.inFirst.Store(true)
		close(c.entered)
		<-c.release
		c.panicked.Store(true)
		panic("tee boom")
	}
	if c.inFirst.Load() && !c.panicked.Load() {
		c.violate(chunk.Text + ": 与上一个回调并发执行")
	}
	if c.panicked.Load() {
		c.violate(chunk.Text + ": panic 之后仍被调用")
	}
	return nil
}
func (c *teePanicConsumer) OnError(error) error { return nil }
func (c *teePanicConsumer) OnComplete() error   { return nil }

// orderedConsumer 按顺序记录数据块，收到 want 个块后关闭 done。
type orderedConsumer struct {
	mockConsumer
	want int
	done chan struct{}
}

func (c *orderedConsumer) OnChunk(chunk *core.LegacyStreamChunk) error {
	_ = c.mockConsumer.OnChunk(chunk)
	if len(c.GetChunks()) == c.want {
		close(c.done)
	}
	return nil
}

// TeeMiddleware 对同一消费者串行回调，panic 之后不再调用它：第一个块阻塞期间后续块已全部分发，
// 它们不得与第一个块并发执行，也不得在其 panic 之后执行；其他消费者按顺序收到全部数据。
func TestTeeMiddlewareConsumerCallbacksSerialAndStopAfterPanic(t *testing.T) {
	bad := &teePanicConsumer{entered: make(chan struct{}), release: make(chan struct{})}
	good := &orderedConsumer{want: 3, done: make(chan struct{})}
	source := &eofSource{mockStreamOutput: mockStreamOutput{chunks: textChunks("a", "b", "c")}, drained: make(chan struct{})}
	out, err := NewTeeMiddleware(bad, good).Apply(context.Background(), source)
	require.NoError(t, err)
	main := rawChunks(t, out)
	require.Len(t, main, 4, "主流收到全部数据与结束块")
	<-source.drained
	<-good.done
	<-bad.entered
	close(bad.release)

	require.Equal(t, []string{"a", "b", "c"}, chunkTexts(good.GetChunks()))
	bad.mu.Lock()
	defer bad.mu.Unlock()
	require.Empty(t, bad.issues)
	require.EqualValues(t, 1, bad.calls.Load())
}
