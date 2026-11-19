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

// NewMiniMaxWithOptions 创建 MiniMax 客户端，走 OpenAI 兼容协议，支持视觉与原生工具流。
// effort=none 时 M3 系列改发 thinking 禁用；M2.x 不接受该开关，none 被忽略。
// 视觉接口只读 max_completion_tokens，带图片的请求改发该字段，纯文本请求仍发 max_tokens。
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
