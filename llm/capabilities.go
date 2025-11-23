package llm

import (
	"context"

	"github.com/kart-io/goagent/interfaces"
)

// ToolCallingClient defines the interface for LLM providers that support tool/function calling.
// Implement this interface in addition to Client to enable tool-based interactions.
//
// Not all providers support tool calling - use CapabilityChecker.HasCapability("tool_calling")
// to check at runtime.
//
// Implementations:
//   - providers.OpenAIProvider
//   - providers.DeepSeekProvider
//   - providers.GeminiProvider
type ToolCallingClient interface {
	// GenerateWithTools sends a prompt with available tools and returns the response.
	// The LLM may respond with content, tool calls, or both.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - prompt: The user's input prompt
	//   - tools: Available tools that the LLM can call
	//
	// Returns:
	//   - ToolCallResponse containing content and/or tool calls
	//   - error if the request fails
	GenerateWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (*ToolCallResponse, error)

	// StreamWithTools is like GenerateWithTools but streams the response.
	// Tool calls are streamed as ToolChunk with type "tool_call", "tool_name", "tool_args".
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - prompt: The user's input prompt
	//   - tools: Available tools that the LLM can call
	//
	// Returns:
	//   - channel of ToolChunk for streaming response
	//   - error if stream setup fails
	StreamWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (<-chan ToolChunk, error)
}

// EmbeddingClient defines the interface for LLM providers that support text embeddings.
// Implement this interface in addition to Client to enable embedding generation.
//
// Not all providers support embeddings - use CapabilityChecker.HasCapability("embedding")
// to check at runtime.
//
// Implementations:
//   - providers.OpenAIProvider
//   - providers.DeepSeekProvider
type EmbeddingClient interface {
	// Embed generates a vector embedding for the given text.
	// The embedding can be used for semantic search, similarity, clustering, etc.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - text: The text to embed
	//
	// Returns:
	//   - []float64: The embedding vector
	//   - error if embedding generation fails
	Embed(ctx context.Context, text string) ([]float64, error)
}

// BatchEmbeddingClient extends EmbeddingClient with batch embedding support.
// This is more efficient than calling Embed multiple times.
type BatchEmbeddingClient interface {
	EmbeddingClient

	// EmbedBatch generates embeddings for multiple texts in a single request.
	// This is more efficient than calling Embed in a loop.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - texts: The texts to embed
	//
	// Returns:
	//   - [][]float64: The embedding vectors, one per input text
	//   - error if batch embedding fails
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
}

// TokenStreamingClient defines the interface for simple token-based streaming.
// This is a simpler alternative to StreamClient's chunk-based streaming.
//
// Implementations:
//   - providers.DeepSeekProvider
//   - providers.CohereProvider
//   - providers.HuggingFaceProvider
//   - providers.AnthropicProvider
type TokenStreamingClient interface {
	// Stream generates tokens for the given prompt and streams them as strings.
	// This is simpler than CompleteStream but provides less metadata.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - prompt: The input prompt
	//
	// Returns:
	//   - channel of string tokens
	//   - error if stream setup fails
	Stream(ctx context.Context, prompt string) (<-chan string, error)
}

// Capability represents a provider capability that can be checked at runtime.
type Capability string

// Standard provider capabilities
const (
	// CapabilityCompletion indicates basic completion support (all providers)
	CapabilityCompletion Capability = "completion"

	// CapabilityChat indicates chat/conversation support (all providers)
	CapabilityChat Capability = "chat"

	// CapabilityStreaming indicates streaming response support
	CapabilityStreaming Capability = "streaming"

	// CapabilityToolCalling indicates function/tool calling support
	CapabilityToolCalling Capability = "tool_calling"

	// CapabilityEmbedding indicates text embedding support
	CapabilityEmbedding Capability = "embedding"

	// CapabilityBatchEmbedding indicates batch embedding support
	CapabilityBatchEmbedding Capability = "batch_embedding"

	// CapabilityVision indicates vision/image understanding support
	CapabilityVision Capability = "vision"

	// CapabilityJSON indicates JSON mode/structured output support
	CapabilityJSON Capability = "json_mode"
)

// CapabilityChecker allows runtime capability discovery.
// Providers can implement this to advertise their capabilities.
//
// Example usage:
//
//	if checker, ok := client.(CapabilityChecker); ok {
//	    if checker.HasCapability(CapabilityToolCalling) {
//	        // Use tool calling
//	    }
//	}
type CapabilityChecker interface {
	// HasCapability checks if the provider supports the given capability.
	HasCapability(cap Capability) bool

	// Capabilities returns all supported capabilities.
	Capabilities() []Capability
}

// FullClient combines all optional client interfaces.
// Use this for providers that support all capabilities.
type FullClient interface {
	Client
	StreamClient
	ToolCallingClient
	EmbeddingClient
	CapabilityChecker
}

// Helper functions for capability checking

// HasToolCalling checks if a client supports tool calling.
func HasToolCalling(client Client) bool {
	_, ok := client.(ToolCallingClient)
	return ok
}

// HasEmbedding checks if a client supports embeddings.
func HasEmbedding(client Client) bool {
	_, ok := client.(EmbeddingClient)
	return ok
}

// HasStreaming checks if a client supports streaming.
func HasStreaming(client Client) bool {
	_, ok := client.(StreamClient)
	return ok
}

// HasTokenStreaming checks if a client supports simple token streaming.
func HasTokenStreaming(client Client) bool {
	_, ok := client.(TokenStreamingClient)
	return ok
}

// AsToolCaller safely casts a client to ToolCallingClient.
// Returns nil if the client doesn't support tool calling.
func AsToolCaller(client Client) ToolCallingClient {
	if tc, ok := client.(ToolCallingClient); ok {
		return tc
	}
	return nil
}

// AsEmbedder safely casts a client to EmbeddingClient.
// Returns nil if the client doesn't support embeddings.
func AsEmbedder(client Client) EmbeddingClient {
	if ec, ok := client.(EmbeddingClient); ok {
		return ec
	}
	return nil
}

// AsStreamClient safely casts a client to StreamClient.
// Returns nil if the client doesn't support streaming.
func AsStreamClient(client Client) StreamClient {
	if sc, ok := client.(StreamClient); ok {
		return sc
	}
	return nil
}
