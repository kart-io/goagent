package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

func main() {
	fmt.Println("=== Anthropic Claude Provider Examples ===")

	// Example 1: Basic Configuration
	fmt.Println("Example 1: Basic Configuration")
	basicExample()

	// Example 2: Using Different Models
	fmt.Println("\nExample 2: Using Different Models")
	modelExample()

	// Example 3: Streaming Responses
	fmt.Println("\nExample 3: Streaming Responses")
	streamingExample()

	// Example 4: Multi-turn Conversation
	fmt.Println("\nExample 4: Multi-turn Conversation")
	conversationExample()

	// Example 5: Token Usage Tracking
	fmt.Println("\nExample 5: Token Usage Tracking")
	tokenUsageExample()

	// Example 6: Error Handling
	fmt.Println("\nExample 6: Error Handling")
	errorHandlingExample()
}

// Example 1: Basic Configuration
func basicExample() {
	// Initialize provider with API key from environment
	provider, err := providers.NewAnthropic(&llm.Config{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"), // or set directly: APIKey: "your-api-key"
		Model:  "claude-3-sonnet-20240229",     // Optional: defaults to claude-3-sonnet-20240229
	})
	if err != nil {
		log.Fatalf("Failed to create Anthropic provider: %v", err)
	}

	// Create a simple completion request
	ctx := context.Background()
	resp, err := provider.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "What is the capital of France?"},
		},
	})
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Response: %s\n", resp.Content)
	fmt.Printf("Model: %s\n", resp.Model)
	fmt.Printf("Tokens Used: %d\n", resp.TokensUsed)
}

// Example 2: Using Different Models
func modelExample() {
	models := []string{
		"claude-3-opus-20240229",   // Most capable
		"claude-3-sonnet-20240229", // Balanced
		"claude-3-haiku-20240307",  // Fastest
	}

	for _, model := range models {
		provider, err := providers.NewAnthropic(&llm.Config{
			APIKey: os.Getenv("ANTHROPIC_API_KEY"),
			Model:  model,
		})
		if err != nil {
			log.Printf("Failed to create provider for %s: %v\n", model, err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := provider.Complete(ctx, &llm.CompletionRequest{
			Messages: []llm.Message{
				{Role: "user", Content: "Say hello in one word."},
			},
			MaxTokens: 50,
		})
		if err != nil {
			log.Printf("Error with %s: %v\n", model, err)
			continue
		}

		fmt.Printf("Model: %s\n", model)
		fmt.Printf("Response: %s\n", resp.Content)
		fmt.Printf("Tokens: %d\n\n", resp.TokensUsed)
	}
}

// Example 3: Streaming Responses
func streamingExample() {
	provider, err := providers.NewAnthropic(&llm.Config{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	tokenChan, err := provider.Stream(ctx, "Write a haiku about programming.")
	if err != nil {
		log.Printf("Streaming error: %v\n", err)
		return
	}

	fmt.Print("Streaming response: ")
	for token := range tokenChan {
		fmt.Print(token)
	}
	fmt.Println()
}

// Example 4: Multi-turn Conversation
func conversationExample() {
	provider, err := providers.NewAnthropic(&llm.Config{
		APIKey:      os.Getenv("ANTHROPIC_API_KEY"),
		Temperature: 0.7,
		MaxTokens:   1000,
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	// Conversation history
	messages := []llm.Message{
		{Role: "user", Content: "My name is Alice. What's the weather like?"},
	}

	ctx := context.Background()

	// First turn
	resp1, err := provider.Complete(ctx, &llm.CompletionRequest{
		Messages: messages,
	})
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Assistant: %s\n\n", resp1.Content)

	// Add response to history
	messages = append(messages, llm.Message{
		Role:    "assistant",
		Content: resp1.Content,
	})

	// Second turn - reference earlier context
	messages = append(messages, llm.Message{
		Role:    "user",
		Content: "Do you remember my name?",
	})

	resp2, err := provider.Complete(ctx, &llm.CompletionRequest{
		Messages: messages,
	})
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Assistant: %s\n", resp2.Content)
}

// Example 5: Token Usage Tracking
func tokenUsageExample() {
	provider, err := providers.NewAnthropic(&llm.Config{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	resp, err := provider.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "Explain quantum computing in 50 words."},
		},
		MaxTokens: 100,
	})
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Response: %s\n\n", resp.Content)
	fmt.Println("Token Usage:")
	if resp.Usage != nil {
		fmt.Printf("  Prompt Tokens: %d\n", resp.Usage.PromptTokens)
		fmt.Printf("  Completion Tokens: %d\n", resp.Usage.CompletionTokens)
		fmt.Printf("  Total Tokens: %d\n", resp.Usage.TotalTokens)
	}
	fmt.Printf("  Finish Reason: %s\n", resp.FinishReason)
}

// Example 6: Error Handling
func errorHandlingExample() {
	// Example with invalid API key
	provider, err := providers.NewAnthropic(&llm.Config{
		APIKey: "invalid-key",
	})
	if err != nil {
		log.Printf("Initialization error (expected): %v\n", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = provider.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "Test"},
		},
	})
	if err != nil {
		fmt.Printf("API error (expected): %v\n", err)
		// In production, you would handle specific error types:
		// - Invalid API key (401)
		// - Rate limit (429)
		// - Server error (500+)
		// - Context timeout
	}
}
