package stream

import (
	"context"
	"errors"
	"fmt"
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
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer := &mockConsumer{}
	mux.AddConsumer(consumer)

	chunks := []*core.LegacyStreamChunk{
		core.NewTextChunk("a"),
		core.NewTextChunk("b"),
	}

	source := &mockStreamOutput{chunks: chunks}

	// Run with timeout context
	mux.Start(ctx, source)

	assert.True(t, consumer.IsComplete())
}

// TestMultiplexer_Start_MultipleConsumers tests start with multiple consumers
func TestMultiplexer_Start_MultipleConsumers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

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

	mux.Start(ctx, source)

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

	testErr := errors.New("test error")
	mux.broadcastError(testErr)

	time.Sleep(50 * time.Millisecond)

	assert.Len(t, consumer.errors, 1)
}

// TestMultiplexer_ConsumerComplete tests handling consumer completion
func TestMultiplexer_ConsumerComplete(t *testing.T) {
	ctx := context.Background()
	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer := &mockConsumer{}
	mux.AddConsumer(consumer)

	mux.broadcastComplete()

	time.Sleep(50 * time.Millisecond)

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

	// Create infinite source
	source := &mockStreamOutput{
		chunks: []*core.LegacyStreamChunk{core.NewTextChunk("a")},
		index:  0,
	}

	mux.Start(ctx, source)
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
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	chunks := []*core.LegacyStreamChunk{
		core.NewTextChunk("a"),
	}

	source := &mockStreamOutput{chunks: chunks}

	err := mux.Start(ctx, source)

	assert.NoError(t, err)
}

// TestMultiplexer_StartAlreadyRunning tests starting already running multiplexer
func TestMultiplexer_StartAlreadyRunning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	mux := NewMultiplexer(ctx, nil)
	defer mux.Close()

	consumer := &mockConsumer{}
	mux.AddConsumer(consumer)

	chunks := []*core.LegacyStreamChunk{
		core.NewTextChunk("a"),
		core.NewTextChunk("b"),
		core.NewTextChunk("c"),
	}

	source := &mockStreamOutput{chunks: chunks}

	// Start first time should succeed - run in goroutine to keep it running
	go mux.Start(ctx, source)

	// Give it a moment to set the running flag
	time.Sleep(10 * time.Millisecond)

	// Try to start again - should fail
	source2 := &mockStreamOutput{chunks: chunks}
	err := mux.Start(ctx, source2)

	assert.Error(t, err)
}
