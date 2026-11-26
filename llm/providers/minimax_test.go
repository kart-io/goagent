package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/usage"
	agentllm "github.com/kart-io/goagent/llm"
)

// captureTripper 记录请求并返回最小的 SSE 成功响应。
type captureTripper struct {
	mu     sync.Mutex
	urls   []string
	bodies []map[string]json.RawMessage
}

func (c *captureTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	data, _ := io.ReadAll(req.Body)
	var body map[string]json.RawMessage
	_ = json.Unmarshal(data, &body)
	c.mu.Lock()
	c.urls = append(c.urls, req.URL.String())
	c.bodies = append(c.bodies, body)
	c.mu.Unlock()
	sse := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
		Request:    req,
	}, nil
}

func newCapturedMiniMax(t *testing.T, opts ...agentllm.ClientOption) (*OpenAIProvider, *captureTripper) {
	t.Helper()
	t.Setenv("MINIMAX_BASE_URL", "")
	t.Setenv("MINIMAX_MODEL", "")
	tripper := &captureTripper{}
	opts = append([]agentllm.ClientOption{agentllm.WithAPIKey("k"), agentllm.WithHTTPClient(&http.Client{Transport: tripper})}, opts...)
	p, err := NewMiniMaxWithOptions(opts...)
	require.NoError(t, err)
	return p, tripper
}

func noneRequest() *agentllm.CompletionRequest {
	return &agentllm.CompletionRequest{
		Messages:  []agentllm.Message{agentllm.UserMessage("hi")},
		Reasoning: &usage.ReasoningConfig{Effort: "none"},
	}
}

func drainStream(t *testing.T, ch <-chan *agentllm.StreamChunk, err error) {
	t.Helper()
	require.NoError(t, err)
	for range ch {
	}
}

func TestMiniMaxM3DisablesThinking(t *testing.T) {
	p, tripper := newCapturedMiniMax(t, agentllm.WithModel("MiniMax-M3"))

	_, err := agentllm.Complete(context.Background(), p, noneRequest())
	require.NoError(t, err)
	ch, err := p.CompleteToolsStream(context.Background(), noneRequest(), nil)
	drainStream(t, ch, err)

	require.Len(t, tripper.bodies, 2)
	for _, body := range tripper.bodies {
		require.JSONEq(t, `{"type":"disabled"}`, string(body["thinking"]))
		require.NotContains(t, body, "reasoning_effort")
	}
}

func TestMiniMaxM27IgnoresNoneEffort(t *testing.T) {
	p, tripper := newCapturedMiniMax(t, agentllm.WithModel("minimax-m2.7"))

	_, err := agentllm.Complete(context.Background(), p, noneRequest())
	require.NoError(t, err)

	require.Len(t, tripper.bodies, 1)
	require.NotContains(t, tripper.bodies[0], "thinking")
	require.NotContains(t, tripper.bodies[0], "reasoning_effort")
}

func TestMiniMaxM3OtherEffortUsesReasoningEffort(t *testing.T) {
	p, tripper := newCapturedMiniMax(t, agentllm.WithModel("MiniMax-M3"))
	req := noneRequest()
	req.Reasoning = &usage.ReasoningConfig{Effort: "low"}

	_, err := agentllm.Complete(context.Background(), p, req)
	require.NoError(t, err)

	require.NotContains(t, tripper.bodies[0], "thinking")
	require.JSONEq(t, `"low"`, string(tripper.bodies[0]["reasoning_effort"]))
}

func TestMiniMaxDefaults(t *testing.T) {
	p, tripper := newCapturedMiniMax(t)

	_, err := agentllm.Complete(context.Background(), p, &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("hi")}})
	require.NoError(t, err)

	require.Equal(t, []string{"https://api.minimaxi.com/v1/chat/completions"}, tripper.urls)
	require.JSONEq(t, `"minimax-m2.7"`, string(tripper.bodies[0]["model"]))
	require.True(t, p.HasCapability(agentllm.CapabilityVision))
	require.True(t, p.HasCapability(agentllm.CapabilityToolStreaming))
}

func TestMiniMaxImageRequestsUseMaxCompletionTokens(t *testing.T) {
	imageMessage := agentllm.Message{Role: "user", Content: "describe", Images: []agentllm.ImagePart{{URL: "https://example.com/a.png"}}}
	tests := []struct {
		name          string
		newClient     func(*testing.T) (*OpenAIProvider, *captureTripper)
		message       agentllm.Message
		send          func(*testing.T, *OpenAIProvider, *agentllm.CompletionRequest)
		wantField     string
		unwantedField string
	}{
		{name: "minimax image complete", newClient: minimaxCapture, message: imageMessage, send: sendComplete, wantField: "max_completion_tokens", unwantedField: "max_tokens"},
		{name: "minimax image stream", newClient: minimaxCapture, message: imageMessage, send: sendStream, wantField: "max_completion_tokens", unwantedField: "max_tokens"},
		{name: "minimax image tool stream", newClient: minimaxCapture, message: imageMessage, send: sendToolsStream, wantField: "max_completion_tokens", unwantedField: "max_tokens"},
		{name: "minimax text complete", newClient: minimaxCapture, message: agentllm.UserMessage("hi"), send: sendComplete, wantField: "max_tokens", unwantedField: "max_completion_tokens"},
		{name: "minimax text tool stream", newClient: minimaxCapture, message: agentllm.UserMessage("hi"), send: sendToolsStream, wantField: "max_tokens", unwantedField: "max_completion_tokens"},
		{name: "openai image tool stream", newClient: openAICapture, message: imageMessage, send: sendToolsStream, wantField: "max_tokens", unwantedField: "max_completion_tokens"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, tripper := tt.newClient(t)
			tt.send(t, p, &agentllm.CompletionRequest{Messages: []agentllm.Message{tt.message}, MaxTokens: 321})

			require.Len(t, tripper.bodies, 1)
			body := tripper.bodies[0]
			require.JSONEq(t, `321`, string(body[tt.wantField]))
			require.NotContains(t, body, tt.unwantedField)
			require.Contains(t, body, "messages")
			require.Contains(t, body, "stream_options")
		})
	}
}

func minimaxCapture(t *testing.T) (*OpenAIProvider, *captureTripper) {
	return newCapturedMiniMax(t)
}

func openAICapture(t *testing.T) (*OpenAIProvider, *captureTripper) {
	t.Helper()
	tripper := &captureTripper{}
	p, err := NewOpenAIWithOptions(agentllm.WithAPIKey("k"), agentllm.WithBaseURL("https://api.openai.test/v1"), agentllm.WithModel("gpt-4o"), agentllm.WithHTTPClient(&http.Client{Transport: tripper}))
	require.NoError(t, err)
	return p, tripper
}

func sendComplete(t *testing.T, p *OpenAIProvider, req *agentllm.CompletionRequest) {
	_, err := agentllm.Complete(context.Background(), p, req)
	require.NoError(t, err)
}

func sendStream(t *testing.T, p *OpenAIProvider, req *agentllm.CompletionRequest) {
	ch, err := p.CompleteStream(context.Background(), req)
	drainStream(t, ch, err)
}

func sendToolsStream(t *testing.T, p *OpenAIProvider, req *agentllm.CompletionRequest) {
	ch, err := p.CompleteToolsStream(context.Background(), req, nil)
	drainStream(t, ch, err)
}

// MiniMax 默认把思维链以 <think> 内联在 content 中，须请求 reasoning_split 才分到 reasoning_content。
// 关闭 thinking 时同样发送：字段对无思维输出的请求没有作用，规则保持一条。
func TestMiniMaxRequestsReasoningSplit(t *testing.T) {
	tests := []struct {
		name  string
		model string
		req   func() *agentllm.CompletionRequest
		send  func(*testing.T, *OpenAIProvider, *agentllm.CompletionRequest)
	}{
		{name: "M3 complete", model: "MiniMax-M3", req: textRequest, send: sendComplete},
		{name: "M3 tool stream", model: "MiniMax-M3", req: textRequest, send: sendToolsStream},
		{name: "M3 thinking disabled", model: "MiniMax-M3", req: noneRequest, send: sendToolsStream},
		{name: "M2.7 stream", model: "minimax-m2.7", req: textRequest, send: sendStream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, tripper := newCapturedMiniMax(t, agentllm.WithModel(tt.model))
			tt.send(t, p, tt.req())

			require.Len(t, tripper.bodies, 1)
			require.JSONEq(t, `true`, string(tripper.bodies[0]["reasoning_split"]))
		})
	}
}

func TestOpenAIDoesNotSendReasoningSplit(t *testing.T) {
	p, tripper := openAICapture(t)
	sendToolsStream(t, p, textRequest())

	require.Len(t, tripper.bodies, 1)
	require.NotContains(t, tripper.bodies[0], "reasoning_split")
}

func textRequest() *agentllm.CompletionRequest {
	return &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("hi")}}
}

// minimaxSplitSSE 取自 MiniMax-M3 在 reasoning_split 下的真实流（id 与时间戳已替换）。
const minimaxSplitSSE = `data: {"id":"x","choices":[{"index":0,"delta":{"role":"assistant"}}],"model":"MiniMax-M3","object":"chat.completion.chunk"}

data: {"id":"x","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"We","reasoning_details":[{"type":"reasoning.text","id":"reasoning-text-1","format":"MiniMax-response-v1","index":0,"text":"We"}]}}],"model":"MiniMax-M3","object":"chat.completion.chunk"}

data: {"id":"x","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":" need answer in one Chinese sentence.","reasoning_details":[{"type":"reasoning.text","id":"reasoning-text-1","format":"MiniMax-response-v1","index":0,"text":" need answer in one Chinese sentence."}]}}],"model":"MiniMax-M3","object":"chat.completion.chunk"}

data: {"id":"x","choices":[{"index":0,"delta":{"content":"1","role":"assistant"}}],"model":"MiniMax-M3","object":"chat.completion.chunk"}

data: {"id":"x","choices":[{"index":0,"delta":{"content":"+1等于2。","role":"assistant"}}],"model":"MiniMax-M3","object":"chat.completion.chunk"}

data: {"id":"x","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"model":"MiniMax-M3","object":"chat.completion.chunk","usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}

data: [DONE]

`

// replayTripper 记录请求体并回放固定的 SSE 响应。
type replayTripper struct {
	sse    string
	bodies []map[string]json.RawMessage
}

func (r *replayTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	data, _ := io.ReadAll(req.Body)
	var body map[string]json.RawMessage
	_ = json.Unmarshal(data, &body)
	r.bodies = append(r.bodies, body)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(r.sse)),
		Request:    req,
	}, nil
}

func TestMiniMaxSplitStreamKeepsThinkingOutOfContent(t *testing.T) {
	t.Setenv("MINIMAX_BASE_URL", "")
	tripper := &replayTripper{sse: minimaxSplitSSE}
	p, err := NewMiniMaxWithOptions(agentllm.WithAPIKey("k"), agentllm.WithModel("MiniMax-M3"), agentllm.WithHTTPClient(&http.Client{Transport: tripper}))
	require.NoError(t, err)

	ch, err := p.CompleteToolsStream(context.Background(), textRequest(), nil)
	require.NoError(t, err)
	var deltas strings.Builder
	var terminal *agentllm.StreamChunk
	for chunk := range ch {
		deltas.WriteString(chunk.Delta)
		if chunk.Done {
			terminal = chunk
		}
	}

	require.Len(t, tripper.bodies, 1)
	require.JSONEq(t, `true`, string(tripper.bodies[0]["reasoning_split"]))
	require.NotNil(t, terminal)
	require.NoError(t, terminal.Error)
	require.Equal(t, "1+1等于2。", terminal.Content)
	require.Equal(t, "1+1等于2。", deltas.String())
	require.NotContains(t, terminal.Content, "<think>")
	require.Equal(t, "We need answer in one Chinese sentence.", terminal.ReasoningContent)
}
