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
	fmt.Println("=== Hugging Face Provider Examples ===")

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

	// Example 5: Model Loading and Retry
	fmt.Println("\nExample 5: Model Loading and Retry")
	modelLoadingExample()

	// Example 6: Custom Inference API Parameters
	fmt.Println("\nExample 6: Custom Inference API Parameters")
	customParametersExample()
}

// Example 1: Basic Configuration
func basicExample() {
	// Initialize provider with API key from environment
	provider, err := providers.NewHuggingFace(&llm.Config{
		APIKey: os.Getenv("HUGGINGFACE_API_KEY"),      // or set directly: APIKey: "your-api-key"
		Model:  "meta-llama/Meta-Llama-3-8B-Instruct", // Optional: defaults to Meta-Llama-3-8B-Instruct
	})
	if err != nil {
		log.Fatalf("Failed to create HuggingFace provider: %v", err)
	}

	// Create a simple completion request
	ctx := context.Background()
	resp, err := provider.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "What is deep learning?"},
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
	// Popular open-source models available on HuggingFace
	models := []string{
		"meta-llama/Meta-Llama-3-8B-Instruct",
		"mistralai/Mixtral-8x7B-Instruct-v0.1",
		"google/flan-t5-xxl",
		"bigscience/bloom",
	}

	for _, model := range models {
		provider, err := providers.NewHuggingFace(&llm.Config{
			APIKey: os.Getenv("HUGGINGFACE_API_KEY"),
			Model:  model,
		})
		if err != nil {
			log.Printf("Failed to create provider for %s: %v\n", model, err)
			continue
		}

		// Note: Model loading may take 20-60 seconds on first request
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		resp, err := provider.Complete(ctx, &llm.CompletionRequest{
			Messages: []llm.Message{
				{Role: "user", Content: "Say hello in one sentence."},
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
	provider, err := providers.NewHuggingFace(&llm.Config{
		APIKey: os.Getenv("HUGGINGFACE_API_KEY"),
		Model:  "meta-llama/Meta-Llama-3-8B-Instruct",
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	tokenChan, err := provider.Stream(ctx, "Write a haiku about artificial intelligence.")
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
	provider, err := providers.NewHuggingFace(&llm.Config{
		APIKey:      os.Getenv("HUGGINGFACE_API_KEY"),
		Model:       "meta-llama/Meta-Llama-3-8B-Instruct",
		Temperature: 0.7,
		MaxTokens:   500,
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	// Build multi-turn conversation
	messages := []llm.Message{
		{Role: "system", Content: "You are a helpful AI assistant."},
		{Role: "user", Content: "What is the Fibonacci sequence?"},
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

	// Second turn
	messages = append(messages, llm.Message{
		Role:    "user",
		Content: "Can you give me the first 10 numbers?",
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

// Example 5: Model Loading and Retry
func modelLoadingExample() {
	// HuggingFace Inference API may need to load models from cold start
	// The provider automatically retries with exponential backoff (up to 5 attempts)
	provider, err := providers.NewHuggingFace(&llm.Config{
		APIKey:  os.Getenv("HUGGINGFACE_API_KEY"),
		Model:   "mistralai/Mixtral-8x7B-Instruct-v0.1",
		Timeout: 180, // 3 minutes timeout for model loading
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	fmt.Println("Requesting completion (model may need to load, please wait)...")

	ctx := context.Background()
	start := time.Now()

	resp, err := provider.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "What is the meaning of life?"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		log.Printf("Error (may indicate model loading timeout): %v\n", err)
		return
	}

	duration := time.Since(start)
	fmt.Printf("Response: %s\n", resp.Content)
	fmt.Printf("Time taken: %.2f seconds\n", duration.Seconds())
	fmt.Printf("Note: First request may take 20-60s for model loading\n")
}

// Example 6: Custom Inference API Parameters
func customParametersExample() {
	provider, err := providers.NewHuggingFace(&llm.Config{
		APIKey:      os.Getenv("HUGGINGFACE_API_KEY"),
		Model:       "meta-llama/Meta-Llama-3-8B-Instruct",
		Temperature: 0.8,
		MaxTokens:   200,
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	resp, err := provider.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "Generate a creative story opening."},
		},
		Temperature: 0.9,  // Higher creativity
		TopP:        0.95, // Nucleus sampling
		MaxTokens:   150,
		Stop:        []string{"\n\n", "The End"}, // Stop sequences
	})
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Creative response: %s\n", resp.Content)
	fmt.Printf("Finish reason: %s\n", resp.FinishReason)
	if resp.Usage != nil {
		fmt.Printf("Tokens: %d (prompt) + %d (completion) = %d (total)\n",
			resp.Usage.PromptTokens,
			resp.Usage.CompletionTokens,
			resp.Usage.TotalTokens)
	}
}
