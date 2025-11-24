package providers

import (
	"testing"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/sashabaranov/go-openai"
)

// BenchmarkMessageConversion benchmarks the message conversion process
// to verify the sync.Pool optimization effectiveness
func BenchmarkMessageConversion(b *testing.B) {
	testMessages := []agentllm.Message{
		{Role: "system", Content: "You are a helpful assistant"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
		{Role: "user", Content: "How are you?"},
	}

	b.Run("WithPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Get from pool
			messagesPtr := messageSlicePool.Get().(*[]openai.ChatCompletionMessage)
			messages := *messagesPtr

			// Ensure capacity and reset length
			if cap(messages) < len(testMessages) {
				messages = make([]openai.ChatCompletionMessage, len(testMessages))
			} else {
				messages = messages[:len(testMessages)]
			}

			// Convert messages
			for j, msg := range testMessages {
				messages[j] = openai.ChatCompletionMessage{
					Role:    msg.Role,
					Content: msg.Content,
					Name:    msg.Name,
				}
			}

			// Clear and return to pool
			for j := range messages {
				messages[j] = openai.ChatCompletionMessage{}
			}
			messages = messages[:0]
			*messagesPtr = messages
			messageSlicePool.Put(messagesPtr)
		}
	})

	b.Run("WithoutPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Allocate new slice each time (old approach)
			messages := make([]openai.ChatCompletionMessage, len(testMessages))
			for j, msg := range testMessages {
				messages[j] = openai.ChatCompletionMessage{
					Role:    msg.Role,
					Content: msg.Content,
					Name:    msg.Name,
				}
			}
			// Messages are discarded (will be GC'd)
			_ = messages
		}
	})
}

// BenchmarkMessageConversionLarge benchmarks with typical conversation length
func BenchmarkMessageConversionLarge(b *testing.B) {
	// Simulate a longer conversation (10 messages)
	testMessages := make([]agentllm.Message, 10)
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			testMessages[i] = agentllm.Message{
				Role:    "user",
				Content: "This is message " + string(rune(i)),
			}
		} else {
			testMessages[i] = agentllm.Message{
				Role:    "assistant",
				Content: "This is response " + string(rune(i)),
			}
		}
	}

	b.Run("WithPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			messagesPtr := messageSlicePool.Get().(*[]openai.ChatCompletionMessage)
			messages := *messagesPtr

			if cap(messages) < len(testMessages) {
				messages = make([]openai.ChatCompletionMessage, len(testMessages))
			} else {
				messages = messages[:len(testMessages)]
			}

			for j, msg := range testMessages {
				messages[j] = openai.ChatCompletionMessage{
					Role:    msg.Role,
					Content: msg.Content,
					Name:    msg.Name,
				}
			}

			for j := range messages {
				messages[j] = openai.ChatCompletionMessage{}
			}
			messages = messages[:0]
			*messagesPtr = messages
			messageSlicePool.Put(messagesPtr)
		}
	})

	b.Run("WithoutPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			messages := make([]openai.ChatCompletionMessage, len(testMessages))
			for j, msg := range testMessages {
				messages[j] = openai.ChatCompletionMessage{
					Role:    msg.Role,
					Content: msg.Content,
					Name:    msg.Name,
				}
			}
			_ = messages
		}
	})
}
