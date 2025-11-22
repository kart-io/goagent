package llm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kart-io/goagent/llm/constants"
)

func TestMockStreamClient(t *testing.T) {
	client := NewMockStreamClient()

	t.Run("Provider and IsAvailable", func(t *testing.T) {
		if client.Provider() != constants.ProviderCustom {
			t.Errorf("Expected provider %s, got %s", constants.ProviderCustom, client.Provider())
		}

		if !client.IsAvailable() {
			t.Error("Expected client to be available")
		}
	})

	t.Run("Complete", func(t *testing.T) {
		ctx := context.Background()
		req := &CompletionRequest{
			Messages: []Message{
				UserMessage("Hello"),
			},
		}

		resp, err := client.Complete(ctx, req)
		if err != nil {
			t.Fatalf("Complete failed: %v", err)
		}

		if resp.Content == "" {
			t.Error("Expected non-empty content")
		}
	})

	t.Run("Chat", func(t *testing.T) {
		ctx := context.Background()
		messages := []Message{
			SystemMessage("You are a helpful assistant"),
			UserMessage("Hello"),
		}

		resp, err := client.Chat(ctx, messages)
		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}

		if resp.Content == "" {
			t.Error("Expected non-empty content")
		}
	})
}

func TestMockStreamClient_CompleteStream(t *testing.T) {
	client := NewMockStreamClient()

	t.Run("Stream completion", func(t *testing.T) {
		ctx := context.Background()
		req := &CompletionRequest{
			Messages: []Message{
				UserMessage("Tell me a story"),
			},
		}

		stream, err := client.CompleteStream(ctx, req)
		if err != nil {
			t.Fatalf("CompleteStream failed: %v", err)
		}

		chunkCount := 0
		var finalContent string

		for chunk := range stream {
			if chunk.Error != nil {
				t.Fatalf("Stream chunk error: %v", chunk.Error)
			}

			chunkCount++
			finalContent = chunk.Content

			if chunk.Done {
				if chunk.Usage == nil {
					t.Error("Expected usage information in final chunk")
				}
				break
			}

			// 增量应该非空（除了最后一块）
			if !chunk.Done && chunk.Delta == "" {
				t.Error("Expected non-empty delta in non-final chunk")
			}
		}

		if chunkCount == 0 {
			t.Error("Expected at least one chunk")
		}

		if finalContent == "" {
			t.Error("Expected non-empty final content")
		}
	})

	t.Run("Stream with context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		req := &CompletionRequest{
			Messages: []Message{
				UserMessage("Test"),
			},
		}

		stream, err := client.CompleteStream(ctx, req)
		if err != nil {
			t.Fatalf("CompleteStream failed: %v", err)
		}

		for chunk := range stream {
			if chunk.Error != nil {
				// 应该得到 context 错误
				if !errors.Is(chunk.Error, context.DeadlineExceeded) {
					t.Logf("Got error: %v", chunk.Error)
				}
				break
			}

			if chunk.Done {
				break
			}
		}
	})

	t.Run("No goroutine leak on early exit", func(t *testing.T) {
		ctx := context.Background()

		req := &CompletionRequest{
			Messages: []Message{
				UserMessage("Test"),
			},
		}

		stream, err := client.CompleteStream(ctx, req)
		if err != nil {
			t.Fatalf("CompleteStream failed: %v", err)
		}

		// Read only the first chunk and stop
		// This simulates a consumer that stops reading early
		chunkCount := 0
		for chunk := range stream {
			chunkCount++
			if chunkCount >= 1 {
				// Stop consuming - goroutine should not leak
				break
			}

			if chunk.Error != nil {
				t.Fatalf("Unexpected error: %v", chunk.Error)
			}
		}

		// Give time for goroutine to potentially leak
		time.Sleep(200 * time.Millisecond)

		// If we reach here without hanging, the goroutine didn't leak
		if chunkCount < 1 {
			t.Error("Expected at least one chunk")
		}
	})

	t.Run("No goroutine leak on context cancellation without consuming", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		req := &CompletionRequest{
			Messages: []Message{
				UserMessage("Test"),
			},
		}

		stream, err := client.CompleteStream(ctx, req)
		if err != nil {
			t.Fatalf("CompleteStream failed: %v", err)
		}

		// Don't consume the stream at all - simulate a consumer that never reads
		// Wait for context to expire
		time.Sleep(100 * time.Millisecond)

		// Now try to consume - should get error or closed channel
		select {
		case chunk, ok := <-stream:
			if ok && chunk.Error != nil {
				// Expected: got error chunk
				if !errors.Is(chunk.Error, context.DeadlineExceeded) {
					t.Logf("Got error: %v", chunk.Error)
				}
			}
		case <-time.After(500 * time.Millisecond):
			t.Error("Timeout waiting for stream - potential goroutine leak")
		}
	})
}

func TestStreamReader(t *testing.T) {
	t.Run("ReadAll", func(t *testing.T) {
		ctx := context.Background()

		// 创建模拟流
		stream := make(chan *StreamChunk, 10)
		go func() {
			defer close(stream)

			text := "Hello, world!"
			accumulated := ""

			for i, char := range text {
				accumulated += string(char)
				stream <- &StreamChunk{
					Content:   accumulated,
					Delta:     string(char),
					Index:     i,
					Timestamp: time.Now(),
					Done:      i == len(text)-1,
				}
			}
		}()

		reader := NewStreamReader(stream)
		content, err := reader.ReadAll(ctx)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}

		if content != "Hello, world!" {
			t.Errorf("Expected 'Hello, world!', got '%s'", content)
		}
	})

	t.Run("ReadChunks", func(t *testing.T) {
		ctx := context.Background()

		stream := make(chan *StreamChunk, 5)
		go func() {
			defer close(stream)

			for i := 1; i <= 3; i++ {
				stream <- &StreamChunk{
					Content:   "chunk",
					Delta:     "chunk",
					Index:     i,
					Done:      i == 3,
					Timestamp: time.Now(),
				}
			}
		}()

		reader := NewStreamReader(stream)
		chunkCount := 0

		err := reader.ReadChunks(ctx, func(chunk *StreamChunk) error {
			chunkCount++
			return nil
		})
		if err != nil {
			t.Fatalf("ReadChunks failed: %v", err)
		}

		if chunkCount != 3 {
			t.Errorf("Expected 3 chunks, got %d", chunkCount)
		}
	})

	t.Run("CollectDeltas", func(t *testing.T) {
		ctx := context.Background()

		stream := make(chan *StreamChunk, 5)
		go func() {
			defer close(stream)

			words := []string{"Hello", " ", "world"}
			for i, word := range words {
				stream <- &StreamChunk{
					Delta:     word,
					Index:     i,
					Done:      i == len(words)-1,
					Timestamp: time.Now(),
				}
			}
		}()

		reader := NewStreamReader(stream)
		deltas, err := reader.CollectDeltas(ctx)
		if err != nil {
			t.Fatalf("CollectDeltas failed: %v", err)
		}

		if len(deltas) != 3 {
			t.Errorf("Expected 3 deltas, got %d", len(deltas))
		}

		expected := []string{"Hello", " ", "world"}
		for i, delta := range deltas {
			if delta != expected[i] {
				t.Errorf("Delta %d: expected '%s', got '%s'", i, expected[i], delta)
			}
		}
	})
}

func TestStreamWriter(t *testing.T) {
	t.Run("Write and WriteChunk", func(t *testing.T) {
		stream := make(chan *StreamChunk, 10)
		writer := NewStreamWriter(stream)

		go func() {
			_ = writer.Write("Hello")
			_ = writer.Write(" ")
			_ = writer.Write("world")
			_ = writer.Close()
		}()

		chunks := make([]*StreamChunk, 0)
		for chunk := range stream {
			chunks = append(chunks, chunk)
		}

		if len(chunks) != 3 {
			t.Errorf("Expected 3 chunks, got %d", len(chunks))
		}

		for i, chunk := range chunks {
			if chunk.Delta == "" {
				t.Errorf("Chunk %d has empty delta", i)
			}
		}
	})
}

func BenchmarkStreamClient_CompleteStream(b *testing.B) {
	client := NewMockStreamClient()
	ctx := context.Background()

	req := &CompletionRequest{
		Messages: []Message{
			UserMessage("Benchmark test"),
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stream, _ := client.CompleteStream(ctx, req)

		// 消费流
		for range stream {
		}
	}
}
