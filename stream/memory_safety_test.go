package stream

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
)

// TestReader_Collect_SizeLimitExceeded tests that Collect respects MaxCollectSize
func TestReader_Collect_SizeLimitExceeded(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 10)

	// Create options with small limit
	opts := core.DefaultStreamOptions()
	opts.MaxCollectSize = 500 // 500 bytes limit (accounting for overhead)

	// Send chunks that exceed the limit in background goroutine
	// Each chunk: 30 bytes text + 256 bytes overhead = ~286 bytes
	// 2 chunks = ~572 bytes > 500 bytes limit
	go func() {
		defer close(ch)
		for i := 0; i < 3; i++ {
			ch <- core.NewTextChunk(strings.Repeat("x", 30))
		}
	}()

	reader := NewReader(context.Background(), ch, opts)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	chunks, err := reader.Collect()

	// Should get error due to size limit
	require.Error(t, err)
	assert.True(t, agentErrors.IsCode(err, agentErrors.CodeStreamRead))
	assert.Contains(t, err.Error(), "collect size limit exceeded")

	// Should have collected at least one chunk before hitting limit
	assert.NotEmpty(t, chunks)
	assert.Less(t, len(chunks), 3)
}

// TestReader_Collect_WithinLimit tests Collect works when under limit
func TestReader_Collect_WithinLimit(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 3)
	defer close(ch)

	opts := core.DefaultStreamOptions()
	opts.MaxCollectSize = 1000 // 1KB limit

	// Send chunks within limit
	ch <- core.NewTextChunk("hello")
	ch <- core.NewTextChunk("world")
	finalChunk := core.NewTextChunk("!")
	finalChunk.IsLast = true
	ch <- finalChunk

	reader := NewReader(context.Background(), ch, opts)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	chunks, err := reader.Collect()

	require.NoError(t, err)
	assert.Len(t, chunks, 3)
}

// TestReader_Collect_DefaultLimit tests default limit when not specified
func TestReader_Collect_DefaultLimit(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 2)
	defer close(ch)

	opts := core.DefaultStreamOptions()
	opts.MaxCollectSize = 0 // Should use default 100MB

	ch <- core.NewTextChunk("test")
	finalChunk := core.NewTextChunk("data")
	finalChunk.IsLast = true
	ch <- finalChunk

	reader := NewReader(context.Background(), ch, opts)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	chunks, err := reader.Collect()

	require.NoError(t, err)
	assert.Len(t, chunks, 2)
}

// TestReader_CollectText_SizeLimitExceeded tests that CollectText respects MaxCollectSize
func TestReader_CollectText_SizeLimitExceeded(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 10)

	opts := core.DefaultStreamOptions()
	opts.MaxCollectSize = 50 // 50 bytes limit

	// Send text chunks that exceed limit in background goroutine
	go func() {
		defer close(ch)
		for i := 0; i < 5; i++ {
			ch <- core.NewTextChunk(strings.Repeat("a", 20)) // 20 bytes each, total 100 > 50
		}
	}()

	reader := NewReader(context.Background(), ch, opts)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	text, err := reader.CollectText()

	// Should get error due to size limit
	require.Error(t, err)
	assert.True(t, agentErrors.IsCode(err, agentErrors.CodeStreamRead))
	assert.Contains(t, err.Error(), "collect text size limit exceeded")

	// Should have collected some text before hitting limit
	assert.NotEmpty(t, text)
	assert.Less(t, len(text), 100)
}

// TestReader_CollectText_WithinLimit tests CollectText works when under limit
func TestReader_CollectText_WithinLimit(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 3)
	defer close(ch)

	opts := core.DefaultStreamOptions()
	opts.MaxCollectSize = 1000

	ch <- core.NewTextChunk("Hello, ")
	ch <- core.NewTextChunk("World")
	finalChunk := core.NewTextChunk("!")
	finalChunk.IsLast = true
	ch <- finalChunk

	reader := NewReader(context.Background(), ch, opts)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	text, err := reader.CollectText()

	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", text)
}

// TestReader_CollectText_UsesBuilder tests that CollectText uses strings.Builder efficiently
func TestReader_CollectText_UsesBuilder(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 101) // Buffer for 100 chunks + final chunk
	defer close(ch)

	opts := core.DefaultStreamOptions()

	// Send many small chunks to test builder efficiency
	for i := 0; i < 100; i++ {
		ch <- core.NewTextChunk("x")
	}
	finalChunk := core.NewTextChunk("")
	finalChunk.IsLast = true
	ch <- finalChunk

	reader := NewReader(context.Background(), ch, opts)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	start := time.Now()
	text, err := reader.CollectText()
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Len(t, text, 100)

	// Should be fast (less than 100ms for 100 chunks)
	assert.Less(t, elapsed, 100*time.Millisecond)
}

// TestReader_CollectText_IgnoresNonTextChunks tests that CollectText only collects text chunks
func TestReader_CollectText_IgnoresNonTextChunks(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 4)
	defer close(ch)

	opts := core.DefaultStreamOptions()

	ch <- core.NewTextChunk("text1")
	ch <- core.NewProgressChunk(50, "progress")
	ch <- core.NewTextChunk("text2")
	finalChunk := core.NewTextChunk("")
	finalChunk.IsLast = true
	ch <- finalChunk

	reader := NewReader(context.Background(), ch, opts)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	text, err := reader.CollectText()

	require.NoError(t, err)
	assert.Equal(t, "text1text2", text)
}

// TestReader_ContextCancellation tests that reader respects context cancellation
func TestReader_ContextCancellation(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk)
	defer close(ch)

	ctx, cancel := context.WithCancel(context.Background())
	opts := core.DefaultStreamOptions()

	reader := NewReader(ctx, ch, opts)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	// Cancel context before reading
	cancel()

	chunk, err := reader.Next()

	assert.Error(t, err)
	assert.Nil(t, chunk)
	assert.Equal(t, context.Canceled, err)
}

// TestReader_ContextTimeout tests that reader respects context timeout
func TestReader_ContextTimeout(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk)
	defer close(ch)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	opts := core.DefaultStreamOptions()
	opts.ChunkTimeout = 0 // Disable chunk timeout to test context timeout

	reader := NewReader(ctx, ch, opts)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	// Try to read without sending data - should timeout
	chunk, err := reader.Next()

	assert.Error(t, err)
	assert.Nil(t, chunk)
	assert.Equal(t, context.DeadlineExceeded, err)
}

// TestTextAccumulatorConsumer_SizeLimitExceeded tests size limit in TextAccumulatorConsumer
func TestTextAccumulatorConsumer_SizeLimitExceeded(t *testing.T) {
	consumer := NewTextAccumulatorConsumerWithLimit(50) // 50 bytes limit

	err := consumer.OnStart()
	require.NoError(t, err)

	// Add text within limit
	chunk1 := core.NewTextChunk(strings.Repeat("x", 30))
	err = consumer.OnChunk(chunk1)
	require.NoError(t, err)

	// Add text that exceeds limit
	chunk2 := core.NewTextChunk(strings.Repeat("y", 30))
	err = consumer.OnChunk(chunk2)

	require.Error(t, err)
	assert.True(t, agentErrors.IsCode(err, agentErrors.CodeStreamRead))
	assert.Contains(t, err.Error(), "text accumulator size limit exceeded")

	// Text should contain only first chunk
	text := consumer.Text()
	assert.Len(t, text, 30)
}

// TestTextAccumulatorConsumer_WithinLimit tests normal operation within limit
func TestTextAccumulatorConsumer_WithinLimit(t *testing.T) {
	consumer := NewTextAccumulatorConsumerWithLimit(1000)

	err := consumer.OnStart()
	require.NoError(t, err)

	chunks := []string{"Hello", ", ", "World", "!"}
	for _, txt := range chunks {
		chunk := core.NewTextChunk(txt)
		err = consumer.OnChunk(chunk)
		require.NoError(t, err)
	}

	text := consumer.Text()
	assert.Equal(t, "Hello, World!", text)
}

// TestTextAccumulatorConsumer_DefaultLimit tests default limit
func TestTextAccumulatorConsumer_DefaultLimit(t *testing.T) {
	consumer := NewTextAccumulatorConsumer()

	err := consumer.OnStart()
	require.NoError(t, err)

	// Add small text - should work fine
	chunk := core.NewTextChunk("test")
	err = consumer.OnChunk(chunk)
	require.NoError(t, err)

	text := consumer.Text()
	assert.Equal(t, "test", text)
}

// TestTextAccumulatorConsumer_Reset tests that OnStart resets the accumulator
func TestTextAccumulatorConsumer_Reset(t *testing.T) {
	consumer := NewTextAccumulatorConsumer()

	chunk := core.NewTextChunk("first")
	err := consumer.OnChunk(chunk)
	require.NoError(t, err)

	assert.Equal(t, "first", consumer.Text())

	// Reset
	err = consumer.OnStart()
	require.NoError(t, err)

	assert.Empty(t, consumer.Text())

	// Add new text
	chunk2 := core.NewTextChunk("second")
	err = consumer.OnChunk(chunk2)
	require.NoError(t, err)

	assert.Equal(t, "second", consumer.Text())
}

// TestTextAccumulatorConsumer_IgnoresNonTextChunks tests ignoring non-text chunks
func TestTextAccumulatorConsumer_IgnoresNonTextChunks(t *testing.T) {
	consumer := NewTextAccumulatorConsumer()

	err := consumer.OnStart()
	require.NoError(t, err)

	// Add text chunk
	textChunk := core.NewTextChunk("hello")
	err = consumer.OnChunk(textChunk)
	require.NoError(t, err)

	// Add non-text chunk
	progressChunk := core.NewProgressChunk(50, "progress")
	err = consumer.OnChunk(progressChunk)
	require.NoError(t, err)

	// Add another text chunk
	textChunk2 := core.NewTextChunk("world")
	err = consumer.OnChunk(textChunk2)
	require.NoError(t, err)

	assert.Equal(t, "helloworld", consumer.Text())
}

// TestEstimateChunkSize tests chunk size estimation
func TestEstimateChunkSize(t *testing.T) {
	tests := []struct {
		name     string
		chunk    *core.LegacyStreamChunk
		minSize  int64
		hasExtra bool
	}{
		{
			name:     "text chunk",
			chunk:    core.NewTextChunk("hello"),
			minSize:  5,
			hasExtra: true,
		},
		{
			name: "binary data chunk",
			chunk: &core.LegacyStreamChunk{
				Type: core.ChunkTypeBinary,
				Data: []byte("binary"),
			},
			minSize:  6,
			hasExtra: true,
		},
		{
			name: "string data chunk",
			chunk: &core.LegacyStreamChunk{
				Type: core.ChunkTypeJSON,
				Data: "string data",
			},
			minSize:  11,
			hasExtra: true,
		},
		{
			name: "empty chunk",
			chunk: &core.LegacyStreamChunk{
				Type: core.ChunkTypeStatus,
			},
			minSize:  0,
			hasExtra: true, // Still has overhead
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := estimateChunkSize(tt.chunk)

			// Should at least include the text/data size
			assert.GreaterOrEqual(t, size, tt.minSize)

			// Should include overhead (256 bytes for metadata)
			if tt.hasExtra {
				assert.GreaterOrEqual(t, size, tt.minSize+256)
			}
		})
	}
}

// TestReader_Collect_LargeChunks tests collecting large chunks
func TestReader_Collect_LargeChunks(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 2)

	opts := core.DefaultStreamOptions()
	opts.MaxCollectSize = 10 * 1024 // 10KB limit

	// Send chunks in background goroutine
	go func() {
		defer close(ch)
		// Create a chunk that fits (3KB text + ~256 bytes overhead = ~3.25KB)
		largeText := strings.Repeat("x", 3*1024)
		ch <- core.NewTextChunk(largeText)

		// Second chunk (8KB text + ~256 bytes overhead = ~8.25KB)
		// Total would be ~11.5KB which exceeds 10KB limit
		largeText2 := strings.Repeat("y", 8*1024)
		ch <- core.NewTextChunk(largeText2)
	}()

	reader := NewReader(context.Background(), ch, opts)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	chunks, err := reader.Collect()

	// Should fail on second chunk (first chunk collected successfully)
	require.Error(t, err)
	assert.True(t, agentErrors.IsCode(err, agentErrors.CodeStreamRead))
	assert.Contains(t, err.Error(), "collect size limit exceeded")

	// Should have collected exactly one chunk
	require.Len(t, chunks, 1)
	assert.Len(t, chunks[0].Text, 3*1024)
}

// TestReader_CollectText_EmptyChunks tests handling empty text chunks
func TestReader_CollectText_EmptyChunks(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 4)
	defer close(ch)

	opts := core.DefaultStreamOptions()

	ch <- core.NewTextChunk("hello")
	ch <- core.NewTextChunk("") // Empty chunk
	ch <- core.NewTextChunk("world")
	finalChunk := core.NewTextChunk("")
	finalChunk.IsLast = true
	ch <- finalChunk

	reader := NewReader(context.Background(), ch, opts)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	text, err := reader.CollectText()

	require.NoError(t, err)
	assert.Equal(t, "helloworld", text)
}

// TestReader_Next_AfterClose tests that Next returns EOF after close
func TestReader_Next_AfterClose(t *testing.T) {
	ch := make(chan *core.LegacyStreamChunk, 1)
	defer close(ch)

	ch <- core.NewTextChunk("test")

	reader := NewReader(context.Background(), ch, core.DefaultStreamOptions())

	// Close immediately
	err := reader.Close()
	require.NoError(t, err)

	// Try to read
	chunk, err := reader.Next()

	assert.Equal(t, io.EOF, err)
	assert.Nil(t, chunk)
}
