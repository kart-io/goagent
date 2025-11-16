package stream

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kart-io/goagent/core"
)

// TestRingBuffer_Creation tests buffer creation
func TestRingBuffer_Creation(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		expected int
	}{
		{
			name:     "default size when invalid",
			size:     0,
			expected: 100,
		},
		{
			name:     "negative size defaults",
			size:     -1,
			expected: 100,
		},
		{
			name:     "custom size",
			size:     50,
			expected: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := NewRingBuffer(tt.size)
			assert.Equal(t, tt.expected, rb.Size())
			assert.True(t, rb.IsEmpty())
		})
	}
}

// TestRingBuffer_Push_Single tests pushing single element
func TestRingBuffer_Push_Single(t *testing.T) {
	rb := NewRingBuffer(10)

	chunk := core.NewTextChunk("test")
	result := rb.Push(chunk)

	assert.True(t, result)
	assert.Equal(t, 1, rb.Count())
	assert.False(t, rb.IsEmpty())
}

// TestRingBuffer_Push_Multiple tests pushing multiple elements
func TestRingBuffer_Push_Multiple(t *testing.T) {
	rb := NewRingBuffer(10)

	for i := 0; i < 5; i++ {
		chunk := core.NewTextChunk("test")
		result := rb.Push(chunk)
		assert.True(t, result)
	}

	assert.Equal(t, 5, rb.Count())
	assert.False(t, rb.IsFull())
}

// TestRingBuffer_Push_Full tests filling buffer to capacity
func TestRingBuffer_Push_Full(t *testing.T) {
	rb := NewRingBuffer(5)

	for i := 0; i < 5; i++ {
		chunk := core.NewTextChunk("test")
		result := rb.Push(chunk)
		assert.True(t, result)
	}

	assert.True(t, rb.IsFull())
	assert.Equal(t, 5, rb.Count())
}

// TestRingBuffer_Push_Overflow tests pushing beyond capacity
func TestRingBuffer_Push_Overflow(t *testing.T) {
	rb := NewRingBuffer(3)

	// Fill buffer
	for i := 0; i < 3; i++ {
		rb.Push(core.NewTextChunk("test"))
	}

	assert.True(t, rb.IsFull())

	// Push beyond capacity - should return false (overwrite)
	result := rb.Push(core.NewTextChunk("overflow"))
	assert.False(t, result)
	assert.Equal(t, 3, rb.Count()) // Count stays at max
}

// TestRingBuffer_Pop_Success tests popping elements
func TestRingBuffer_Pop_Success(t *testing.T) {
	rb := NewRingBuffer(10)

	chunk := core.NewTextChunk("test")
	rb.Push(chunk)

	popped := rb.Pop()

	assert.NotNil(t, popped)
	assert.Equal(t, "test", popped.Text)
	assert.True(t, rb.IsEmpty())
}

// TestRingBuffer_Pop_Empty tests popping from empty buffer
func TestRingBuffer_Pop_Empty(t *testing.T) {
	rb := NewRingBuffer(10)

	popped := rb.Pop()

	assert.Nil(t, popped)
	assert.True(t, rb.IsEmpty())
}

// TestRingBuffer_Pop_Multiple tests popping multiple elements
func TestRingBuffer_Pop_Multiple(t *testing.T) {
	rb := NewRingBuffer(10)

	for i := 0; i < 3; i++ {
		chunk := core.NewTextChunk("test")
		rb.Push(chunk)
	}

	for i := 0; i < 3; i++ {
		popped := rb.Pop()
		assert.NotNil(t, popped)
	}

	assert.True(t, rb.IsEmpty())
}

// TestRingBuffer_Peek_Success tests peeking element
func TestRingBuffer_Peek_Success(t *testing.T) {
	rb := NewRingBuffer(10)

	chunk := core.NewTextChunk("test")
	rb.Push(chunk)

	peeked := rb.Peek()

	assert.NotNil(t, peeked)
	assert.Equal(t, "test", peeked.Text)
	assert.Equal(t, 1, rb.Count()) // Count unchanged
}

// TestRingBuffer_Peek_Empty tests peeking empty buffer
func TestRingBuffer_Peek_Empty(t *testing.T) {
	rb := NewRingBuffer(10)

	peeked := rb.Peek()

	assert.Nil(t, peeked)
}

// TestRingBuffer_Clear tests clearing buffer
func TestRingBuffer_Clear(t *testing.T) {
	rb := NewRingBuffer(10)

	for i := 0; i < 5; i++ {
		rb.Push(core.NewTextChunk("test"))
	}

	assert.Equal(t, 5, rb.Count())

	rb.Clear()

	assert.True(t, rb.IsEmpty())
	assert.Equal(t, 0, rb.Count())
}

// TestRingBuffer_ToSlice tests converting to slice
func TestRingBuffer_ToSlice(t *testing.T) {
	rb := NewRingBuffer(10)

	for i := 0; i < 3; i++ {
		rb.Push(core.NewTextChunk("test"))
	}

	slice := rb.ToSlice()

	assert.Len(t, slice, 3)
	for i := 0; i < 3; i++ {
		assert.NotNil(t, slice[i])
	}
}

// TestRingBuffer_ToSlice_Empty tests converting empty buffer to slice
func TestRingBuffer_ToSlice_Empty(t *testing.T) {
	rb := NewRingBuffer(10)

	slice := rb.ToSlice()

	assert.Nil(t, slice)
}

// TestRingBuffer_Resize_Expand tests resizing buffer to larger size
func TestRingBuffer_Resize_Expand(t *testing.T) {
	rb := NewRingBuffer(5)

	for i := 0; i < 3; i++ {
		rb.Push(core.NewTextChunk("test"))
	}

	rb.Resize(10)

	assert.Equal(t, 10, rb.Size())
	assert.Equal(t, 3, rb.Count())
}

// TestRingBuffer_Resize_Shrink tests resizing buffer to smaller size
func TestRingBuffer_Resize_Shrink(t *testing.T) {
	rb := NewRingBuffer(10)

	for i := 0; i < 5; i++ {
		rb.Push(core.NewTextChunk("test"))
	}

	rb.Resize(3)

	assert.Equal(t, 3, rb.Size())
	assert.Equal(t, 3, rb.Count()) // Only 3 fit in new size
}

// TestRingBuffer_Resize_InvalidSize tests resizing with invalid size
func TestRingBuffer_Resize_InvalidSize(t *testing.T) {
	rb := NewRingBuffer(10)

	originalSize := rb.Size()
	rb.Resize(0)

	assert.Equal(t, originalSize, rb.Size())
}

// TestRingBuffer_Usage tests usage calculation
func TestRingBuffer_Usage(t *testing.T) {
	rb := NewRingBuffer(10)

	assert.Equal(t, 0.0, rb.Usage())

	rb.Push(core.NewTextChunk("test"))
	assert.Equal(t, 0.1, rb.Usage())

	for i := 0; i < 9; i++ {
		rb.Push(core.NewTextChunk("test"))
	}
	assert.Equal(t, 1.0, rb.Usage())
}

// TestRingBuffer_Circular tests circular wrapping
func TestRingBuffer_Circular(t *testing.T) {
	rb := NewRingBuffer(3)

	// Fill and overflow
	rb.Push(core.NewTextChunk("a"))
	rb.Push(core.NewTextChunk("b"))
	rb.Push(core.NewTextChunk("c"))
	rb.Push(core.NewTextChunk("d")) // Overflows, wraps

	popped := rb.Pop()
	assert.Equal(t, "b", popped.Text) // b should be first now

	popped = rb.Pop()
	assert.Equal(t, "c", popped.Text)

	popped = rb.Pop()
	assert.Equal(t, "d", popped.Text)
}

// Reader tests

// TestReader_Creation tests reader creation
func TestReader_Creation(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 10)
	defer close(ch)

	ctx := context.Background()
	opts := core.DefaultStreamOptions()

	reader := NewReader(ctx, ch, opts)

	assert.NotNil(t, reader)
	assert.False(t, reader.IsClosed())
}

// TestReader_Next_Success tests reading next chunk
func TestReader_Next_Success(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 1)
	defer close(ch)

	ch <- core.NewTextChunk("test data")

	reader := NewReader(context.Background(), ch, core.DefaultStreamOptions())
	defer reader.Close()

	chunk, err := reader.Next()

	assert.NoError(t, err)
	assert.NotNil(t, chunk)
	assert.Equal(t, "test data", chunk.Text)
}

// TestReader_Next_EOF tests EOF handling
func TestReader_Next_EOF(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk)
	close(ch)

	reader := NewReader(context.Background(), ch, core.DefaultStreamOptions())
	defer reader.Close()

	chunk, err := reader.Next()

	assert.Equal(t, io.EOF, err)
	assert.Nil(t, chunk)
}

// TestReader_Close tests closing reader
func TestReader_Close(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk)
	defer close(ch)

	reader := NewReader(context.Background(), ch, core.DefaultStreamOptions())

	err := reader.Close()

	assert.NoError(t, err)
	assert.True(t, reader.IsClosed())
}

// TestReader_Close_Idempotent tests that close is idempotent
func TestReader_Close_Idempotent(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk)
	defer close(ch)

	reader := NewReader(context.Background(), ch, core.DefaultStreamOptions())

	err1 := reader.Close()
	err2 := reader.Close()

	assert.NoError(t, err1)
	assert.Error(t, err2)
}

// TestReader_Stats tests statistics tracking
func TestReader_Stats(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 2)
	defer close(ch)

	ch <- core.NewTextChunk("chunk1")
	ch <- core.NewTextChunk("chunk2")

	reader := NewReader(context.Background(), ch, core.DefaultStreamOptions())
	defer reader.Close()

	reader.Next()
	reader.Next()

	stats := reader.Stats()

	assert.Equal(t, int64(2), stats.ChunksRead)
	assert.True(t, stats.BytesRead > 0)
}

// TestReader_Collect tests collecting all chunks
func TestReader_Collect(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 3)
	defer close(ch)

	ch <- core.NewTextChunk("a")
	ch <- core.NewTextChunk("b")
	finalChunk := core.NewTextChunk("c")
	finalChunk.IsLast = true
	ch <- finalChunk

	reader := NewReader(context.Background(), ch, core.DefaultStreamOptions())
	defer reader.Close()

	chunks, err := reader.Collect()

	assert.NoError(t, err)
	assert.Len(t, chunks, 3)
}

// TestReader_CollectText tests collecting text chunks
func TestReader_CollectText(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 3)
	defer close(ch)

	ch <- core.NewTextChunk("hello")
	ch <- core.NewTextChunk(" ")
	finalChunk := core.NewTextChunk("world")
	finalChunk.IsLast = true
	ch <- finalChunk

	reader := NewReader(context.Background(), ch, core.DefaultStreamOptions())
	defer reader.Close()

	text, err := reader.CollectText()

	assert.NoError(t, err)
	assert.Equal(t, "hello world", text)
}

// Writer tests

// TestWriter_Creation tests writer creation
func TestWriter_Creation(t *testing.T) {
	ctx := context.Background()
	opts := core.DefaultStreamOptions()

	writer := NewWriter(ctx, opts)

	assert.NotNil(t, writer)
	assert.False(t, writer.IsClosed())
	assert.NotNil(t, writer.Channel())
}

// TestWriter_WriteChunk tests writing chunk
func TestWriter_WriteChunk(t *testing.T) {
	writer := NewWriter(context.Background(), core.DefaultStreamOptions())
	defer func() { _ = writer.Close() }()

	chunk := core.NewTextChunk("test")
	err := writer.WriteChunk(chunk)

	assert.NoError(t, err)
	assert.Equal(t, int64(1), chunk.Metadata.Sequence)
}

// TestWriter_Write tests io.Writer interface
func TestWriter_Write(t *testing.T) {
	writer := NewWriter(context.Background(), core.DefaultStreamOptions())
	defer func() { _ = writer.Close() }()

	data := []byte("test data")
	n, err := writer.Write(data)

	assert.NoError(t, err)
	assert.Equal(t, len(data), n)
}

// TestWriter_WriteText tests writing text
func TestWriter_WriteText(t *testing.T) {
	writer := NewWriter(context.Background(), core.DefaultStreamOptions())
	defer func() { _ = writer.Close() }()

	err := writer.WriteText("hello")

	assert.NoError(t, err)
}

// TestWriter_WriteProgress tests writing progress
func TestWriter_WriteProgress(t *testing.T) {
	writer := NewWriter(context.Background(), core.DefaultStreamOptions())
	defer func() { _ = writer.Close() }()

	err := writer.WriteProgress(50.0, "Half done")

	assert.NoError(t, err)
}

// TestWriter_WriteStatus tests writing status
func TestWriter_WriteStatus(t *testing.T) {
	writer := NewWriter(context.Background(), core.DefaultStreamOptions())
	defer func() { _ = writer.Close() }()

	err := writer.WriteStatus("processing")

	assert.NoError(t, err)
}

// TestWriter_WriteError tests writing error
func TestWriter_WriteError(t *testing.T) {
	writer := NewWriter(context.Background(), core.DefaultStreamOptions())
	defer func() { _ = writer.Close() }()

	err := writer.WriteError(errors.New("test error"))

	assert.NoError(t, err)
}

// TestWriter_WriteBatch tests batch writing
func TestWriter_WriteBatch(t *testing.T) {
	writer := NewWriter(context.Background(), core.DefaultStreamOptions())
	defer func() { _ = writer.Close() }()

	chunks := []*core.LegacyStreamChunk{
		core.NewTextChunk("a"),
		core.NewTextChunk("b"),
		core.NewTextChunk("c"),
	}

	err := writer.WriteBatch(chunks)

	assert.NoError(t, err)
}

// TestWriter_Close tests closing writer
func TestWriter_Close(t *testing.T) {
	writer := NewWriter(context.Background(), core.DefaultStreamOptions())

	err := writer.Close()

	assert.NoError(t, err)
	assert.True(t, writer.IsClosed())
}

// TestWriter_WriteAfterClose tests error when writing after close
func TestWriter_WriteAfterClose(t *testing.T) {
	writer := NewWriter(context.Background(), core.DefaultStreamOptions())
	_ = writer.Close()

	chunk := core.NewTextChunk("test")
	err := writer.WriteChunk(chunk)

	assert.Error(t, err)
}

// TestWriter_Stats tests statistics
func TestWriter_Stats(t *testing.T) {
	writer := NewWriter(context.Background(), core.DefaultStreamOptions())
	defer func() { _ = writer.Close() }()

	writer.WriteText("test1")
	writer.WriteText("test2")

	stats := writer.Stats()

	assert.Equal(t, int64(2), stats.ChunksWritten)
	assert.True(t, stats.BytesWritten > 0)
}

// Integration tests

// TestReaderWriter_Integration tests reader-writer integration
func TestReaderWriter_Integration(t *testing.T) {
	writer := NewWriter(context.Background(), core.DefaultStreamOptions())

	reader := NewReader(context.Background(), writer.Channel(), core.DefaultStreamOptions())
	defer reader.Close()

	go func() {
		writer.WriteText("message1")
		writer.WriteText("message2")
		_ = writer.Close()
	}()

	text, err := reader.CollectText()

	assert.NoError(t, err)
	assert.Equal(t, "message1message2", text)
}

// TestReaderWithBuffer tests reader with buffer enabled
func TestReaderWithBuffer(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 3)

	opts := core.DefaultStreamOptions()
	opts.EnableBuffer = true
	opts.BufferSize = 10

	reader := NewReader(context.Background(), ch, opts)
	defer reader.Close()

	ch <- core.NewTextChunk("a")
	ch <- core.NewTextChunk("b")
	finalChunk := core.NewTextChunk("c")
	finalChunk.IsLast = true
	ch <- finalChunk

	chunks, err := reader.Collect()

	assert.NoError(t, err)
	assert.Len(t, chunks, 3)
}

// TestReaderTimeout tests reader timeout
func TestReaderTimeout(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk)

	opts := core.DefaultStreamOptions()
	opts.ChunkTimeout = 100 * time.Millisecond

	reader := NewReader(context.Background(), ch, opts)
	defer reader.Close()

	start := time.Now()
	_, err := reader.Next()
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.True(t, elapsed >= 100*time.Millisecond)
}

// TestWriterTimeout tests writer timeout
func TestWriterTimeout(t *testing.T) {
	opts := core.DefaultStreamOptions()
	opts.ChunkTimeout = 100 * time.Millisecond

	writer := NewWriter(context.Background(), opts)
	defer func() { _ = writer.Close() }()

	// Don't read from channel to cause blocking
	chunk := core.NewTextChunk("test")
	chunk.Metadata.Sequence = 1
	chunk.Metadata.Timestamp = time.Now()

	// The channel buffer is limited, so this should eventually timeout
	_ = writer.WriteChunk(chunk)
}

// TestReaderErrorHandling tests reader error handling
func TestReaderErrorHandling(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 2)
	defer close(ch)

	ch <- core.NewTextChunk("good")
	errorChunk := core.NewErrorChunk(errors.New("test error"))
	ch <- errorChunk

	reader := NewReader(context.Background(), ch, core.DefaultStreamOptions())
	defer reader.Close()

	chunk1, _ := reader.Next()
	assert.Equal(t, "good", chunk1.Text)

	chunk2, err := reader.Next()

	assert.Error(t, err)
	assert.Nil(t, chunk2)
}

// TestRingBuffer_ThreadSafety tests concurrent access
func TestRingBuffer_ThreadSafety(t *testing.T) {
	rb := NewRingBuffer(1000)

	done := make(chan bool)

	// Producer goroutine
	go func() {
		for i := 0; i < 500; i++ {
			rb.Push(core.NewTextChunk("test"))
		}
		done <- true
	}()

	// Consumer goroutine
	go func() {
		for i := 0; i < 500; i++ {
			rb.Pop()
		}
		done <- true
	}()

	<-done
	<-done

	assert.True(t, rb.IsEmpty() || rb.Count() > 0)
}

// Benchmark tests

// BenchmarkRingBuffer_Push benchmarks push operation
func BenchmarkRingBuffer_Push(b *testing.B) {
	rb := NewRingBuffer(10000)
	chunk := core.NewTextChunk("test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Push(chunk)
	}
}

// BenchmarkRingBuffer_Pop benchmarks pop operation
func BenchmarkRingBuffer_Pop(b *testing.B) {
	rb := NewRingBuffer(10000)
	for i := 0; i < 10000; i++ {
		rb.Push(core.NewTextChunk("test"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Pop()
	}
}

// BenchmarkReader_Next benchmarks reading
func BenchmarkReader_Next(b *testing.B) {
	ch := make(chan *core.LegacyStreamChunk, 10000)
	for i := 0; i < 10000; i++ {
		ch <- core.NewTextChunk("test")
	}
	close(ch)

	reader := NewReader(context.Background(), ch, core.DefaultStreamOptions())
	defer reader.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Next()
	}
}

// BenchmarkWriter_WriteChunk benchmarks writing
func BenchmarkWriter_WriteChunk(b *testing.B) {
	writer := NewWriter(context.Background(), core.DefaultStreamOptions())
	defer func() { _ = writer.Close() }()

	chunk := core.NewTextChunk("test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		writer.WriteChunk(chunk)
	}
}
