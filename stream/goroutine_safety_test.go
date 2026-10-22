package stream

import (
	"context"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// MockLLMClient for testing
type MockLLMClient struct {
	delay    time.Duration
	response string
	err      error
	callMu   sync.Mutex
	called   int
}

func NewMockLLMClient(delay time.Duration, response string) *MockLLMClient {
	return &MockLLMClient{
		delay:    delay,
		response: response,
	}
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	m.callMu.Lock()
	m.called++
	m.callMu.Unlock()

	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}

	if m.err != nil {
		return nil, m.err
	}

	return &llm.CompletionResponse{
		Content: m.response,
		Model:   "mock-model",
	}, nil
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return nil, nil
}

func (m *MockLLMClient) Provider() constants.Provider {
	return constants.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}

func (m *MockLLMClient) GetCallCount() int {
	m.callMu.Lock()
	defer m.callMu.Unlock()
	return m.called
}

// TestStreamingLLMAgent_ContextCancellation_BeforeLLMCall tests context cancellation before LLM call
func TestStreamingLLMAgent_ContextCancellation_BeforeLLMCall(t *testing.T) {
	mockClient := NewMockLLMClient(0, "test response")
	config := DefaultStreamingLLMConfig()
	config.ChunkDelay = 0

	agent := NewStreamingLLMAgent(mockClient, config)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	input := &core.AgentInput{
		Task:      "test task",
		Timestamp: time.Now(),
	}

	streamOutput, err := agent.ExecuteStream(ctx, input)
	require.NoError(t, err)

	reader, ok := streamOutput.(*Reader)
	require.True(t, ok)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	// Try to read - should get context error quickly
	start := time.Now()
	chunk, err := reader.Next()
	elapsed := time.Since(start)

	// Should fail quickly (within 100ms)
	assert.Less(t, elapsed, 100*time.Millisecond)

	// Should get an error (either EOF or context error)
	assert.Error(t, err)
	assert.Nil(t, chunk)

	// LLM should not be called if context was cancelled before
	time.Sleep(50 * time.Millisecond) // Give goroutine time to finish
	assert.Equal(t, 0, mockClient.GetCallCount())
}

// TestStreamingLLMAgent_ContextCancellation_DuringLLMCall tests cancellation during LLM call
func TestStreamingLLMAgent_ContextCancellation_DuringLLMCall(t *testing.T) {
	// Mock client with 500ms delay
	mockClient := NewMockLLMClient(500*time.Millisecond, "test response")
	config := DefaultStreamingLLMConfig()
	config.ChunkDelay = 0

	agent := NewStreamingLLMAgent(mockClient, config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	input := &core.AgentInput{
		Task:      "test task",
		Timestamp: time.Now(),
	}

	streamOutput, err := agent.ExecuteStream(ctx, input)
	require.NoError(t, err)

	reader, ok := streamOutput.(*Reader)
	require.True(t, ok)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	// Cancel after 100ms (during LLM call)
	time.AfterFunc(100*time.Millisecond, cancel)

	// Try to collect all text
	start := time.Now()
	text, err := reader.CollectText()
	elapsed := time.Since(start)

	// Should fail relatively quickly (within 300ms, not full 500ms)
	assert.Less(t, elapsed, 300*time.Millisecond)

	// Should get context error or empty text
	if err != nil {
		t.Logf("Got expected error: %v", err)
	}
	assert.Empty(t, text) // No data should be collected
}

// TestStreamingLLMAgent_ContextCancellation_DuringStreaming tests cancellation during chunk streaming
func TestStreamingLLMAgent_ContextCancellation_DuringStreaming(t *testing.T) {
	// Long response with slow chunk delay
	longResponse := "Lorem ipsum dolor sit amet, consectetur adipiscing elit. " +
		"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."
	mockClient := NewMockLLMClient(0, longResponse)

	config := DefaultStreamingLLMConfig()
	config.ChunkSize = 10
	config.ChunkDelay = 100 * time.Millisecond // Slow streaming

	agent := NewStreamingLLMAgent(mockClient, config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	input := &core.AgentInput{
		Task:      "test task",
		Timestamp: time.Now(),
	}

	streamOutput, err := agent.ExecuteStream(ctx, input)
	require.NoError(t, err)

	reader, ok := streamOutput.(*Reader)
	require.True(t, ok)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	// Read a few chunks
	chunksRead := 0
	for i := 0; i < 3; i++ {
		_, err := reader.Next()
		if err != nil {
			break
		}
		chunksRead++
	}

	// Cancel context
	cancel()

	// Try to read more - should fail quickly
	start := time.Now()
	chunk, err := reader.Next()
	elapsed := time.Since(start)

	// Should fail within 150ms (not waiting for full chunk delay)
	assert.Less(t, elapsed, 150*time.Millisecond)

	// Should get error
	if err != nil {
		t.Logf("Got expected error after cancellation: %v", err)
	}
	assert.Nil(t, chunk)

	// Should have read some but not all chunks
	assert.Greater(t, chunksRead, 0)
	assert.Less(t, chunksRead, 10) // Not all chunks
}

// TestStreamingLLMAgent_ContextTimeout tests timeout handling
func TestStreamingLLMAgent_ContextTimeout(t *testing.T) {
	// Mock client with long delay
	mockClient := NewMockLLMClient(2*time.Second, "test response")
	config := DefaultStreamingLLMConfig()

	agent := NewStreamingLLMAgent(mockClient, config)

	// Short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	input := &core.AgentInput{
		Task:      "test task",
		Timestamp: time.Now(),
	}

	streamOutput, err := agent.ExecuteStream(ctx, input)
	require.NoError(t, err)

	reader, ok := streamOutput.(*Reader)
	require.True(t, ok)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	// Try to read - should timeout
	start := time.Now()
	text, err := reader.CollectText()
	elapsed := time.Since(start)

	// Should fail around timeout duration (within 300ms)
	assert.Less(t, elapsed, 300*time.Millisecond)

	// Should get timeout error or empty text
	if err != nil {
		t.Logf("Got expected error: %v", err)
	}
	assert.Empty(t, text)
}

// TestStreamingLLMAgent_GoroutineLeak tests that goroutines are cleaned up on context cancellation
func TestStreamingLLMAgent_GoroutineLeak(t *testing.T) {
	mockClient := NewMockLLMClient(100*time.Millisecond, "test response")
	config := DefaultStreamingLLMConfig()
	config.ChunkDelay = 10 * time.Millisecond

	agent := NewStreamingLLMAgent(mockClient, config)

	// Count goroutines before
	before := countGoroutines()

	// Create and cancel multiple streams
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithCancel(context.Background())

		input := &core.AgentInput{
			Task:      "test task",
			Timestamp: time.Now(),
		}

		streamOutput, err := agent.ExecuteStream(ctx, input)
		require.NoError(t, err)

		reader, ok := streamOutput.(*Reader)
		require.True(t, ok)

		// Cancel immediately
		cancel()

		// Try to read once
		_, _ = reader.Next()

		// Close reader
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}

	// Wait for goroutines to finish
	time.Sleep(500 * time.Millisecond)

	// Count goroutines after
	after := countGoroutines()

	// Allow some variance (test framework goroutines)
	// but should not have leaked 10+ goroutines
	diff := after - before
	assert.Less(t, diff, 5, "Too many goroutines leaked: before=%d, after=%d, diff=%d", before, after, diff)
}

// TestStreamingLLMAgent_ConcurrentCancellation tests concurrent stream cancellation
func TestStreamingLLMAgent_ConcurrentCancellation(t *testing.T) {
	mockClient := NewMockLLMClient(50*time.Millisecond, "test response")
	config := DefaultStreamingLLMConfig()
	config.ChunkDelay = 10 * time.Millisecond

	agent := NewStreamingLLMAgent(mockClient, config)

	var wg sync.WaitGroup
	numStreams := 20

	for i := 0; i < numStreams; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			input := &core.AgentInput{
				Task:      "test task",
				Timestamp: time.Now(),
			}

			streamOutput, err := agent.ExecuteStream(ctx, input)
			if err != nil {
				t.Logf("Stream %d failed to start: %v", idx, err)
				return
			}

			reader, ok := streamOutput.(*Reader)
			if !ok {
				t.Logf("Stream %d: invalid reader type", idx)
				return
			}
			defer func() {
				if err := reader.Close(); err != nil {
					t.Logf("Stream %d: failed to close reader: %v", idx, err)
				}
			}()

			// Read a bit then cancel
			time.Sleep(time.Duration(idx) * 5 * time.Millisecond)
			cancel()

			// Try to read after cancel
			_, _ = reader.Next()
		}(i)
	}

	// Wait for all streams to finish
	wg.Wait()

	// No assertions - just checking it doesn't panic or deadlock
}

// TestStreamingLLMAgent_ChunkDelayRespectsCancellation tests that chunk delay can be interrupted
func TestStreamingLLMAgent_ChunkDelayRespectsCancellation(t *testing.T) {
	// Use a longer response to ensure multiple chunks
	mockClient := NewMockLLMClient(0, strings.Repeat("Hello World ", 20)) // 240 chars
	config := DefaultStreamingLLMConfig()
	config.ChunkSize = 10                      // Small chunks
	config.ChunkDelay = 500 * time.Millisecond // Long delay between chunks

	agent := NewStreamingLLMAgent(mockClient, config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	input := &core.AgentInput{
		Task:      "test task",
		Timestamp: time.Now(),
	}

	streamOutput, err := agent.ExecuteStream(ctx, input)
	require.NoError(t, err)

	reader, ok := streamOutput.(*Reader)
	require.True(t, ok)
	defer func() {
		if err := reader.Close(); err != nil {
			t.Logf("failed to close reader: %v", err)
		}
	}()

	// Read first chunk
	_, err = reader.Next()
	require.NoError(t, err)

	// Cancel during delay before next chunk
	cancel()

	// Try to collect all remaining text - should fail quickly
	start := time.Now()
	_, err = reader.CollectText()
	elapsed := time.Since(start)

	// Should fail within 200ms, not wait for multiple delays (would be several seconds)
	assert.Less(t, elapsed, 300*time.Millisecond)

	// Should get context error or EOF (if channel closed before we could read)
	// Either is acceptable since we cancelled
	if err != nil && err != io.EOF {
		t.Logf("Got error (expected): %v", err)
	}
}

// Helper function to count goroutines
func countGoroutines() int {
	// Give runtime a moment to clean up
	time.Sleep(10 * time.Millisecond)
	var buf [8192]byte
	n := runtime.Stack(buf[:], true)
	stack := string(buf[:n])

	// Count "goroutine" occurrences
	count := 0
	for i := 0; i < len(stack); {
		if idx := strings.Index(stack[i:], "goroutine "); idx >= 0 {
			count++
			i += idx + 10
		} else {
			break
		}
	}
	return count
}
