package providers

import (
	"context"
	"net/http"
	"strings"

	"github.com/sashabaranov/go-openai"

	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
)

var _ agentllm.ToolStreamClient = (*DeepSeekProvider)(nil)

// nativeStreamProvider reuses the same SSE decoder and completion checks as
// OpenAI-compatible endpoints, retaining DeepSeek configuration and transport.
// It performs no implicit retries; callers own the request budget.
func (p *DeepSeekProvider) nativeStreamProvider() *OpenAIProvider {
	config := openai.DefaultConfig(p.Config.APIKey)
	config.BaseURL = strings.TrimRight(p.Config.BaseURL, "/")
	config.HTTPClient = retryAfterDoer{next: &deepSeekStreamHTTPClient{client: p.client.Resty().GetClient(), headers: p.Config.CustomHeaders}}
	return &OpenAIProvider{BaseProvider: p.BaseProvider, client: openai.NewClientWithConfig(config)}
}

type deepSeekStreamHTTPClient struct {
	client  *http.Client
	headers map[string]string
}

func (c *deepSeekStreamHTTPClient) Do(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	for key, value := range c.headers {
		request.Header.Set(key, value)
	}
	return c.client.Do(request)
}

// CompleteStream implements structured streaming with terminal status and usage.
func (p *DeepSeekProvider) CompleteStream(ctx context.Context, request *agentllm.CompletionRequest) (<-chan *agentllm.StreamChunk, error) {
	return p.nativeStreamProvider().CompleteStream(ctx, request)
}

// ChatStream preserves role messages through the structured streaming path.
func (p *DeepSeekProvider) ChatStream(ctx context.Context, messages []agentllm.Message) (<-chan *agentllm.StreamChunk, error) {
	return p.CompleteStream(ctx, &agentllm.CompletionRequest{Messages: messages})
}

// CompleteToolsStream preserves native tool IDs, reasoning content and usage.
func (p *DeepSeekProvider) CompleteToolsStream(ctx context.Context, request *agentllm.CompletionRequest, tools []interfaces.Tool) (<-chan *agentllm.StreamChunk, error) {
	return p.nativeStreamProvider().CompleteToolsStream(ctx, request, tools)
}
