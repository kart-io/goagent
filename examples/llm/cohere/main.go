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
	fmt.Println("=== Cohere Provider Examples ===")

	// Example 1: Basic Configuration
	fmt.Println("Example 1: Basic Configuration")
	basicExample()

	// Example 2: Using Different Models
	fmt.Println("\nExample 2: Using Different Models")
	modelExample()

	// Example 3: Streaming Responses
	fmt.Println("\nExample 3: Streaming Responses")
	streamingExample()

	// Example 4: Chat with History
	fmt.Println("\nExample 4: Chat with History")
	chatHistoryExample()

	// Example 5: Custom Parameters
	fmt.Println("\nExample 5: Custom Parameters")
	customParametersExample()

	// Example 6: Token Usage and Billing
	fmt.Println("\nExample 6: Token Usage and Billing")
	tokenUsageExample()
}

// Example 1: Basic Configuration
func basicExample() {
	// Initialize provider with API key from environment
	provider, err := providers.NewCohere(&llm.LLMOptions{
		APIKey: os.Getenv("COHERE_API_KEY"), // or set directly: APIKey: "your-api-key"
		Model:  "command",                   // Optional: defaults to "command"
	})
	if err != nil {
		log.Fatalf("Failed to create Cohere provider: %v", err)
	}

	// Create a simple completion request
	ctx := context.Background()
	resp, err := provider.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "What is machine learning?"},
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
		"command",        // Standard model
		"command-light",  // Faster, lighter model
		"command-r",      // RAG-optimized model
		"command-r-plus", // Enhanced RAG model
	}

	for _, model := range models {
		provider, err := providers.NewCohere(&llm.LLMOptions{
			APIKey: os.Getenv("COHERE_API_KEY"),
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
				{Role: "user", Content: "Say hello in French."},
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
	provider, err := providers.NewCohere(&llm.LLMOptions{
		APIKey: os.Getenv("COHERE_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	tokenChan, err := provider.Stream(ctx, "Write a short poem about the ocean.")
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

// Example 4: Chat with History
func chatHistoryExample() {
	provider, err := providers.NewCohere(&llm.LLMOptions{
		APIKey:      os.Getenv("COHERE_API_KEY"),
		Temperature: 0.7,
		MaxTokens:   1000,
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	// Build conversation with chat history
	// Note: Cohere uses last user message as main message, previous as history
	messages := []llm.Message{
		{Role: "user", Content: "I'm planning a trip to Japan."},
		{Role: "assistant", Content: "That sounds exciting! Japan is a wonderful destination. What would you like to know about planning your trip?"},
		{Role: "user", Content: "What's the best time to visit?"},
	}

	ctx := context.Background()
	resp, err := provider.Complete(ctx, &llm.CompletionRequest{
		Messages: messages,
	})
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Response with history: %s\n", resp.Content)
}

// Example 5: Custom Parameters
func customParametersExample() {
	provider, err := providers.NewCohere(&llm.LLMOptions{
		APIKey:      os.Getenv("COHERE_API_KEY"),
		Temperature: 0.9, // Higher creativity
		MaxTokens:   200,
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	resp, err := provider.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "Invent a creative name for a coffee shop."},
		},
		Temperature: 0.9,              // Override provider default
		TopP:        0.95,             // Nucleus sampling
		Stop:        []string{"\n\n"}, // Stop sequences
	})
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Creative response: %s\n", resp.Content)
	fmt.Printf("Finish reason: %s\n", resp.FinishReason)
}

// Example 6: Token Usage and Billing
func tokenUsageExample() {
	provider, err := providers.NewCohere(&llm.LLMOptions{
		APIKey: os.Getenv("COHERE_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	resp, err := provider.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "Summarize the benefits of renewable energy in three bullet points."},
		},
		MaxTokens: 150,
	})
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Response: %s\n\n", resp.Content)
	fmt.Println("Token Usage:")
	if resp.Usage != nil {
		fmt.Printf("  Prompt Tokens: %d\n", resp.Usage.PromptTokens)
		fmt.Printf("  Response Tokens: %d\n", resp.Usage.CompletionTokens)
		fmt.Printf("  Total Tokens: %d\n", resp.Usage.TotalTokens)
		// Note: Cohere also provides BilledTokens in native response
	}
	fmt.Printf("  Provider: %s\n", resp.Provider)
	fmt.Printf("  Finish Reason: %s\n", resp.FinishReason)
}
