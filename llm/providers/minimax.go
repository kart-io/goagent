package providers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sashabaranov/go-openai"

	"github.com/kart-io/goagent/codec/json"
	shared "github.com/kart-io/goagent/constants"
	agentllm "github.com/kart-io/goagent/llm"
)

// NewMiniMaxWithOptions creates a MiniMax client over the OpenAI-compatible
// protocol, with vision and native tool streaming. Effort "none" disables
// thinking on M3 models and is ignored on M2.x, which lack the switch.
// Requests with images send max_completion_tokens (the only limit the vision
// endpoint reads); text-only requests still send max_tokens.
func NewMiniMaxWithOptions(opts ...agentllm.ClientOption) (*OpenAIProvider, error) {
	return newOpenAICompatible(openAICompatibleSpec{
		provider:              shared.ProviderMiniMax,
		defaultBaseURL:        shared.MiniMaxBaseURL,
		defaultModel:          shared.MiniMaxDefaultModel,
		envAPIKey:             shared.EnvMiniMaxAPIKey,
		envBaseURL:            shared.EnvMiniMaxBaseURL,
		envModel:              shared.EnvMiniMaxModel,
		reasoning:             true,
		thinkingSwitch:        true,
		imageCompletionTokens: true,
		capabilities: []agentllm.Capability{
			agentllm.CapabilityCompletion,
			agentllm.CapabilityChat,
			agentllm.CapabilityStreaming,
			agentllm.CapabilityStructuredStreaming,
			agentllm.CapabilityToolStreaming,
			agentllm.CapabilityToolCalling,
			agentllm.CapabilityVision,
		},
	}, opts...)
}

func isMiniMaxM3(model string) bool {
	return strings.Contains(strings.ToLower(model), "m3")
}

type thinkingDisabledKey struct{}

func withThinkingDisabled(ctx context.Context) context.Context {
	return context.WithValue(ctx, thinkingDisabledKey{}, true)
}

// thinkingDisabledDoer 在请求体中补 thinking 禁用字段，SDK 的请求结构没有该字段。
type thinkingDisabledDoer struct{ next openai.HTTPDoer }

func (d thinkingDisabledDoer) Do(request *http.Request) (*http.Response, error) {
	disabled, _ := request.Context().Value(thinkingDisabledKey{}).(bool)
	if !disabled || request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/chat/completions") {
		return d.next.Do(request)
	}
	data, err := io.ReadAll(request.Body)
	_ = request.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("读取 thinking 请求：%w", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("解析 thinking 请求：%w", err)
	}
	if payload == nil {
		payload = map[string]json.RawMessage{}
	}
	payload["thinking"] = json.RawMessage(`{"type":"disabled"}`)
	data, err = json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request = request.Clone(request.Context())
	request.Body = io.NopCloser(bytes.NewReader(data))
	request.ContentLength = int64(len(data))
	request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	request.Header.Del("Content-Length")
	return d.next.Do(request)
}
