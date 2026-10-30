package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kart-io/goagent/core"
)

// Mock consumer for testing
type mockConsumer struct {
	chunks   []*core.LegacyStreamChunk
	errors   []error
	complete bool
	mu       sync.Mutex
}

func (m *mockConsumer) OnStart() error {
	return nil
}

func (m *mockConsumer) OnChunk(chunk *core.LegacyStreamChunk) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chunks = append(m.chunks, chunk)
	return nil
}

func (m *mockConsumer) OnError(err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = append(m.errors, err)
	return nil
}

func (m *mockConsumer) OnComplete() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.complete = true
	return nil
}

func (m *mockConsumer) GetChunks() []*core.LegacyStreamChunk {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.chunks
}

func (m *mockConsumer) IsComplete() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.complete
}

func (m *mockConsumer) GetErrors() []error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.errors
}

// Mock stream output for testing
type mockStreamOutput struct {
	chunks []*core.LegacyStreamChunk
	index  int
}

func (m *mockStreamOutput) Next() (*core.LegacyStreamChunk, error) {
	if m.index >= len(m.chunks) {
		return nil, errors.New("no more chunks")
	}
	chunk := m.chunks[m.index]
	m.index++
	return chunk, nil
}

func (m *mockStreamOutput) Close() error {
	return nil
}

func (m *mockStreamOutput) Context() context.Context {
	return context.Background()
}

func (m *mockStreamOutput) IsClosed() bool {
	return false
}

// TestMultiplexer_Creation tests multiplexer creation
func TestMultiplexer_Creation(t *testing.T) {
	ctx := context.Background()
	opts := core.DefaultStreamOptions()

	mux := NewMultiplexer(ctx, opts)

	assert.NotNil(t, mux)
	assert.False(t, mux.closed)
	assert.Len(t, mux.consumers, 0)
}

// TestMultiplexer_AddConsumer_Success tests adding consumer
func TestMultiplexer_AddConsumer_Success(t *testing.T) {
	ctx := context.Background()
	opts := core.DefaultStreamOptions()
	mux := NewMultiplexer(ctx, opts)
	defer mux.Close()

	consumer := &mockConsumer{}
	id, err := mux.AddConsumer(consumer)

	assert.NoError(t, err)
	assert.NotEmpty(t, id)
	assert.Len(t, mux.consumers, 1)
}

// TestMultiplexer_AddConsumer_AfterClose tests adding consumer after close
func TestMultiplexer_AddConsumer_AfterClose(t *testing.T) {
	ctx := context.Background()
	opts := core.DefaultStreamOptions()
	mux := NewMultiplexer(ctx, opts)
	mux.Close()

	consumer := &mockConsumer{}
	_, err := mux.AddConsumer(consumer)

	assert.Error(t, err)
}

// TestMultiplexer_AddConsumer_MaxLimitExceeded tests max consumer limit
func TestMultiplexer_AddConsumer_MaxLimitExceeded(t *testing.T) {
	ctx := context.Background()
	opts := core.DefaultStreamOptions()
	opts.MaxConsumers = 2

	mux := NewMultiplexer(ctx, opts)
	defer mux.Close()

	consumer1 := &mockConsumer{}
	id1, err1 := mux.AddConsumer(consumer1)
	assert.NoError(t, err1)

	consumer2 := &mockConsumer{}
	id2, err2 := mux.AddConsumer(consumer2)
	assert.NoError(t, err2)

	consumer3 := &mockConsumer{}
	_, err3 := mux.AddConsumer(consumer3)

	assert.Error(t, err3)
	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
}

// TestMultiplexer_RemoveConsumer_Success tests removing consumer
func TestMultiplexer_RemoveConsumer_Success(t *testing.T) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer := &mockConsumer{}
	id, _ := mux.AddConsumer(consumer)

	err := mux.RemoveConsumer(id)

	assert.NoError(t, err)
	assert.Len(t, mux.consumers, 0)
}

// TestMultiplexer_RemoveConsumer_NotFound tests removing non-existent consumer
func TestMultiplexer_RemoveConsumer_NotFound(t *testing.T) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	err := mux.RemoveConsumer("non-existent")

	assert.Error(t, err)
}

// TestMultiplexer_Consumers tests getting consumer list
func TestMultiplexer_Consumers(t *testing.T) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer1 := &mockConsumer{}
	consumer2 := &mockConsumer{}

	id1, _ := mux.AddConsumer(consumer1)
	id2, _ := mux.AddConsumer(consumer2)

	consumers := mux.Consumers()

	assert.Len(t, consumers, 2)
	assert.Contains(t, consumers, id1)
	assert.Contains(t, consumers, id2)
}

// TestMultiplexer_Broadcast_Single tests broadcasting to single consumer
func TestMultiplexer_Broadcast_Single(t *testing.T) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer := &mockConsumer{}
	mux.AddConsumer(consumer)

	chunk := core.NewTextChunk("test")
	err := mux.broadcast(chunk)

	assert.NoError(t, err)
}

// TestMultiplexer_Broadcast_Multiple tests broadcasting to multiple consumers
func TestMultiplexer_Broadcast_Multiple(t *testing.T) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer1 := &mockConsumer{}
	consumer2 := &mockConsumer{}

	mux.AddConsumer(consumer1)
	mux.AddConsumer(consumer2)

	chunk := core.NewTextChunk("test")
	err := mux.broadcast(chunk)

	assert.NoError(t, err)
}

// TestMultiplexer_Start_SingleConsumer tests multiplexer start with single consumer
func TestMultiplexer_Start_SingleConsumer(t *testing.T) {
	ctx := context.Background()

	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer := &mockConsumer{}
	mux.AddConsumer(consumer)

	chunks := []*core.LegacyStreamChunk{
		core.NewTextChunk("a"),
		core.NewTextChunk("b"),
	}

	source := &mockStreamOutput{chunks: chunks}

	assert.NoError(t, startWithin(t, ctx, mux, source))

	assert.Len(t, consumer.GetChunks(), 2)
	assert.True(t, consumer.IsComplete())
}

// TestMultiplexer_Start_MultipleConsumers tests start with multiple consumers
func TestMultiplexer_Start_MultipleConsumers(t *testing.T) {
	ctx := context.Background()

	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer1 := &mockConsumer{}
	consumer2 := &mockConsumer{}

	mux.AddConsumer(consumer1)
	mux.AddConsumer(consumer2)

	chunks := []*core.LegacyStreamChunk{
		core.NewTextChunk("a"),
		core.NewTextChunk("b"),
	}

	source := &mockStreamOutput{chunks: chunks}

	assert.NoError(t, startWithin(t, ctx, mux, source))

	assert.Len(t, consumer1.GetChunks(), 2)
	assert.Len(t, consumer2.GetChunks(), 2)
	assert.True(t, consumer1.IsComplete())
	assert.True(t, consumer2.IsComplete())
}

// TestMultiplexer_Close tests closing multiplexer
func TestMultiplexer_Close(t *testing.T) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)

	consumer := &mockConsumer{}
	mux.AddConsumer(consumer)

	err := mux.Close()

	assert.NoError(t, err)
	assert.True(t, mux.closed)
}

// TestMultiplexer_Close_Idempotent tests that close is idempotent
func TestMultiplexer_Close_Idempotent(t *testing.T) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)

	err1 := mux.Close()
	err2 := mux.Close()

	assert.NoError(t, err1)
	assert.Error(t, err2)
}

// TestMultiplexer_Stats tests statistics
func TestMultiplexer_Stats(t *testing.T) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer1 := &mockConsumer{}
	consumer2 := &mockConsumer{}

	mux.AddConsumer(consumer1)
	mux.AddConsumer(consumer2)

	stats := mux.Stats()

	assert.Equal(t, 2, stats.ConsumerCount)
	assert.Equal(t, 2, stats.ActiveConsumers)
}

// TestMultiplexer_Backpressure tests backpressure handling
func TestMultiplexer_Backpressure(t *testing.T) {
	ctx := context.Background()
	opts := core.DefaultStreamOptions()
	opts.EnableBackpressure = true
	opts.BufferSize = 1

	mux := NewMultiplexer(ctx, opts)
	defer mux.Close()

	consumer := &mockConsumer{}
	mux.AddConsumer(consumer)

	chunk := core.NewTextChunk("test")
	err := mux.broadcast(chunk)

	assert.NoError(t, err)
}

// TestMultiplexer_ConsumerError tests handling consumer error
func TestMultiplexer_ConsumerError(t *testing.T) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer := &mockConsumer{}
	mux.AddConsumer(consumer)

	assert.NoError(t, startWithin(t, ctx, mux, &mockStreamOutput{}))

	errs := consumer.GetErrors()
	if assert.Len(t, errs, 1) {
		assert.EqualError(t, errs[0], "no more chunks")
	}
}

// TestMultiplexer_ConsumerComplete tests handling consumer completion
func TestMultiplexer_ConsumerComplete(t *testing.T) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer := &mockConsumer{}
	mux.AddConsumer(consumer)

	assert.NoError(t, startWithin(t, ctx, mux, &mockStreamOutput{}))

	assert.True(t, consumer.IsComplete())
}

// TestMultiplexer_ContextTimeout tests context timeout
func TestMultiplexer_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer := &mockConsumer{}
	mux.AddConsumer(consumer)

	// 源永不结束，Start 只能因超时返回
	assert.NoError(t, startWithin(t, ctx, mux, &endlessSource{}))

	assert.True(t, consumer.IsComplete())
	errs := consumer.GetErrors()
	if assert.Len(t, errs, 1) {
		assert.ErrorIs(t, errs[0], context.DeadlineExceeded)
	}
}

// Concurrent operation tests

// TestMultiplexer_ConcurrentAddRemove tests concurrent add/remove
func TestMultiplexer_ConcurrentAddRemove(t *testing.T) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	var wg sync.WaitGroup
	var ids []string
	var mu sync.Mutex

	// Add consumers concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			consumer := &mockConsumer{}
			id, _ := mux.AddConsumer(consumer)
			mu.Lock()
			ids = append(ids, id)
			mu.Unlock()
		}()
	}

	wg.Wait()

	assert.Len(t, ids, 10)

	// Remove consumers concurrently
	for _, id := range ids {
		wg.Add(1)
		go func(consumerID string) {
			defer wg.Done()
			mux.RemoveConsumer(consumerID)
		}(id)
	}

	wg.Wait()

	assert.Len(t, mux.Consumers(), 0)
}

// TestMultiplexer_ConcurrentBroadcast tests concurrent broadcasting
func TestMultiplexer_ConcurrentBroadcast(t *testing.T) {
	ctx := context.Background()
	opts := core.DefaultStreamOptions()
	opts.BufferSize = 100

	mux := NewMultiplexer(ctx, opts)
	defer mux.Close()

	for i := 0; i < 5; i++ {
		consumer := &mockConsumer{}
		mux.AddConsumer(consumer)
	}

	var wg sync.WaitGroup
	var broadcastCount int32

	// Broadcast concurrently
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			chunk := core.NewTextChunk(fmt.Sprintf("chunk_%d", idx))
			mux.broadcast(chunk)
			atomic.AddInt32(&broadcastCount, 1)
		}(i)
	}

	wg.Wait()

	assert.Equal(t, int32(50), atomic.LoadInt32(&broadcastCount))
}

// TestMultiplexer_RapidStartClose tests rapid start/close
func TestMultiplexer_RapidStartClose(t *testing.T) {
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		mux := NewMultiplexer(ctx, nil)
		consumer := &mockConsumer{}
		mux.AddConsumer(consumer)
		mux.Close()
	}
}

// Stress tests

// TestMultiplexer_ManyConsumers tests with many consumers
func TestMultiplexer_ManyConsumers(t *testing.T) {
	ctx := context.Background()
	opts := core.DefaultStreamOptions()
	opts.BufferSize = 100
	opts.MaxConsumers = 1000

	mux := NewMultiplexer(ctx, opts)
	defer mux.Close()

	// Add many consumers
	for i := 0; i < 100; i++ {
		consumer := &mockConsumer{}
		mux.AddConsumer(consumer)
	}

	assert.Len(t, mux.Consumers(), 100)
}

// TestMultiplexer_HighThroughput tests high throughput broadcasting
func TestMultiplexer_HighThroughput(t *testing.T) {
	ctx := context.Background()
	opts := core.DefaultStreamOptions()
	opts.BufferSize = 1000

	mux := NewMultiplexer(ctx, opts)
	defer mux.Close()

	consumer := &mockConsumer{}
	mux.AddConsumer(consumer)

	// Broadcast many chunks
	for i := 0; i < 1000; i++ {
		chunk := core.NewTextChunk(fmt.Sprintf("data_%d", i))
		mux.broadcast(chunk)
	}
}

// Performance tests

// BenchmarkMultiplexer_AddConsumer benchmarks adding consumer
func BenchmarkMultiplexer_AddConsumer(b *testing.B) {
	ctx := context.Background()
	opts := core.DefaultStreamOptions()
	opts.MaxConsumers = 10000

	mux := NewMultiplexer(ctx, opts)
	defer mux.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		consumer := &mockConsumer{}
		mux.AddConsumer(consumer)
	}
}

// BenchmarkMultiplexer_Broadcast benchmarks broadcasting
func BenchmarkMultiplexer_Broadcast(b *testing.B) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer := &mockConsumer{}
	mux.AddConsumer(consumer)

	chunk := core.NewTextChunk("test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mux.broadcast(chunk)
	}
}

// BenchmarkMultiplexer_BroadcastMultiple benchmarks broadcast to many consumers
func BenchmarkMultiplexer_BroadcastMultiple(b *testing.B) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	for i := 0; i < 10; i++ {
		consumer := &mockConsumer{}
		mux.AddConsumer(consumer)
	}

	chunk := core.NewTextChunk("test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mux.broadcast(chunk)
	}
}

// Edge case tests

// TestMultiplexer_StartWithNoConsumers tests starting with no consumers
func TestMultiplexer_StartWithNoConsumers(t *testing.T) {
	ctx := context.Background()

	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	chunks := []*core.LegacyStreamChunk{
		core.NewTextChunk("a"),
	}

	source := &mockStreamOutput{chunks: chunks}

	assert.NoError(t, startWithin(t, ctx, mux, source))
}

// TestMultiplexer_StartAlreadyRunning tests starting already running multiplexer
func TestMultiplexer_StartAlreadyRunning(t *testing.T) {
	ctx := context.Background()

	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer := &mockConsumer{}
	mux.AddConsumer(consumer)

	// 第一次 Start 读完数据后卡在源上，保持运行状态
	gate := make(chan struct{})
	source := &drainedSource{mockStreamOutput: mockStreamOutput{chunks: textChunks("a", "b", "c")}, drained: make(chan struct{}), gate: gate}
	done := make(chan error, 1)
	go func() { done <- mux.Start(ctx, source) }()
	<-source.drained

	// Try to start again - should fail
	err := mux.Start(ctx, &mockStreamOutput{chunks: textChunks("a")})
	assert.Error(t, err)

	close(gate)
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(startReturnLimit):
		t.Fatalf("Start 在 %v 内未返回", startReturnLimit)
	}
}

// startReturnLimit 只用来防止测试挂起：默认流超时为 5 分钟，在此时限内返回就说明 Start 没有等 ctx 结束。
const startReturnLimit = 2 * time.Second

// startWithin 在 goroutine 中运行 Start，超过 startReturnLimit 仍未返回即判失败。
func startWithin(t *testing.T, ctx context.Context, mux *Multiplexer, source core.StreamOutput) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- mux.Start(ctx, source) }()
	select {
	case err := <-done:
		return err
	case <-time.After(startReturnLimit):
		t.Fatalf("Start 在 %v 内未返回", startReturnLimit)
		return nil
	}
}

// 源结束（EOF 或错误）后 Start 应在消费者处理完数据与结束回调后返回，不等 ctx 结束。
func TestMultiplexerStartReturnsWhenSourceEnds(t *testing.T) {
	mux := NewMultiplexer(context.Background(), nil)
	defer mux.Close()
	consumers := []*mockConsumer{{}, {}}
	for _, c := range consumers {
		_, err := mux.AddConsumer(c)
		assert.NoError(t, err)
	}

	err := startWithin(t, context.Background(), mux, &mockStreamOutput{chunks: textChunks("a", "b", "c")})
	assert.NoError(t, err)
	for _, c := range consumers {
		assert.Len(t, c.GetChunks(), 3)
		assert.Len(t, c.GetErrors(), 1, "源的结束错误交给 OnError")
		assert.True(t, c.IsComplete())
	}
}

// orderConsumer 按调用顺序记录回调；第一个块阻塞到 gate 关闭，使其余块在缓冲区积压。
type orderConsumer struct {
	gate      chan struct{}
	completed chan struct{}
	mu        sync.Mutex
	events    []string
}

func (c *orderConsumer) record(event string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *orderConsumer) OnStart() error { return nil }

func (c *orderConsumer) OnChunk(chunk *core.LegacyStreamChunk) error {
	if chunk.Text == "0" {
		<-c.gate
	}
	c.record("chunk:" + chunk.Text)
	return nil
}

func (c *orderConsumer) OnError(error) error {
	c.record("error")
	return nil
}

func (c *orderConsumer) OnComplete() error {
	c.record("complete")
	close(c.completed)
	return nil
}

// drainedSource 产出全部块后关闭 drained，gate 非 nil 时再等它关闭，然后返回结束错误。
type drainedSource struct {
	mockStreamOutput
	drained chan struct{}
	gate    <-chan struct{}
}

func (s *drainedSource) Next() (*core.LegacyStreamChunk, error) {
	if s.index >= len(s.chunks) {
		close(s.drained)
		if s.gate != nil {
			<-s.gate
		}
	}
	return s.mockStreamOutput.Next()
}

// 每个消费者按“数据块 → 结束回调”的顺序收到回调：源结束时缓冲区里还积压着数据，OnError/OnComplete 也要排在它们之后。
func TestMultiplexerFinishCallbacksAfterBufferedChunks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()
	consumer := &orderConsumer{gate: make(chan struct{}), completed: make(chan struct{})}
	_, err := mux.AddConsumer(consumer)
	assert.NoError(t, err)

	texts := []string{"0", "1", "2", "3", "4", "5", "6", "7"}
	source := &drainedSource{mockStreamOutput: mockStreamOutput{chunks: textChunks(texts...)}, drained: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- mux.Start(ctx, source) }()
	// 源已读完时消费者仍卡在第一个块上，其余块都在缓冲区中
	<-source.drained
	close(consumer.gate)
	select {
	case <-consumer.completed:
	case <-time.After(startReturnLimit):
		t.Fatalf("OnComplete 在 %v 内未被调用", startReturnLimit)
	}

	want := make([]string, 0, len(texts)+2)
	for _, text := range texts {
		want = append(want, "chunk:"+text)
	}
	want = append(want, "error", "complete")
	consumer.mu.Lock()
	assert.Equal(t, want, consumer.events)
	consumer.mu.Unlock()
	cancel()
	assert.NoError(t, <-done)
}

// endlessSource 每次都返回数据块；cancel 非 nil 时在第 cancelAt 次调用时取消 ctx，用来确定性地触发 ctx 结束。
type endlessSource struct {
	mockStreamOutput
	calls    int
	cancelAt int
	cancel   context.CancelFunc
}

func (s *endlessSource) Next() (*core.LegacyStreamChunk, error) {
	s.calls++
	if s.cancel != nil && s.calls == s.cancelAt {
		s.cancel()
	}
	return core.NewTextChunk("x"), nil
}

// 源不结束时，ctx 取消后 Start 依然及时返回，消费者收到 ctx 的错误与完成通知。
func TestMultiplexerStartReturnsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mux := NewMultiplexer(context.Background(), nil)
	defer mux.Close()
	consumer := &mockConsumer{}
	_, err := mux.AddConsumer(consumer)
	assert.NoError(t, err)

	err = startWithin(t, ctx, mux, &endlessSource{cancelAt: 3, cancel: cancel})
	assert.NoError(t, err)
	assert.True(t, consumer.IsComplete())
	errs := consumer.GetErrors()
	if assert.Len(t, errs, 1) {
		assert.ErrorIs(t, errs[0], context.Canceled)
	}
}

// stepSource 每次 Next 先向 entered 发送信号，再从 items 取块；items 关闭后返回 io.EOF。
// 测试据此确定源正停在哪一次 Next 上，从而确定性地安排 AddConsumer 的时机。
type stepSource struct {
	mockStreamOutput
	entered chan struct{}
	items   chan *core.LegacyStreamChunk
}

func newStepSource() *stepSource {
	return &stepSource{entered: make(chan struct{}), items: make(chan *core.LegacyStreamChunk)}
}

func (s *stepSource) Next() (*core.LegacyStreamChunk, error) {
	s.entered <- struct{}{}
	chunk, ok := <-s.items
	if !ok {
		return nil, io.EOF
	}
	return chunk, nil
}

// gatedConsumer 在 OnChunk 中阻塞到 gate 关闭，用来确认 Start 会等待它。
type gatedConsumer struct {
	mockConsumer
	gate chan struct{}
}

func (c *gatedConsumer) OnChunk(chunk *core.LegacyStreamChunk) error {
	<-c.gate
	return c.mockConsumer.OnChunk(chunk)
}

// Start 运行中加入的消费者从加入时起接收后续数据，源结束后照常收到 OnError/OnComplete，Start 等它处理完才返回。
func TestMultiplexerConsumerAddedWhileRunning(t *testing.T) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()
	early := &mockConsumer{}
	_, err := mux.AddConsumer(early)
	assert.NoError(t, err)

	source := newStepSource()
	done := make(chan error, 1)
	go func() { done <- mux.Start(ctx, source) }()

	<-source.entered
	source.items <- core.NewTextChunk("before")
	// 第二次进入 Next 说明 "before" 已广播完
	<-source.entered
	late := &gatedConsumer{gate: make(chan struct{})}
	_, err = mux.AddConsumer(late)
	assert.NoError(t, err)
	source.items <- core.NewTextChunk("after")
	<-source.entered
	close(source.items)

	select {
	case <-done:
		t.Fatal("Start 在运行中加入的消费者处理完之前就返回了")
	case <-time.After(50 * time.Millisecond):
	}
	close(late.gate)
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(startReturnLimit):
		t.Fatalf("Start 在 %v 内未返回", startReturnLimit)
	}

	assert.Equal(t, []string{"before", "after"}, chunkTexts(early.GetChunks()))
	assert.Equal(t, []string{"after"}, chunkTexts(late.GetChunks()), "只接收加入之后的数据")
	errs := late.GetErrors()
	if assert.Len(t, errs, 1) {
		assert.ErrorIs(t, errs[0], io.EOF)
	}
	assert.True(t, late.IsComplete())
}

func chunkTexts(chunks []*core.LegacyStreamChunk) []string {
	texts := make([]string, 0, len(chunks))
	for _, c := range chunks {
		texts = append(texts, c.Text)
	}
	return texts
}

// seqConsumer 检查回调串行且 OnComplete 之后不再有回调。
type seqConsumer struct {
	inCall    atomic.Int32
	completed atomic.Bool
	violation atomic.Value
}

func (c *seqConsumer) enter(name string) func() {
	if c.inCall.Add(1) != 1 {
		c.violation.Store(name + ": 回调并发执行")
	}
	if c.completed.Load() {
		c.violation.Store(name + ": OnComplete 之后仍有回调")
	}
	return func() { c.inCall.Add(-1) }
}

func (c *seqConsumer) OnStart() error { return nil }
func (c *seqConsumer) OnChunk(*core.LegacyStreamChunk) error {
	defer c.enter("OnChunk")()
	return nil
}
func (c *seqConsumer) OnError(error) error {
	defer c.enter("OnError")()
	return nil
}
func (c *seqConsumer) OnComplete() error {
	defer c.enter("OnComplete")()
	c.completed.Store(true)
	return nil
}

// AddConsumer 与源结束、Close 并发：Start 都能及时返回，回调串行且 OnComplete 之后没有回调；
// 在源结束前加入的消费者都收到 OnComplete（未被 Close 时）。
func TestMultiplexerAddConsumerRacesWithEndAndClose(t *testing.T) {
	for i := 0; i < 200; i++ {
		withClose := i%2 == 1
		ctx := context.Background()
		mux := NewMultiplexer(ctx, nil)
		_, err := mux.AddConsumer(&seqConsumer{})
		assert.NoError(t, err)

		source := &mockStreamOutput{chunks: textChunks("a", "b", "c")}
		var added []*seqConsumer
		var addWG sync.WaitGroup
		var addMu sync.Mutex
		for j := 0; j < 4; j++ {
			addWG.Add(1)
			go func() {
				defer addWG.Done()
				c := &seqConsumer{}
				if _, err := mux.AddConsumer(c); err == nil {
					addMu.Lock()
					added = append(added, c)
					addMu.Unlock()
				}
			}()
		}
		if withClose {
			addWG.Add(1)
			go func() {
				defer addWG.Done()
				_ = mux.Close()
			}()
		}
		assert.NoError(t, startWithin(t, ctx, mux, source))
		addWG.Wait()
		if !withClose {
			_ = mux.Close()
		}
		for _, c := range added {
			if v := c.violation.Load(); v != nil {
				t.Fatalf("第 %d 轮: %v", i, v)
			}
		}
	}
}

// removingConsumer 在第一个块上阻塞到 gate 关闭并返回错误，随后在 OnError 中调用 remove 把自己移除；按调用顺序记录回调。
type removingConsumer struct {
	orderConsumer
	remove func()
}

func (c *removingConsumer) OnChunk(chunk *core.LegacyStreamChunk) error {
	if err := c.orderConsumer.OnChunk(chunk); err != nil {
		return err
	}
	if chunk.Text == "0" {
		return errors.New("chunk failed")
	}
	return nil
}

func (c *removingConsumer) OnError(err error) error {
	c.record("error")
	if c.remove != nil {
		remove := c.remove
		c.remove = nil
		remove()
	}
	return nil
}

func (c *removingConsumer) OnComplete() error {
	c.record("complete")
	return nil
}

// signalConsumer 收到源的结束错误时关闭 finished。
type signalConsumer struct {
	mockConsumer
	finished chan struct{}
}

func (c *signalConsumer) OnError(err error) error {
	close(c.finished)
	return c.mockConsumer.OnError(err)
}

// 消费者被 RemoveConsumer/Close 移除后不再收到任何回调：即使源已结束、缓冲区里还积压着数据，
// 也不再收到这些数据块与 OnError/OnComplete。
func TestMultiplexerRemovedConsumerGetsNoMoreCallbacks(t *testing.T) {
	cases := map[string]func(mux *Multiplexer, id string){
		"RemoveConsumer": func(mux *Multiplexer, id string) { _ = mux.RemoveConsumer(id) },
		"Close":          func(mux *Multiplexer, _ string) { _ = mux.Close() },
	}
	for name, remove := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mux := NewMultiplexer(ctx, nil)
			defer mux.Close()
			consumer := &removingConsumer{orderConsumer: orderConsumer{gate: make(chan struct{})}}
			id, err := mux.AddConsumer(consumer)
			assert.NoError(t, err)
			consumer.remove = func() { remove(mux, id) }
			witness := &signalConsumer{finished: make(chan struct{})}
			_, err = mux.AddConsumer(witness)
			assert.NoError(t, err)

			done := make(chan error, 1)
			go func() { done <- mux.Start(ctx, &mockStreamOutput{chunks: textChunks("0", "1", "2")}) }()
			// witness 收到结束错误说明 Start 已进入关闭通道的临界区；Stats 取锁等它退出，此时所有通道都已标记为源结束
			<-witness.finished
			_ = mux.Stats()
			close(consumer.gate)
			select {
			case err := <-done:
				assert.NoError(t, err)
			case <-time.After(startReturnLimit):
				t.Fatalf("Start 在 %v 内未返回", startReturnLimit)
			}

			consumer.mu.Lock()
			defer consumer.mu.Unlock()
			assert.Equal(t, []string{"chunk:0", "error"}, consumer.events, "移除之后不应再收到缓冲数据与结束回调")
		})
	}
}

// failingConsumer 的 OnChunk 总返回错误；第一个块阻塞到 gate 关闭，使后续块因缓冲区满而被丢弃。
type failingConsumer struct {
	gate chan struct{}
	once sync.Once
}

func (c *failingConsumer) OnStart() error { return nil }
func (c *failingConsumer) OnChunk(*core.LegacyStreamChunk) error {
	c.once.Do(func() { <-c.gate })
	return errors.New("chunk failed")
}
func (c *failingConsumer) OnError(error) error { return nil }
func (c *failingConsumer) OnComplete() error   { return nil }

// 广播丢块与消费者报错都累加错误计数，Stats 并发读取：在 -race 下不得有数据竞争，结束后计数等于块数。
func TestMultiplexerErrorCountRaceFree(t *testing.T) {
	const total = 50
	ctx := context.Background()
	opts := core.DefaultStreamOptions()
	opts.BufferSize = 1
	opts.EnableBackpressure = true
	mux := NewMultiplexer(ctx, opts)
	defer mux.Close()
	consumer := &failingConsumer{gate: make(chan struct{})}
	_, err := mux.AddConsumer(consumer)
	assert.NoError(t, err)

	texts := make([]string, total)
	for i := range texts {
		texts[i] = fmt.Sprint(i)
	}
	source := &drainedSource{mockStreamOutput: mockStreamOutput{chunks: textChunks(texts...)}, drained: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- mux.Start(ctx, source) }()

	statsDone := make(chan struct{})
	go func() {
		defer close(statsDone)
		for {
			select {
			case <-done:
				return
			default:
				_ = mux.Stats()
			}
		}
	}()
	<-source.drained
	close(consumer.gate)
	<-statsDone
	// 每个块要么被丢弃、要么被消费者处理并返回错误，各计一次
	assert.Equal(t, total, mux.Stats().TotalErrors)
}

// hookConsumer 在 OnStart 中调用 hook。
type hookConsumer struct {
	mockConsumer
	hook func()
}

func (c *hookConsumer) OnStart() error {
	c.hook()
	return nil
}

// OnStart 等宿主回调在锁外调用：回调里调用多路复用器的 Stats/Consumers/Close 不会死锁。
func TestMultiplexerOnStartCanCallMultiplexer(t *testing.T) {
	cases := map[string]struct {
		hook    func(mux *Multiplexer)
		wantErr bool
	}{
		"Stats":     {hook: func(mux *Multiplexer) { _ = mux.Stats() }},
		"Consumers": {hook: func(mux *Multiplexer) { _ = mux.Consumers() }},
		"Close":     {hook: func(mux *Multiplexer) { _ = mux.Close() }, wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// 死锁时 mux.mu 一直被占用，不能 defer Close，只在 AddConsumer 返回后关闭
			mux := NewMultiplexer(context.Background(), nil)
			done := make(chan error, 1)
			go func() {
				_, err := mux.AddConsumer(&hookConsumer{hook: func() { tc.hook(mux) }})
				done <- err
			}()
			select {
			case err := <-done:
				if tc.wantErr {
					assert.Error(t, err, "OnStart 期间多路复用器已关闭，消费者不应被加入")
					assert.Empty(t, mux.Consumers())
				} else {
					assert.NoError(t, err)
					assert.Len(t, mux.Consumers(), 1)
				}
				_ = mux.Close()
			case <-time.After(startReturnLimit):
				t.Fatalf("AddConsumer 在 %v 内未返回，OnStart 中调用 %s 死锁", startReturnLimit, name)
			}
		})
	}
}
