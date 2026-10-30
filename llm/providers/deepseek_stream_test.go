package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/utils/json"
)

// writeDeepSeekSSE 按真实 SSE 格式输出帧；done 为 false 时模拟缺少 [DONE] 的截断流。
func writeDeepSeekSSE[T any](w http.ResponseWriter, done bool, frames ...T) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, frame := range frames {
		data, _ := json.Marshal(frame)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	if done {
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

// slowDeepSeekServer 首帧后停顿 1.5s 再发后续帧，并统计请求次数以确认流开始后不重试。
func slowDeepSeekServer(t *testing.T, first string, rest ...string) (*httptest.Server, *int32) {
	t.Helper()
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", first)
		w.(http.Flusher).Flush()
		time.Sleep(1500 * time.Millisecond)
		for _, frame := range rest {
			fmt.Fprintf(w, "data: %s\n\n", frame)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

// 超时只约束响应头：首块之后的流式读取超过超时也不会被截断，自定义请求头仍然注入
func TestDeepSeekTimeoutDoesNotCutStream(t *testing.T) {
	headers := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Get("X-Custom")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"first \"}}]}\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(1500 * time.Millisecond)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"second\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Second),
		agentllm.WithCustomHeaders(map[string]string{"X-Custom": "kept"}))
	require.NoError(t, err)
	chunks, err := client.ChatStream(context.Background(), []agentllm.Message{agentllm.UserMessage("question")})
	require.NoError(t, err)
	var last *agentllm.StreamChunk
	for chunk := range chunks {
		last = chunk
	}
	require.NotNil(t, last)
	require.NoError(t, last.Error)
	require.Equal(t, "first second", last.Content)
	require.Equal(t, "kept", <-headers)
}

// 服务端迟迟不返回响应头时，流式请求在超时附近失败，而不是无限等待
func TestDeepSeekTimeoutBoundsStreamResponseWait(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	client, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Second))
	require.NoError(t, err)
	start := time.Now()
	chunks, err := client.CompleteStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("question")}})
	if err == nil {
		var last *agentllm.StreamChunk
		for chunk := range chunks {
			last = chunk
		}
		require.NotNil(t, last)
		err = last.Error
	}
	require.Error(t, err)
	require.Less(t, time.Since(start), 5*time.Second)
	var netErr net.Error
	require.True(t, errors.As(err, &netErr) && netErr.Timeout(), "超时错误应可识别为 net.Error.Timeout()，实际 %T: %v", err, err)
}

func TestDeepSeekStreamNotCutByTimeout(t *testing.T) {
	server, requests := slowDeepSeekServer(t,
		`{"choices":[{"delta":{"content":"first "}}]}`,
		`{"choices":[{"delta":{"content":"second"},"finish_reason":"stop"}]}`)
	client, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Second))
	require.NoError(t, err)
	tokens, err := client.Stream(context.Background(), "question")
	require.NoError(t, err)
	var got []string
	for token := range tokens {
		got = append(got, token)
	}
	require.Equal(t, []string{"first ", "second"}, got)
	require.EqualValues(t, 1, atomic.LoadInt32(requests))
}

func TestDeepSeekStreamWithToolsNotCutByTimeout(t *testing.T) {
	server, requests := slowDeepSeekServer(t,
		`{"choices":[{"delta":{"content":"thinking "}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":1}"}}]},"finish_reason":"tool_calls"}]}`)
	client, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Second))
	require.NoError(t, err)
	chunks, err := client.StreamWithTools(context.Background(), "question", []interfaces.Tool{&MockTool{}})
	require.NoError(t, err)
	var types []string
	var call *agentllm.ToolCall
	for chunk := range chunks {
		types = append(types, chunk.Type)
		switch chunk.Type {
		case "content":
			require.Equal(t, "thinking ", chunk.Value)
		case "tool_call":
			call = chunk.Value.(*agentllm.ToolCall)
		case "error":
			t.Fatalf("unexpected error chunk: %v", chunk.Value)
		}
	}
	require.Equal(t, []string{"content", "tool_name", "tool_args", "tool_call"}, types)
	require.NotNil(t, call)
	require.Equal(t, "call_1", call.ID)
	require.Equal(t, "lookup", call.Function.Name)
	require.Equal(t, `{"q":1}`, call.Function.Arguments)
	require.EqualValues(t, 1, atomic.LoadInt32(requests))
}

func TestDeepSeekStreamWithMetadataNotCutByTimeout(t *testing.T) {
	server, requests := slowDeepSeekServer(t,
		`{"choices":[{"delta":{"content":"first "}}]}`,
		`{"choices":[{"delta":{"content":"second"},"finish_reason":"stop"}]}`)
	client, err := NewDeepSeekStreamingWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Second), agentllm.WithModel("deepseek-chat"))
	require.NoError(t, err)
	tokens, err := client.StreamWithMetadata(context.Background(), "question")
	require.NoError(t, err)
	var got []TokenWithMetadata
	for token := range tokens {
		got = append(got, token)
	}
	require.Len(t, got, 3)
	require.Equal(t, TokenWithMetadata{Type: "token", Content: "first ", Metadata: map[string]interface{}{"index": 1, "finish_reason": ""}}, got[0])
	require.Equal(t, TokenWithMetadata{Type: "token", Content: "second", Metadata: map[string]interface{}{"index": 2, "finish_reason": "stop"}}, got[1])
	require.Equal(t, TokenWithMetadata{Type: "finish", Metadata: map[string]interface{}{"total_tokens": 2, "model": "deepseek-chat"}}, got[2])
	require.EqualValues(t, 1, atomic.LoadInt32(requests))
}

// 请求阶段失败时保持旧实现的错误类型：*AgentError，错误码 LLM_REQUEST
func TestDeepSeekStreamWithToolsRequestErrorIsLLMRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"bad request","type":"invalid_request_error"}}`)
	}))
	defer server.Close()
	client, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Second))
	require.NoError(t, err)
	_, err = client.StreamWithTools(context.Background(), "question", []interfaces.Tool{&MockTool{}})
	require.Error(t, err)
	require.True(t, agentErrors.IsCode(err, agentErrors.CodeLLMRequest), "got %T: %v", err, err)
}

// Stream 系列都必须把 WithSystemPrompt 作为首条 system 消息发送
func TestDeepSeekStreamMethodsSendSystemPrompt(t *testing.T) {
	cases := map[string]func(t *testing.T, p *DeepSeekStreamingProvider){
		"Stream": func(t *testing.T, p *DeepSeekStreamingProvider) {
			tokens, err := p.Stream(context.Background(), "question")
			require.NoError(t, err)
			for range tokens {
			}
		},
		"StreamWithTools": func(t *testing.T, p *DeepSeekStreamingProvider) {
			chunks, err := p.StreamWithTools(context.Background(), "question", []interfaces.Tool{&MockTool{}})
			require.NoError(t, err)
			for range chunks {
			}
		},
		"StreamWithMetadata": func(t *testing.T, p *DeepSeekStreamingProvider) {
			tokens, err := p.StreamWithMetadata(context.Background(), "question")
			require.NoError(t, err)
			for range tokens {
			}
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			bodies := make(chan map[string]interface{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				bodies <- body
				writeDeepSeekSSE(w, true, json.RawMessage(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer server.Close()
			p, err := NewDeepSeekStreamingWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Second), agentllm.WithSystemPrompt("be brief"))
			require.NoError(t, err)
			run(t, p)
			messages := (<-bodies)["messages"].([]interface{})
			require.Len(t, messages, 2)
			require.Equal(t, map[string]interface{}{"role": "system", "content": "be brief"}, messages[0])
			require.Equal(t, "user", messages[1].(map[string]interface{})["role"])
		})
	}
}

// 工具流在 finish_reason 之前被截断时必须以 error 结束，且不产出不完整的 tool_call
func TestDeepSeekStreamWithToolsTruncatedEndsWithError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeDeepSeekSSE(w, false,
			json.RawMessage(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}}]}}]}`))
	}))
	defer server.Close()
	client, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Second))
	require.NoError(t, err)
	chunks, err := client.StreamWithTools(context.Background(), "question", []interfaces.Tool{&MockTool{}})
	require.NoError(t, err)
	var types []string
	var last agentllm.ToolChunk
	for chunk := range chunks {
		types = append(types, chunk.Type)
		last = chunk
	}
	require.Equal(t, []string{"tool_name", "tool_args", "error"}, types)
	require.ErrorIs(t, last.Value.(error), io.ErrUnexpectedEOF)
}

// 请求阶段 429 的状态码与 Retry-After 须能通过 *agentllm.ProviderError 取得
func TestDeepSeekStreamMethodsExposeProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"slow down","type":"rate_limit"}}`)
	}))
	defer server.Close()
	p, err := NewDeepSeekStreamingWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Second))
	require.NoError(t, err)
	calls := map[string]func() error{
		"Stream": func() error { _, err := p.Stream(context.Background(), "q"); return err },
		"StreamWithTools": func() error {
			_, err := p.StreamWithTools(context.Background(), "q", []interfaces.Tool{&MockTool{}})
			require.True(t, agentErrors.IsCode(err, agentErrors.CodeLLMRequest), "got %T: %v", err, err)
			return err
		},
		"StreamWithMetadata": func() error { _, err := p.StreamWithMetadata(context.Background(), "q"); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			var providerErr *agentllm.ProviderError
			require.True(t, errors.As(err, &providerErr), "got %T: %v", err, err)
			require.Equal(t, http.StatusTooManyRequests, providerErr.StatusCode)
			require.Equal(t, 7*time.Second, providerErr.RetryAfter)
		})
	}
}
