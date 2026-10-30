package providers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/stretchr/testify/require"
)

// 三种传输使用相同语义输入，仅在测试边界编码各自已支持的协议。
func hardeningProvider(t *testing.T, name, endpoint string) llm.ToolStreamClient {
	t.Helper()
	if name != "gemini" {
		return contractProvider(t, name, endpoint)
	}
	p, err := providers.NewGeminiWithOptions(llm.WithAPIKey("contract-test"), llm.WithBaseURL(endpoint), llm.WithModel("contract-model"), llm.WithTimeout(3*time.Second))
	require.NoError(t, err)
	return p
}

func hardeningFrames(name string, complete, usage bool) []any {
	args := map[string]any{"query": "中文 \"quoted\"", "nested": map[string]any{"ids": []int{1, 2}}}
	if name == "gemini" {
		frames := []any{map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"functionCall": map[string]any{"id": "call-1", "name": "lookup", "args": args}}}}}}}}
		if complete {
			frames = append(frames, map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": ""}}}, "finishReason": "STOP"}}})
		}
		if usage {
			frames = append(frames, map[string]any{"usageMetadata": map[string]int{"promptTokenCount": 3, "candidatesTokenCount": 2, "totalTokenCount": 5}})
		}
		return frames
	}
	encoded, _ := json.Marshal(args)
	split := len(encoded) / 2
	frames := []any{}
	for i, part := range []string{string(encoded[:split]), string(encoded[split:])} {
		call := map[string]any{"index": 0, "function": map[string]any{"arguments": part}}
		if i == 0 {
			call["id"] = "call-1"
			call["type"] = "function"
			call["function"].(map[string]any)["name"] = "lookup"
		}
		frames = append(frames, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{call}}}}})
	}
	if complete {
		frames = append(frames, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}}})
	}
	if usage {
		frames = append(frames, map[string]any{"choices": []any{}, "usage": map[string]int{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}})
	}
	return frames
}

func writeHardeningFrames(w http.ResponseWriter, name string, frames []any) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, frame := range frames {
		encoded, _ := json.Marshal(frame)
		fmt.Fprintf(w, "data: %s\n\n", encoded)
	}
	if name != "gemini" {
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

// 旧测试主要验证已知 usage；这里验证工具原生流不能把未知用量伪造为零用量。
// 同时覆盖 DeepSeek 与 Gemini 对完整参数和终态前不暴露工具的共同契约。
func TestStructuredProvidersToolCompletionAndUnknownUsage(t *testing.T) {
	for _, name := range []string{"openai", "deepseek", "gemini"} {
		for _, usage := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/usage=%t", name, usage), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					writeHardeningFrames(w, name, hardeningFrames(name, true, usage))
				}))
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				stream, err := hardeningProvider(t, name, server.URL).CompleteToolsStream(ctx, &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("query")}}, nil)
				require.NoError(t, err)
				var final *llm.StreamChunk
				terminals := 0
				for chunk := range stream {
					require.NoError(t, chunk.Error)
					if !chunk.Done {
						require.Empty(t, chunk.ToolCalls, "工具只能在完整终态交给调用方")
						continue
					}
					terminals++
					final = chunk
				}
				require.Equal(t, 1, terminals)
				require.NotNil(t, final)
				require.Equal(t, "tool_calls", final.FinishReason)
				require.Len(t, final.ToolCalls, 1)
				require.Equal(t, "call-1", final.ToolCalls[0].ID)
				require.Equal(t, "lookup", final.ToolCalls[0].Function.Name)
				require.JSONEq(t, `{"query":"中文 \"quoted\"","nested":{"ids":[1,2]}}`, final.ToolCalls[0].Function.Arguments)
				if usage {
					require.NotNil(t, final.Usage)
					require.Equal(t, 5, final.Usage.TotalTokens)
				} else {
					require.Nil(t, final.Usage)
				}
			})
		}
	}
}

// 消费者在生产者发送下一块时取消，也必须关闭 HTTP 与输出 channel。
func TestStructuredProvidersCancelWithoutDraining(t *testing.T) {
	for _, name := range []string{"openai", "deepseek", "gemini"} {
		t.Run(name, func(t *testing.T) {
			closed := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(closed)
				w.Header().Set("Content-Type", "text/event-stream")
				frame := `{"choices":[{"index":0,"delta":{"content":"partial"}}]}`
				if name == "gemini" {
					frame = `{"candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]}}]}`
				}
				for i := 0; i < 3; i++ {
					fmt.Fprintf(w, "data: %s\n\n", frame)
				}
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, err := hardeningProvider(t, name, server.URL).CompleteToolsStream(ctx, &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("query")}}, nil)
			require.NoError(t, err)
			cancel()
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("取消后上游没有退出")
			}
			timeout := time.After(time.Second)
			for {
				select {
				case _, ok := <-stream:
					if !ok {
						return
					}
				case <-timeout:
					t.Fatal("取消后生产者没有关闭 channel")
				}
			}
		})
	}
}
