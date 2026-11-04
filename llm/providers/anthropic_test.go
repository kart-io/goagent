package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"
)

type anthropicEvent struct{ name, data string }

// schemaTool 返回真实的 JSON Schema；mockTool 的 ArgsSchema 恒为空。
type schemaTool struct {
	*mockTool
	schema string
}

func (s schemaTool) ArgsSchema() string { return s.schema }

// anthropicServer 按顺序输出 SSE 事件，并记录每次请求的 JSON 体。
func anthropicServer(t *testing.T, events []anthropicEvent, bodies chan<- map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if bodies != nil {
			bodies <- body
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.name, e.data)
		}
	}))
}

func newTestAnthropic(t *testing.T, url string, opts ...agentllm.ClientOption) *AnthropicProvider {
	t.Helper()
	p, err := NewAnthropicWithOptions(append([]agentllm.ClientOption{agentllm.WithAPIKey("test-key"), agentllm.WithBaseURL(url)}, opts...)...)
	require.NoError(t, err)
	return p
}

func drain(t *testing.T, stream <-chan *agentllm.StreamChunk) (deltas []string, final *agentllm.StreamChunk) {
	t.Helper()
	for chunk := range stream {
		if chunk.Done {
			final = chunk
			continue
		}
		deltas = append(deltas, chunk.Delta)
	}
	require.NotNil(t, final)
	return deltas, final
}

var textEvents = []anthropicEvent{
	{"message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5-actual","content":[],"stop_reason":null,"usage":{"input_tokens":10,"cache_read_input_tokens":4,"cache_creation_input_tokens":0,"output_tokens":1}}}`},
	{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你"}}`},
	{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"好"}}`},
	{"content_block_stop", `{"type":"content_block_stop","index":0}`},
	{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":6}}`},
	{"message_stop", `{"type":"message_stop"}`},
}

func TestAnthropicStreamText(t *testing.T) {
	bodies := make(chan map[string]any, 1)
	server := anthropicServer(t, textEvents, bodies)
	defer server.Close()
	p := newTestAnthropic(t, server.URL, agentllm.WithSystemPrompt("配置规则"), agentllm.WithTemperature(0.3))
	stream, err := p.CompleteStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{{Role: "system", Content: "附加规则"}, {Role: "user", Content: "问候"}}, Temperature: 0.2, TemperatureSet: true, TopP: 0.9})
	require.NoError(t, err)
	deltas, final := drain(t, stream)
	require.NoError(t, final.Error)
	require.Equal(t, []string{"你", "好"}, deltas)
	require.Equal(t, "你好", final.Content)
	require.Equal(t, "stop", final.FinishReason)
	require.Equal(t, "claude-opus-5-5-actual", final.ResponseModel)
	require.NotNil(t, final.ProviderState)
	// 缓存读取计入输入总数并单独标出。
	require.Equal(t, 14, final.Usage.PromptTokens)
	require.Equal(t, 4, final.Usage.CachedTokens)
	require.Equal(t, 6, final.Usage.CompletionTokens)

	body := <-bodies
	require.Equal(t, constants.AnthropicDefaultModel, body["model"])
	require.Equal(t, true, body["stream"])
	// SDK 发送的 API 版本拒绝采样参数，显式设置也不得出现在请求中。
	require.NotContains(t, body, "temperature")
	require.NotContains(t, body, "top_p")
	system := body["system"].([]any)
	require.Equal(t, "配置规则\n\n附加规则", system[0].(map[string]any)["text"])
}

func TestAnthropicNativeToolLoopReplaysState(t *testing.T) {
	toolEvents := []anthropicEvent{
		{"message_start", `{"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"usage":{"input_tokens":20,"output_tokens":1}}}`},
		{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-1"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_a","name":"lookup","input":{}}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"1}"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":1}`},
		{"content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_b","name":"lookup","input":{}}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"q\":2}"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":2}`},
		{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`},
		{"message_stop", `{"type":"message_stop"}`},
	}
	bodies := make(chan map[string]any, 2)
	server := anthropicServer(t, toolEvents, bodies)
	defer server.Close()
	p := newTestAnthropic(t, server.URL)
	tools := []interfaces.Tool{schemaTool{mockTool: &mockTool{name: "lookup"}, schema: `{"type":"object","properties":{"q":{"type":"integer"}},"required":["q"],"additionalProperties":false}`}}
	user := agentllm.Message{Role: "user", Content: "查两次"}
	stream, err := p.CompleteToolsStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{user}}, tools)
	require.NoError(t, err)
	_, final := drain(t, stream)
	require.NoError(t, final.Error)
	require.Equal(t, "tool_calls", final.FinishReason)
	require.Len(t, final.ToolCalls, 2)
	require.Equal(t, "toolu_a", final.ToolCalls[0].ID)
	require.JSONEq(t, `{"q":1}`, final.ToolCalls[0].Function.Arguments)
	require.JSONEq(t, `{"q":2}`, final.ToolCalls[1].Function.Arguments)

	first := <-bodies
	tool := first["tools"].([]any)[0].(map[string]any)
	schema := tool["input_schema"].(map[string]any)
	require.Equal(t, false, schema["additionalProperties"])
	require.Equal(t, []any{"q"}, schema["required"])

	// 续接：原生内容（含思考签名）原样回放，两个工具结果合并为一条 user 消息。
	assistant := agentllm.Message{Role: "assistant", ToolCalls: final.ToolCalls, ProviderState: final.ProviderState}
	next := []agentllm.Message{user, assistant, {Role: "tool", ToolCallID: "toolu_a", Content: "一"}, {Role: "tool", ToolCallID: "toolu_b", Content: "二"}}
	stream, err = p.CompleteToolsStream(context.Background(), &agentllm.CompletionRequest{Messages: next}, tools)
	require.NoError(t, err)
	drain(t, stream)
	second := <-bodies
	messages := second["messages"].([]any)
	require.Len(t, messages, 3)
	replayed := messages[1].(map[string]any)["content"].([]any)
	require.Equal(t, "thinking", replayed[0].(map[string]any)["type"])
	require.Equal(t, "sig-1", replayed[0].(map[string]any)["signature"])
	results := messages[2].(map[string]any)["content"].([]any)
	require.Len(t, results, 2)
	require.Equal(t, "toolu_a", results[0].(map[string]any)["tool_use_id"])
	require.Equal(t, "toolu_b", results[1].(map[string]any)["tool_use_id"])
}

func TestAnthropicRejectsUnmatchedToolHistory(t *testing.T) {
	p := newTestAnthropic(t, "http://127.0.0.1:1")
	call := agentllm.ToolCall{ID: "a", Type: "function"}
	call.Function.Name, call.Function.Arguments = "lookup", "{}"
	for name, messages := range map[string][]agentllm.Message{
		"缺少工具结果": {{Role: "user", Content: "q"}, {Role: "assistant", ToolCalls: []agentllm.ToolCall{call}}, {Role: "user", Content: "again"}},
		"多余工具结果": {{Role: "user", Content: "q"}, {Role: "tool", ToolCallID: "x", Content: "r"}},
	} {
		_, err := p.CompleteStream(context.Background(), &agentllm.CompletionRequest{Messages: messages})
		require.Error(t, err, name)
	}
}

func TestAnthropicIncompleteResponseIsError(t *testing.T) {
	events := append([]anthropicEvent{}, textEvents...)
	events[5] = anthropicEvent{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":6}}`}
	server := anthropicServer(t, events, nil)
	defer server.Close()
	stream, err := newTestAnthropic(t, server.URL).CompleteStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{{Role: "user", Content: "q"}}})
	require.NoError(t, err)
	_, final := drain(t, stream)
	require.ErrorContains(t, final.Error, "max_tokens")
	require.Nil(t, final.ProviderState)
	require.Empty(t, final.ToolCalls)
}

// 限流错误同步返回，保留状态码与 Retry-After，且 SDK 不做隐式重试。
func TestAnthropicProviderErrorWithoutSDKRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	defer server.Close()
	p := newTestAnthropic(t, server.URL)
	req := &agentllm.CompletionRequest{Messages: []agentllm.Message{{Role: "user", Content: "q"}}}
	_, streamErr := p.CompleteStream(context.Background(), req)
	_, completeErr := p.Complete(context.Background(), req)
	for name, err := range map[string]error{"stream": streamErr, "complete": completeErr} {
		var providerErr *agentllm.ProviderError
		require.True(t, errors.As(err, &providerErr), "%s: %v", name, err)
		require.Equal(t, http.StatusTooManyRequests, providerErr.StatusCode, name)
		require.Equal(t, 7*time.Second, providerErr.RetryAfter, name)
	}
	require.Equal(t, int32(2), calls.Load())
}

func TestAnthropicComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"msg_3","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"好"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`)
	}))
	defer server.Close()
	resp, err := newTestAnthropic(t, server.URL).Chat(context.Background(), []agentllm.Message{{Role: "user", Content: "q"}})
	require.NoError(t, err)
	require.Equal(t, "好", resp.Content)
	require.Equal(t, "stop", resp.FinishReason)
	require.Equal(t, 5, resp.TokensUsed)
	require.Equal(t, "anthropic", resp.Provider)
}

func TestAnthropicLegacyStringStream(t *testing.T) {
	server := anthropicServer(t, textEvents, nil)
	defer server.Close()
	tokens, err := newTestAnthropic(t, server.URL).Stream(context.Background(), "q")
	require.NoError(t, err)
	var got strings.Builder
	for token := range tokens {
		got.WriteString(token)
	}
	require.Equal(t, "你好", got.String())
}

func TestAnthropicRejectsReasoning(t *testing.T) {
	_, err := NewAnthropicWithOptions(agentllm.WithAPIKey("k"), agentllm.WithReasoning(usage.ReasoningConfig{Effort: "low"}))
	require.ErrorContains(t, err, "does not support")
	p := newTestAnthropic(t, "http://127.0.0.1:1")
	_, err = p.CompleteToolsStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{{Role: "user", Content: "q"}}, Reasoning: &usage.ReasoningConfig{Effort: "low"}}, nil)
	require.ErrorContains(t, err, "does not support")
}

func TestAnthropicRequiresAPIKey(t *testing.T) {
	t.Setenv(constants.EnvAnthropicAPIKey, "")
	_, err := NewAnthropicWithOptions()
	require.Error(t, err)
}
