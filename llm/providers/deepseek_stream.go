package providers

import (
	"context"
	"strings"

	"github.com/sashabaranov/go-openai"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
)

var _ agentllm.ToolStreamClient = (*DeepSeekProvider)(nil)

// nativeStreamProvider reuses the same SSE decoder and completion checks as
// OpenAI-compatible endpoints, retaining DeepSeek configuration and transport.
// It performs no implicit retries; callers own the request budget.
// DeepSeek does not accept reasoning_effort: effort "none" is sent as
// thinking {"type":"disabled"} and any other reasoning configuration is rejected.
// The resty client's overall Timeout would cut long SSE bodies, so the timeout
// only bounds the wait for response headers here.
func (p *DeepSeekProvider) nativeStreamProvider() *OpenAIProvider {
	config := openai.DefaultConfig(p.Config.APIKey)
	config.BaseURL = strings.TrimRight(p.Config.BaseURL, "/")
	config.HTTPClient = zeroTemperatureDoer{next: thinkingDisabledDoer{next: retryAfterDoer{next: responseHeaderTimeoutClient(p.GetTimeout(), p.Config.CustomHeaders, p.Config.HTTPClient)}}}
	return &OpenAIProvider{BaseProvider: p.BaseProvider, client: openai.NewClientWithConfig(config), thinkingNoneOnly: true}
}

// rejectImages refuses image input before any request is built: DeepSeek does
// not accept images.
func rejectImages(request *agentllm.CompletionRequest) error {
	if request == nil {
		return nil
	}
	for _, msg := range request.Messages {
		if len(msg.Images) > 0 {
			return agentErrors.New(agentErrors.CodeInvalidInput, "deepseek: images not supported").
				WithComponent("deepseek").
				WithOperation("build_request")
		}
	}
	return nil
}

// CompleteStream implements structured streaming with terminal status and usage.
func (p *DeepSeekProvider) CompleteStream(ctx context.Context, request *agentllm.CompletionRequest) (<-chan *agentllm.StreamChunk, error) {
	if err := rejectImages(request); err != nil {
		return nil, err
	}
	return p.nativeStreamProvider().CompleteStream(ctx, request)
}

// CompleteToolsStream preserves native tool IDs, reasoning content and usage.
// DSML tool calls written into the content are restored as ToolCalls; see
// recoverDSMLToolCalls.
func (p *DeepSeekProvider) CompleteToolsStream(ctx context.Context, request *agentllm.CompletionRequest, tools []interfaces.Tool) (<-chan *agentllm.StreamChunk, error) {
	if err := rejectImages(request); err != nil {
		return nil, err
	}
	chunks, err := p.nativeStreamProvider().CompleteToolsStream(ctx, request, tools)
	if err != nil || len(tools) == 0 {
		return chunks, err
	}
	return recoverDSMLToolCalls(ctx, chunks, tools), nil
}
