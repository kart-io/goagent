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
