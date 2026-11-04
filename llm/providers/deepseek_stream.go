package providers

import (
	"context"
	"strings"

	"github.com/sashabaranov/go-openai"

	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
)

var _ agentllm.ToolStreamClient = (*DeepSeekProvider)(nil)

// nativeStreamProvider reuses the same SSE decoder and completion checks as
// OpenAI-compatible endpoints, retaining DeepSeek configuration and transport.
// It performs no implicit retries; callers own the request budget.
// The resty client's overall Timeout would cut long SSE bodies, so the timeout
// only bounds the wait for response headers here.
func (p *DeepSeekProvider) nativeStreamProvider() *OpenAIProvider {
	config := openai.DefaultConfig(p.Config.APIKey)
	config.BaseURL = strings.TrimRight(p.Config.BaseURL, "/")
	config.HTTPClient = zeroTemperatureDoer{next: retryAfterDoer{next: responseHeaderTimeoutClient(p.GetTimeout(), p.Config.CustomHeaders)}}
	return &OpenAIProvider{BaseProvider: p.BaseProvider, client: openai.NewClientWithConfig(config)}
}

// CompleteStream implements structured streaming with terminal status and usage.
func (p *DeepSeekProvider) CompleteStream(ctx context.Context, request *agentllm.CompletionRequest) (<-chan *agentllm.StreamChunk, error) {
	return p.nativeStreamProvider().CompleteStream(ctx, request)
}

// CompleteToolsStream preserves native tool IDs, reasoning content and usage.
func (p *DeepSeekProvider) CompleteToolsStream(ctx context.Context, request *agentllm.CompletionRequest, tools []interfaces.Tool) (<-chan *agentllm.StreamChunk, error) {
	return p.nativeStreamProvider().CompleteToolsStream(ctx, request, tools)
}
