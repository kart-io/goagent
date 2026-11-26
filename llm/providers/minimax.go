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
// endpoint reads); text-only requests still send max_tokens. Every chat request
// sets reasoning_split, so thinking arrives as ReasoningContent rather than as
// <think> text inside Content.
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
		reasoningSplit:        true,
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
	if !disabled || !isChatCompletionsPost(request) {
		return d.next.Do(request)
	}
	request, err := withBodyField(request, "thinking", json.RawMessage(`{"type":"disabled"}`))
	if err != nil {
		return nil, fmt.Errorf("改写 thinking 请求：%w", err)
	}
	return d.next.Do(request)
}

// reasoningSplitDoer 为每个 chat/completions 请求补 reasoning_split:true。MiniMax 缺省把思维链以
// <think>…</think> 内联在 content 中；开启后思维改走 reasoning_content，由流解析写入 ReasoningContent。
// thinking 关闭时没有思维输出，字段无作用，因此不按推理配置区分。
type reasoningSplitDoer struct{ next openai.HTTPDoer }

func (d reasoningSplitDoer) Do(request *http.Request) (*http.Response, error) {
	if !isChatCompletionsPost(request) {
		return d.next.Do(request)
	}
	request, err := withBodyField(request, "reasoning_split", json.RawMessage(`true`))
	if err != nil {
		return nil, fmt.Errorf("改写 reasoning_split 请求：%w", err)
	}
	return d.next.Do(request)
}

func isChatCompletionsPost(request *http.Request) bool {
	return request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/chat/completions")
}

// withBodyField 返回请求体 JSON 对象多出一个顶层字段的请求副本，用于 SDK 请求结构没有的字段。
func withBodyField(request *http.Request, field string, value json.RawMessage) (*http.Request, error) {
	data, err := io.ReadAll(request.Body)
	_ = request.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("读取请求体：%w", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("解析请求体：%w", err)
	}
	if payload == nil {
		payload = map[string]json.RawMessage{}
	}
	payload[field] = value
	data, err = json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request = request.Clone(request.Context())
	request.Body = io.NopCloser(bytes.NewReader(data))
	request.ContentLength = int64(len(data))
	request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	request.Header.Del("Content-Length")
	return request, nil
}
