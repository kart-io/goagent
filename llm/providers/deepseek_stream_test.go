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

	"github.com/kart-io/goagent/codec/json"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
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
	chunks, err := client.CompleteStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("question")}})
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

// Stream 系列都必须把 WithSystemPrompt 作为首条 system 消息发送
func TestDeepSeekStreamMethodsSendSystemPrompt(t *testing.T) {
	cases := map[string]func(t *testing.T, p *DeepSeekStreamingProvider){
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
				_ = json.Decode(r.Body, &body)
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

// 连接在 finish_reason 之前关闭是暂时性故障：原生工具流的错误须可被 ClassifyError 判为可重试，
// 否则 RetryTransient 不会重发尚未产出内容的请求。
func TestDeepSeekNativeToolStreamTruncationIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeDeepSeekSSE(w, false, json.RawMessage(`{"choices":[{"delta":{"content":""}}]}`))
	}))
	defer server.Close()
	client, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Second))
	require.NoError(t, err)
	chunks, err := client.CompleteToolsStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("q")}}, []interfaces.Tool{&MockTool{}})
	require.NoError(t, err)
	var last *agentllm.StreamChunk
	for chunk := range chunks {
		last = chunk
	}
	require.NotNil(t, last)
	require.ErrorIs(t, last.Error, io.ErrUnexpectedEOF)
	require.True(t, agentllm.ClassifyError(last.Error).Retryable)
}

// 调用方超时打断流时，若发出终态，须报告超时而不是可重试的 unexpected EOF。调用方可能已不再接收，终态可以不发。
func TestDeepSeekNativeToolStreamDeadlineIsNotEOF(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"思考\"}}]}\n\n"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	client, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Minute))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	chunks, err := client.CompleteToolsStream(ctx, &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("q")}}, []interfaces.Tool{&MockTool{}})
	require.NoError(t, err)
	var last *agentllm.StreamChunk
	for chunk := range chunks {
		last = chunk
	}
	if last != nil && last.Error != nil {
		require.ErrorIs(t, last.Error, context.DeadlineExceeded)
		require.False(t, agentllm.ClassifyError(last.Error).Retryable)
	}
}

// 首包之后服务端不再发数据：空闲时限到期中断，报告可重试的超时，交给 RetryTransient 重发。
func TestDeepSeekNativeToolStreamIdleTimeoutIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"\"}}]}\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	client, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Minute),
		agentllm.WithStreamIdleTimeout(100*time.Millisecond))
	require.NoError(t, err)
	start := time.Now()
	chunks, err := client.CompleteToolsStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("q")}}, []interfaces.Tool{&MockTool{}})
	require.NoError(t, err)
	var last *agentllm.StreamChunk
	for chunk := range chunks {
		last = chunk
	}
	require.Less(t, time.Since(start), 5*time.Second)
	require.NotNil(t, last)
	require.ErrorContains(t, last.Error, "没有新数据")
	require.True(t, agentllm.ClassifyError(last.Error).Retryable)
}

// 数据一点点地来，空闲时限触发不了，总时限到期中断。
func TestDeepSeekNativeToolStreamTotalTimeoutIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for {
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"\"}}]}\n\n"))
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}))
	defer server.Close()
	client, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Minute),
		agentllm.WithStreamIdleTimeout(time.Second), agentllm.WithStreamTimeout(300*time.Millisecond))
	require.NoError(t, err)
	chunks, err := client.CompleteToolsStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("q")}}, []interfaces.Tool{&MockTool{}})
	require.NoError(t, err)
	var last *agentllm.StreamChunk
	for chunk := range chunks {
		last = chunk
	}
	require.NotNil(t, last)
	require.ErrorContains(t, last.Error, "仍未结束")
	require.True(t, agentllm.ClassifyError(last.Error).Retryable)
}

// 首包前停住同样由空闲时限中断；时限内正常结束的流不受影响。
func TestDeepSeekNativeToolStreamTimeoutsBeforeHeadersAndOnSuccess(t *testing.T) {
	// 读完请求体，服务端才会察觉客户端断开并结束处理，Close 不会一直等。
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer hang.Close()
	client, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(hang.URL), agentllm.WithTimeout(time.Minute),
		agentllm.WithStreamIdleTimeout(100*time.Millisecond))
	require.NoError(t, err)
	_, err = client.CompleteToolsStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("q")}}, []interfaces.Tool{&MockTool{}})
	require.ErrorContains(t, err, "没有新数据")
	require.True(t, agentllm.ClassifyError(err).Retryable)

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeDeepSeekSSE(w, true, json.RawMessage(`{"choices":[{"delta":{"content":"好"},"finish_reason":"stop"}]}`))
	}))
	defer ok.Close()
	client, err = NewDeepSeekWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(ok.URL), agentllm.WithTimeout(time.Minute),
		agentllm.WithStreamIdleTimeout(time.Second), agentllm.WithStreamTimeout(time.Second))
	require.NoError(t, err)
	chunks, err := client.CompleteToolsStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("q")}}, []interfaces.Tool{&MockTool{}})
	require.NoError(t, err)
	var last *agentllm.StreamChunk
	for chunk := range chunks {
		last = chunk
	}
	require.NoError(t, last.Error)
	require.Equal(t, "stop", last.FinishReason)
}
