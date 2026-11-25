package providers

import (
	"context"
	"net/http"
	"testing"

	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
)

func capturedOpenAI(t *testing.T, model string) (*OpenAIProvider, *captureTripper) {
	t.Helper()
	tripper := &captureTripper{}
	p, err := NewOpenAIWithOptions(agentllm.WithAPIKey("k"), agentllm.WithBaseURL("https://api.openai.test/v1"), agentllm.WithModel(model), agentllm.WithHTTPClient(&http.Client{Transport: tripper}))
	require.NoError(t, err)
	return p, tripper
}

// 推理模型不接受 max_tokens 与非默认采样参数，SDK 会在发送前拒绝；请求要按模型改写后真正发出。
func TestOpenAIReasoningModelRequestFields(t *testing.T) {
	image := agentllm.Message{Role: "user", Content: "describe", Images: []agentllm.ImagePart{{URL: "https://example.com/a.png"}}}
	sends := map[string]func(*testing.T, *OpenAIProvider, *agentllm.CompletionRequest){
		"complete": sendComplete, "stream": sendStream, "tools stream": sendToolsStream,
	}
	tests := []struct {
		name         string
		model        string
		message      agentllm.Message
		effort       string
		wantTokens   string
		keepSampling bool
	}{
		{name: "普通模型", model: "gpt-4o", message: agentllm.UserMessage("hi"), wantTokens: "max_tokens", keepSampling: true},
		{name: "普通模型带图", model: "gpt-4o", message: image, wantTokens: "max_tokens", keepSampling: true},
		{name: "o3-mini", model: "o3-mini", message: agentllm.UserMessage("hi"), wantTokens: "max_completion_tokens"},
		{name: "o1 带图", model: "o1", message: image, wantTokens: "max_completion_tokens"},
		{name: "gpt-5", model: "gpt-5", message: agentllm.UserMessage("hi"), wantTokens: "max_completion_tokens"},
		{name: "gpt-6-sol 默认推理", model: openai.GPT6Sol, message: agentllm.UserMessage("hi"), wantTokens: "max_completion_tokens"},
		{name: "gpt-6.2-sol 指定推理强度", model: openai.GPT6Dot2Sol, message: agentllm.UserMessage("hi"), effort: "high", wantTokens: "max_completion_tokens"},
		{name: "gpt-6-sol 关闭推理保留采样", model: openai.GPT6Sol, message: agentllm.UserMessage("hi"), effort: "none", wantTokens: "max_completion_tokens", keepSampling: true},
	}
	for _, tt := range tests {
		for sendName, send := range sends {
			t.Run(tt.name+"/"+sendName, func(t *testing.T) {
				p, tripper := capturedOpenAI(t, tt.model)
				req := &agentllm.CompletionRequest{Messages: []agentllm.Message{tt.message}, MaxTokens: 321, TopP: 0.5}
				if tt.effort != "" {
					req.Reasoning = &usage.ReasoningConfig{Effort: tt.effort}
				}
				send(t, p, req)

				require.Len(t, tripper.bodies, 1)
				body := tripper.bodies[0]
				require.JSONEq(t, `321`, string(body[tt.wantTokens]))
				otherTokens := map[string]string{"max_tokens": "max_completion_tokens", "max_completion_tokens": "max_tokens"}[tt.wantTokens]
				require.NotContains(t, body, otherTokens)
				if tt.keepSampling {
					require.Contains(t, body, "temperature")
					require.JSONEq(t, `0.5`, string(body["top_p"]))
					return
				}
				require.NotContains(t, body, "temperature")
				require.NotContains(t, body, "top_p")
			})
		}
	}
}

// 显式零温对推理模型同样不发送：零温注入会让 SDK 已放行的请求在服务端被拒。
func TestOpenAIReasoningModelSkipsZeroTemperatureInjection(t *testing.T) {
	p, tripper := capturedOpenAI(t, "o3-mini")
	sendStream(t, p, &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("hi")}, Temperature: 0, TemperatureSet: true})

	require.Len(t, tripper.bodies, 1)
	require.NotContains(t, tripper.bodies[0], "temperature")
}

// gpt-6 开启推理时工具调用只能走 Responses API：请求不发出，错误明确且不可重试。
func TestOpenAIGPT6ToolsRequireResponsesAPI(t *testing.T) {
	p, tripper := capturedOpenAI(t, openai.GPT6Sol)
	tools := []interfaces.Tool{&mockTool{name: "lookup", description: "lookup", schema: map[string]interface{}{"type": "object"}}}
	ch, err := p.CompleteToolsStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("hi")}}, tools)

	require.Nil(t, ch)
	require.ErrorIs(t, err, openai.ErrReasoningToolsRequireResponses)
	require.False(t, agentllm.ClassifyError(err).Retryable)
	require.Empty(t, tripper.bodies)
}
