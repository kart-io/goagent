package llm

import (
	"context"
)

// EmbeddingClient defines the interface for LLM providers that support text embeddings.
// Implement this interface in addition to Client to enable embedding generation.
//
// Not all providers support embeddings - use CapabilityChecker.HasCapability("embedding")
// to check at runtime.
//
// Implementations:
//   - providers.OpenAIProvider
//   - providers.GeminiProvider
//
// Legacy Embed methods on other providers do not imply advertised support.
type EmbeddingClient interface {
	// Embed generates a vector embedding for the given text.
	// The embedding can be used for semantic search, similarity, clustering, etc.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - text: The text to embed
	//
	// Returns:
	//   - []float32: The embedding vector
	//   - error if embedding generation fails
	Embed(ctx context.Context, text string) ([]float32, error)
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

	// CapabilityStructuredStreaming indicates StreamClient, including terminal
	// status and usage. Legacy string/token streams do not imply this capability.
	CapabilityStructuredStreaming Capability = "structured_streaming"

	// CapabilityToolStreaming indicates ToolStreamClient's native message loop.
	CapabilityToolStreaming Capability = "tool_streaming"

	// CapabilityToolCalling indicates function/tool calling support
	CapabilityToolCalling Capability = "tool_calling"

	// CapabilityEmbedding indicates text embedding support
	CapabilityEmbedding Capability = "embedding"

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
	ToolStreamClient
	EmbeddingClient
	CapabilityChecker
}

// Helper functions for capability checking

// HasEmbedding requires an implementation and respects an explicit capability
// denial. A legacy method can remain callable while returning unsupported.
func HasEmbedding(client Client) bool {
	if checker, ok := client.(CapabilityChecker); ok && !checker.HasCapability(CapabilityEmbedding) {
		return false
	}
	_, ok := client.(EmbeddingClient)
	return ok
}

// HasToolStreaming checks the executable interface, not a provider's advertised
// feature list. Server/model support must still be validated by the application.
func HasToolStreaming(client Client) bool {
	_, ok := client.(ToolStreamClient)
	return ok
}

// AsToolStreamClient returns the structured native tool-stream interface.
func AsToolStreamClient(client Client) ToolStreamClient {
	if c, ok := client.(ToolStreamClient); ok {
		return c
	}
	return nil
}

// AsEmbedder safely casts a client to EmbeddingClient.
// Returns nil if the client doesn't support embeddings.
func AsEmbedder(client Client) EmbeddingClient {
	if !HasEmbedding(client) {
		return nil
	}
	if ec, ok := client.(EmbeddingClient); ok {
		return ec
	}
	return nil
}
