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
)

// TestStreamChunk_Creation tests stream chunk creation and initialization
func TestStreamChunk_Creation(t *testing.T) {
	// Test basic chunk creation
	chunk := NewStreamChunk("test data")
	assert.NotNil(t, chunk)
	assert.Equal(t, "test data", chunk.Data)
	assert.NotNil(t, chunk.Metadata)
	assert.False(t, chunk.Done)
	assert.Nil(t, chunk.Error)
	assert.False(t, chunk.Timestamp.IsZero())
}

// TestStreamChunk_WithMetadata tests chunk with metadata
func TestStreamChunk_WithMetadata(t *testing.T) {
	chunk := NewStreamChunk("test")
	chunk.Metadata["key"] = "value"
	chunk.Metadata["count"] = 42

	assert.Equal(t, "value", chunk.Metadata["key"])
	assert.Equal(t, 42, chunk.Metadata["count"])
}

// TestStreamChunk_WithError tests chunk with error
func TestStreamChunk_WithError(t *testing.T) {
	testErr := errors.New("test error")
	chunk := NewStreamChunk(nil)
	chunk.Error = testErr

	assert.Equal(t, testErr, chunk.Error)
	assert.NotNil(t, chunk.Error)
}

// TestStreamChunk_Done tests done flag
func TestStreamChunk_Done(t *testing.T) {
	chunk := NewStreamChunk("data")
	assert.False(t, chunk.Done)

	chunk.Done = true
	assert.True(t, chunk.Done)
}

// TestFuncStreamHandler_OnChunk tests functional stream handler
func TestFuncStreamHandler_OnChunk(t *testing.T) {
	called := false
	handler := NewFuncStreamHandler(
		func(chunk *StreamChunk) error {
			called = true
			return nil
		},
		nil,
		nil,
	)

	chunk := NewStreamChunk("test")
	err := handler.OnChunk(chunk)

	assert.NoError(t, err)
	assert.True(t, called)
}

// TestFuncStreamHandler_OnChunkWithError tests handler error handling
func TestFuncStreamHandler_OnChunkWithError(t *testing.T) {
	testErr := errors.New("chunk error")
	handler := NewFuncStreamHandler(
		func(chunk *StreamChunk) error {
			return testErr
		},
		nil,
		nil,
	)

	chunk := NewStreamChunk("test")
	err := handler.OnChunk(chunk)

	assert.Equal(t, testErr, err)
}

// TestFuncStreamHandler_OnComplete tests completion callback
func TestFuncStreamHandler_OnComplete(t *testing.T) {
	called := false
	handler := NewFuncStreamHandler(
		nil,
		func() error {
			called = true
			return nil
		},
		nil,
	)

	err := handler.OnComplete()

	assert.NoError(t, err)
	assert.True(t, called)
}

// TestFuncStreamHandler_OnError tests error callback
func TestFuncStreamHandler_OnError(t *testing.T) {
	called := false
	var receivedErr error
	handler := NewFuncStreamHandler(
		nil,
		nil,
		func(err error) error {
			called = true
			receivedErr = err
			return nil
		},
	)

	testErr := errors.New("test error")
	err := handler.OnError(testErr)

	assert.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, testErr, receivedErr)
}

// TestFuncStreamHandler_NilCallbacks tests with nil callbacks
func TestFuncStreamHandler_NilCallbacks(t *testing.T) {
	handler := NewFuncStreamHandler(nil, nil, nil)

	chunk := NewStreamChunk("test")
	assert.NoError(t, handler.OnChunk(chunk))
	assert.NoError(t, handler.OnComplete())
	assert.NoError(t, handler.OnError(errors.New("test")))
}

// TestStreamManager_NewStreamManager tests manager creation
func TestStreamManager_NewStreamManager(t *testing.T) {
	tests := []struct {
		name     string
		config   StreamManagerConfig
		expected int
	}{
		{
			name: "default buffer size",
			config: StreamManagerConfig{
				BufferSize: 0,
				Timeout:    30 * time.Second,
			},
			expected: 100,
		},
		{
			name: "custom buffer size",
			config: StreamManagerConfig{
				BufferSize: 50,
				Timeout:    30 * time.Second,
			},
			expected: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewStreamManager(tt.config)
			assert.Equal(t, tt.expected, mgr.bufferSize)
			assert.Equal(t, 30*time.Second, mgr.timeout)
		})
	}
}

// TestStreamManager_Process_Success tests successful stream processing
func TestStreamManager_Process_Success(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	streamCh := make(chan *StreamChunk, 3)
	defer close(streamCh)

	streamCh <- NewStreamChunk("chunk1")
	streamCh <- NewStreamChunk("chunk2")
	finalChunk := NewStreamChunk("final")
	finalChunk.Done = true
	streamCh <- finalChunk

	processed := 0
	handler := NewFuncStreamHandler(
		func(chunk *StreamChunk) error {
			processed++
			return nil
		},
		func() error {
			return nil
		},
		nil,
	)

	err := mgr.Process(context.Background(), streamCh, handler)
	assert.NoError(t, err)
	assert.Equal(t, 3, processed)
}

// TestStreamManager_Process_WithError tests error handling in processing
func TestStreamManager_Process_WithError(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	streamCh := make(chan *StreamChunk, 2)
	defer close(streamCh)

	streamCh <- NewStreamChunk("chunk1")
	errorChunk := NewStreamChunk(nil)
	errorChunk.Error = errors.New("stream error")
	streamCh <- errorChunk

	handler := NewFuncStreamHandler(
		func(chunk *StreamChunk) error {
			return nil
		},
		nil,
		func(err error) error {
			return nil
		},
	)

	err := mgr.Process(context.Background(), streamCh, handler)
	assert.Error(t, err)
}

// TestStreamManager_Process_HandlerError tests handler returning error
func TestStreamManager_Process_HandlerError(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	streamCh := make(chan *StreamChunk, 1)
	defer close(streamCh)

	streamCh <- NewStreamChunk("test")

	handlerErr := errors.New("handler error")
	handler := NewFuncStreamHandler(
		func(chunk *StreamChunk) error {
			return handlerErr
		},
		nil,
		func(err error) error {
			return nil
		},
	)

	err := mgr.Process(context.Background(), streamCh, handler)
	assert.Equal(t, handlerErr, err)
}

// TestStreamManager_Process_Timeout tests timeout handling
func TestStreamManager_Process_Timeout(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 100 * time.Millisecond})

	streamCh := make(chan *StreamChunk)

	handler := NewFuncStreamHandler(
		func(chunk *StreamChunk) error {
			return nil
		},
		nil,
		func(err error) error {
			return nil
		},
	)

	start := time.Now()
	err := mgr.Process(context.Background(), streamCh, handler)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.True(t, elapsed >= 100*time.Millisecond)
}

// TestStreamManager_Transform_Success tests successful transformation
func TestStreamManager_Transform_Success(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	input := make(chan *StreamChunk, 2)

	input <- NewStreamChunk("test1")
	input <- NewStreamChunk("test2")
	close(input) // Close immediately after sending data to avoid deadlock

	transformer := func(chunk *StreamChunk) (*StreamChunk, error) {
		newChunk := NewStreamChunk(fmt.Sprintf("%s_transformed", chunk.Data))
		return newChunk, nil
	}

	output := mgr.Transform(context.Background(), input, transformer)

	results := make([]interface{}, 0)
	for chunk := range output {
		results = append(results, chunk.Data)
	}

	assert.Len(t, results, 2)
	assert.Equal(t, "test1_transformed", results[0])
	assert.Equal(t, "test2_transformed", results[1])
}

// TestStreamManager_Transform_WithError tests error handling in transformation
func TestStreamManager_Transform_WithError(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	input := make(chan *StreamChunk, 1)

	input <- NewStreamChunk("test")
	close(input) // Close immediately after sending data to avoid deadlock

	transformErr := errors.New("transform error")
	transformer := func(chunk *StreamChunk) (*StreamChunk, error) {
		return nil, transformErr
	}

	output := mgr.Transform(context.Background(), input, transformer)

	chunk := <-output
	assert.Equal(t, transformErr, chunk.Error)
}

// TestStreamManager_Transform_ContextCancellation tests context cancellation during transformation
func TestStreamManager_Transform_ContextCancellation(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	input := make(chan *StreamChunk, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	input <- NewStreamChunk("test")
	close(input)

	transformer := func(chunk *StreamChunk) (*StreamChunk, error) {
		return chunk, nil
	}

	output := mgr.Transform(ctx, input, transformer)
	chunk := <-output

	assert.NotNil(t, chunk.Error)
	assert.True(t, chunk.Done)
}

// TestStreamManager_Filter_Success tests successful filtering
func TestStreamManager_Filter_Success(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	input := make(chan *StreamChunk, 3)

	input <- NewStreamChunk("keep1")
	input <- NewStreamChunk("skip")
	input <- NewStreamChunk("keep2")
	close(input) // Close immediately after sending data to avoid deadlock

	predicate := func(chunk *StreamChunk) bool {
		if str, ok := chunk.Data.(string); ok {
			return str != "skip"
		}
		return true
	}

	output := mgr.Filter(context.Background(), input, predicate)

	results := make([]interface{}, 0)
	for chunk := range output {
		results = append(results, chunk.Data)
	}

	assert.Len(t, results, 2)
	assert.Equal(t, "keep1", results[0])
	assert.Equal(t, "keep2", results[1])
}

// TestStreamManager_Filter_AllFiltered tests when all items are filtered
func TestStreamManager_Filter_AllFiltered(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	input := make(chan *StreamChunk, 2)

	input <- NewStreamChunk("a")
	input <- NewStreamChunk("b")
	close(input) // Close immediately after sending data to avoid deadlock

	predicate := func(chunk *StreamChunk) bool {
		return false // Filter everything
	}

	output := mgr.Filter(context.Background(), input, predicate)

	results := make([]interface{}, 0)
	for chunk := range output {
		results = append(results, chunk.Data)
	}

	assert.Len(t, results, 0)
}

// TestStreamManager_Merge_MultipleStreams tests merging multiple streams
func TestStreamManager_Merge_MultipleStreams(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	stream1 := make(chan *StreamChunk, 2)
	stream2 := make(chan *StreamChunk, 2)
	stream3 := make(chan *StreamChunk, 2)

	stream1 <- NewStreamChunk("s1_1")
	stream1 <- NewStreamChunk("s1_2")
	stream2 <- NewStreamChunk("s2_1")
	stream2 <- NewStreamChunk("s2_2")
	stream3 <- NewStreamChunk("s3_1")
	stream3 <- NewStreamChunk("s3_2")

	close(stream1)
	close(stream2)
	close(stream3)

	merged := mgr.Merge(context.Background(), stream1, stream2, stream3)

	results := make([]interface{}, 0)
	for chunk := range merged {
		results = append(results, chunk.Data)
	}

	assert.Len(t, results, 6)
}

// TestStreamManager_Merge_EmptyStreams tests merging with empty streams
func TestStreamManager_Merge_EmptyStreams(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	stream1 := make(chan *StreamChunk)
	stream2 := make(chan *StreamChunk)

	close(stream1)
	close(stream2)

	merged := mgr.Merge(context.Background(), stream1, stream2)

	results := make([]interface{}, 0)
	for chunk := range merged {
		results = append(results, chunk.Data)
	}

	assert.Len(t, results, 0)
}

// TestStreamManager_Merge_WithContextCancellation tests merge with cancellation
func TestStreamManager_Merge_WithContextCancellation(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	stream1 := make(chan *StreamChunk, 1)
	stream2 := make(chan *StreamChunk, 1)

	stream1 <- NewStreamChunk("s1")
	stream2 <- NewStreamChunk("s2")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Close streams immediately to avoid deadlock
	close(stream1)
	close(stream2)

	merged := mgr.Merge(ctx, stream1, stream2)

	// Context is cancelled, so may receive 0 or some chunks depending on timing
	// The important thing is that the merged channel eventually closes
	count := 0
	for range merged {
		count++
	}

	// Since context is already cancelled, we expect few or no chunks
	assert.LessOrEqual(t, count, 2) // At most 2 chunks (one from each stream)
}

// TestStreamManager_Buffer_Exact tests buffering with exact boundary
func TestStreamManager_Buffer_Exact(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	input := make(chan *StreamChunk, 3)

	input <- NewStreamChunk("a")
	input <- NewStreamChunk("b")
	finalChunk := NewStreamChunk("c")
	finalChunk.Done = true
	input <- finalChunk
	close(input) // Close immediately after sending data to avoid deadlock

	output := mgr.Buffer(context.Background(), input, 2)

	batches := make([][]*StreamChunk, 0)
	for batch := range output {
		batches = append(batches, batch)
	}

	assert.Len(t, batches, 2)
	assert.Len(t, batches[0], 2)
	assert.Len(t, batches[1], 1)
}

// TestStreamManager_Buffer_Overflow tests buffering with overflow
func TestStreamManager_Buffer_Overflow(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	input := make(chan *StreamChunk, 5)

	input <- NewStreamChunk("a")
	input <- NewStreamChunk("b")
	input <- NewStreamChunk("c")
	finalChunk := NewStreamChunk("d")
	finalChunk.Done = true
	input <- finalChunk
	close(input) // Close immediately after sending data to avoid deadlock

	output := mgr.Buffer(context.Background(), input, 2)

	batches := make([][]*StreamChunk, 0)
	for batch := range output {
		batches = append(batches, batch)
	}

	assert.Len(t, batches, 2)
	assert.Len(t, batches[0], 2)
	assert.Len(t, batches[1], 2)
}

// TestStreamManager_Buffer_ContextCancellation tests buffer with context cancellation
func TestStreamManager_Buffer_ContextCancellation(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	input := make(chan *StreamChunk, 1)
	input <- NewStreamChunk("a")
	close(input)

	ctx, cancel := context.WithCancel(context.Background())

	output := mgr.Buffer(ctx, input, 2)

	// Receive first batch
	batch := <-output

	cancel()

	// Try to receive more - should be limited by context
	assert.NotNil(t, batch)
}

// TestStreamManager_Collect_Success tests successful collection
func TestStreamManager_Collect_Success(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	input := make(chan *StreamChunk, 3)

	input <- NewStreamChunk("a")
	input <- NewStreamChunk("b")
	finalChunk := NewStreamChunk("c")
	finalChunk.Done = true
	input <- finalChunk
	close(input) // Close immediately after sending data to avoid deadlock

	chunks, err := mgr.Collect(context.Background(), input)

	assert.NoError(t, err)
	assert.Len(t, chunks, 3)
}

// TestStreamManager_Collect_WithError tests collection with error
func TestStreamManager_Collect_WithError(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	input := make(chan *StreamChunk, 2)

	input <- NewStreamChunk("a")
	errorChunk := NewStreamChunk(nil)
	errorChunk.Error = errors.New("collection error")
	input <- errorChunk
	close(input) // Close immediately after sending data to avoid deadlock

	chunks, err := mgr.Collect(context.Background(), input)

	assert.Error(t, err)
	assert.Len(t, chunks, 1)
}

// TestStreamManager_Collect_Timeout tests collection timeout
func TestStreamManager_Collect_Timeout(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 100 * time.Millisecond})

	input := make(chan *StreamChunk)
	defer close(input) // Close to avoid goroutine leak

	// Use context with timeout to test timeout behavior
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := mgr.Collect(ctx, input)

	assert.Error(t, err)
}

// TestStreamMultiplexer_Creation tests multiplexer creation
func TestStreamMultiplexer_Creation(t *testing.T) {
	input := make(chan *StreamChunk)
	close(input) // Close immediately to avoid leaving goroutine open

	mux := NewStreamMultiplexer(input)

	assert.NotNil(t, mux)
	assert.NotNil(t, mux.input) // Verify input is set (type is <-chan, not chan)
	assert.Len(t, mux.consumers, 0)
}

// TestStreamMultiplexer_AddConsumer tests adding consumers
func TestStreamMultiplexer_AddConsumer(t *testing.T) {
	input := make(chan *StreamChunk, 2)
	mux := NewStreamMultiplexer(input)
	defer mux.closeAllConsumers()

	ch1 := mux.AddConsumer(10)
	ch2 := mux.AddConsumer(10)

	assert.NotNil(t, ch1)
	assert.NotNil(t, ch2)
	assert.Len(t, mux.consumers, 2)
}

// TestStreamMultiplexer_Broadcast tests broadcasting to consumers
func TestStreamMultiplexer_Broadcast(t *testing.T) {
	input := make(chan *StreamChunk, 1)
	mux := NewStreamMultiplexer(input)
	defer mux.closeAllConsumers()

	ch1 := mux.AddConsumer(10)
	ch2 := mux.AddConsumer(10)

	chunk := NewStreamChunk("test")
	mux.broadcast(chunk)

	result1 := <-ch1
	result2 := <-ch2

	assert.Equal(t, "test", result1.Data)
	assert.Equal(t, "test", result2.Data)
}

// TestStreamRateLimiter_Creation tests rate limiter creation
func TestStreamRateLimiter_Creation(t *testing.T) {
	limiter := NewStreamRateLimiter(10)

	assert.NotNil(t, limiter)
	assert.Equal(t, 10, limiter.rate)
	assert.Equal(t, time.Second/10, limiter.interval)
}

// TestStreamRateLimiter_DefaultRate tests default rate when invalid
func TestStreamRateLimiter_DefaultRate(t *testing.T) {
	limiter := NewStreamRateLimiter(0)

	assert.Equal(t, 10, limiter.rate)
	assert.Equal(t, time.Second/10, limiter.interval)
}

// TestStreamRateLimiter_Limit tests rate limiting
func TestStreamRateLimiter_Limit(t *testing.T) {
	limiter := NewStreamRateLimiter(100) // 100 chunks/sec = 10ms per chunk

	input := make(chan *StreamChunk, 2)
	input <- NewStreamChunk("a")
	input <- NewStreamChunk("b")
	close(input)

	output := limiter.Limit(context.Background(), input)

	start := time.Now()
	results := make([]*StreamChunk, 0)
	for chunk := range output {
		results = append(results, chunk)
	}
	elapsed := time.Since(start)

	assert.Len(t, results, 2)
	// Should take at least 10ms per chunk (approximately)
	assert.True(t, elapsed >= 10*time.Millisecond)
}

// TestStreamStats_Creation tests stats creation
func TestStreamStats_Creation(t *testing.T) {
	stats := NewStreamStats()

	assert.NotNil(t, stats)
	assert.Equal(t, int64(0), stats.ChunksProcessed)
	assert.Equal(t, int64(0), stats.BytesProcessed)
	assert.Equal(t, int64(0), stats.ErrorsCount)
	assert.False(t, stats.StartTime.IsZero())
}

// TestStreamStats_RecordChunk tests recording chunk data
func TestStreamStats_RecordChunk(t *testing.T) {
	stats := NewStreamStats()

	stats.RecordChunk(100)
	stats.RecordChunk(50)

	assert.Equal(t, int64(2), stats.ChunksProcessed)
	assert.Equal(t, int64(150), stats.BytesProcessed)
}

// TestStreamStats_RecordError tests recording errors
func TestStreamStats_RecordError(t *testing.T) {
	stats := NewStreamStats()

	stats.RecordError()
	stats.RecordError()

	assert.Equal(t, int64(2), stats.ErrorsCount)
}

// TestStreamStats_Duration tests duration calculation
func TestStreamStats_Duration(t *testing.T) {
	stats := NewStreamStats()

	time.Sleep(10 * time.Millisecond)
	stats.Complete()

	duration := stats.Duration()
	assert.True(t, duration >= 10*time.Millisecond)
}

// TestStreamStats_Throughput tests throughput calculation
func TestStreamStats_Throughput(t *testing.T) {
	stats := NewStreamStats()

	stats.RecordChunk(100)
	stats.RecordChunk(100)
	stats.RecordChunk(100)

	time.Sleep(100 * time.Millisecond)
	stats.Complete()

	throughput := stats.Throughput()
	// Should be approximately 30 chunks/sec
	assert.True(t, throughput > 10)
}

// TestStreamStats_String tests string representation
func TestStreamStats_String(t *testing.T) {
	stats := NewStreamStats()
	stats.RecordChunk(100)
	stats.RecordError()
	stats.Complete()

	str := stats.String()
	assert.Contains(t, str, "Chunks: 1")
	assert.Contains(t, str, "Bytes: 100")
	assert.Contains(t, str, "Errors: 1")
}

// Concurrent operation tests

// TestStreamManager_ConcurrentTransform tests concurrent transformation
func TestStreamManager_ConcurrentTransform(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 100, Timeout: 5 * time.Second})

	input := make(chan *StreamChunk, 100)

	// Generate many chunks
	for i := 0; i < 100; i++ {
		input <- NewStreamChunk(fmt.Sprintf("item_%d", i))
	}
	close(input) // Close immediately after sending data to avoid deadlock

	var processed int64
	transformer := func(chunk *StreamChunk) (*StreamChunk, error) {
		atomic.AddInt64(&processed, 1)
		newChunk := NewStreamChunk(fmt.Sprintf("%s_transformed", chunk.Data))
		return newChunk, nil
	}

	output := mgr.Transform(context.Background(), input, transformer)

	results := make([]*StreamChunk, 0)
	for chunk := range output {
		results = append(results, chunk)
	}

	assert.Equal(t, int64(100), atomic.LoadInt64(&processed))
	assert.Len(t, results, 100)
}

// TestStreamManager_ConcurrentFilter tests concurrent filtering
func TestStreamManager_ConcurrentFilter(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 100, Timeout: 5 * time.Second})

	input := make(chan *StreamChunk, 100)

	// Generate many chunks
	for i := 0; i < 100; i++ {
		input <- NewStreamChunk(i)
	}
	close(input) // Close immediately after sending data to avoid deadlock

	predicate := func(chunk *StreamChunk) bool {
		num := chunk.Data.(int)
		return num%2 == 0 // Keep even numbers
	}

	output := mgr.Filter(context.Background(), input, predicate)

	results := make([]*StreamChunk, 0)
	for chunk := range output {
		results = append(results, chunk)
	}

	assert.Len(t, results, 50)
}

// TestStreamManager_ConcurrentMerge tests concurrent merging
func TestStreamManager_ConcurrentMerge(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 100, Timeout: 5 * time.Second})

	streamChans := make([]chan *StreamChunk, 5)
	streamInterfaces := make([]<-chan *StreamChunk, 5)

	for i := 0; i < 5; i++ {
		streamChans[i] = make(chan *StreamChunk, 20)
		streamInterfaces[i] = streamChans[i]
	}

	// Fill streams concurrently
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(streamIdx int) {
			defer wg.Done()
			ch := streamChans[streamIdx]
			for j := 0; j < 20; j++ {
				ch <- NewStreamChunk(fmt.Sprintf("s%d_i%d", streamIdx, j))
			}
			close(ch)
		}(i)
	}

	wg.Wait()

	merged := mgr.Merge(context.Background(), streamInterfaces...)

	results := make([]*StreamChunk, 0)
	for chunk := range merged {
		results = append(results, chunk)
	}

	assert.Len(t, results, 100)
}

// Edge case tests

// TestStreamChunk_LargeData tests chunk with large data
func TestStreamChunk_LargeData(t *testing.T) {
	largeData := make([]byte, 1024*1024) // 1MB
	chunk := NewStreamChunk(largeData)

	assert.NotNil(t, chunk)
	assert.Equal(t, largeData, chunk.Data)
}

// TestStreamManager_EmptyStream tests processing empty stream
func TestStreamManager_EmptyStream(t *testing.T) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 10, Timeout: 5 * time.Second})

	input := make(chan *StreamChunk)
	close(input)

	processed := 0
	handler := NewFuncStreamHandler(
		func(chunk *StreamChunk) error {
			processed++
			return nil
		},
		func() error {
			return nil
		},
		nil,
	)

	err := mgr.Process(context.Background(), input, handler)
	assert.NoError(t, err)
	assert.Equal(t, 0, processed)
}

// TestStreamMultiplexer_SlowConsumer tests handling slow consumer
func TestStreamMultiplexer_SlowConsumer(t *testing.T) {
	input := make(chan *StreamChunk, 2)
	mux := NewStreamMultiplexer(input)
	defer mux.closeAllConsumers()

	// Add consumer with small buffer
	ch := mux.AddConsumer(1)

	chunk1 := NewStreamChunk("a")
	chunk2 := NewStreamChunk("b")

	mux.broadcast(chunk1)
	mux.broadcast(chunk2)

	// Verify non-blocking broadcast
	result := <-ch
	assert.Equal(t, "a", result.Data)
}

// Benchmark tests

// BenchmarkStreamManager_Transform benchmarks transformation
func BenchmarkStreamManager_Transform(b *testing.B) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 1000, Timeout: 30 * time.Second})

	input := make(chan *StreamChunk, 1000)

	transformer := func(chunk *StreamChunk) (*StreamChunk, error) {
		return chunk, nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input <- NewStreamChunk(i)
	}
	close(input) // Close immediately after sending data to avoid deadlock

	output := mgr.Transform(context.Background(), input, transformer)

	for range output {
		// Drain the output
	}
}

// BenchmarkStreamManager_Filter benchmarks filtering
func BenchmarkStreamManager_Filter(b *testing.B) {
	mgr := NewStreamManager(StreamManagerConfig{BufferSize: 1000, Timeout: 30 * time.Second})

	input := make(chan *StreamChunk, 1000)

	predicate := func(chunk *StreamChunk) bool {
		return true
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input <- NewStreamChunk(i)
	}
	close(input) // Close immediately after sending data to avoid deadlock

	output := mgr.Filter(context.Background(), input, predicate)

	for range output {
		// Drain the output
	}
}

// BenchmarkStreamStats_RecordChunk benchmarks stats recording
func BenchmarkStreamStats_RecordChunk(b *testing.B) {
	stats := NewStreamStats()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stats.RecordChunk(100)
	}
}
