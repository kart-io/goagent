package providers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func temperatureServer(t *testing.T) (*httptest.Server, <-chan map[string]any) {
	t.Helper()
	received := make(chan map[string]any, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		received <- payload
		if payload["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"好\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		} else {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"model":"test","choices":[{"message":{"role":"assistant","content":"好"},"finish_reason":"stop"}]}`)
		}
	}))
	t.Cleanup(server.Close)
	return server, received
}
func checkWireTemperature(t *testing.T, received <-chan map[string]any, want float64) {
	t.Helper()
	select {
	case payload := <-received:
		require.Contains(t, payload, "temperature", "必须实际发送温度，不能依赖服务端默认值")
		require.InDelta(t, want, payload["temperature"], 0.000001)
	case <-time.After(time.Second):
		t.Fatal("未收到线上请求")
	}
}
func TestOpenAIWireTemperatureSelection(t *testing.T) {
	for _, method := range []string{"Complete", "CompleteStream", "CompleteToolsStream", "Stream", "GenerateWithTools", "StreamWithTools"} {
		t.Run(method, func(t *testing.T) {
			server, received := temperatureServer(t)
			p, err := providers.NewOpenAIWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL), llm.WithTemperature(0))
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req := &llm.CompletionRequest{Messages: []llm.Message{{Role: "user", Content: "你好"}}}
			switch method {
			case "Complete":
				_, err = p.Complete(ctx, req)
			case "CompleteStream", "CompleteToolsStream":
				var stream <-chan *llm.StreamChunk
				if method == "CompleteStream" {
					stream, err = p.CompleteStream(ctx, req)
				} else {
					stream, err = p.CompleteToolsStream(ctx, req, nil)
				}
				if err == nil {
					for chunk := range stream {
						require.NoError(t, chunk.Error)
					}
				}
			case "Stream":
				var stream <-chan string
				stream, err = p.Stream(ctx, "你好")
				if err == nil {
					for range stream {
					}
				}
			case "GenerateWithTools":
				_, err = p.GenerateWithTools(ctx, "你好", nil)
			case "StreamWithTools":
				var stream <-chan llm.ToolChunk
				stream, err = p.StreamWithTools(ctx, "你好", nil)
				if err == nil {
					for chunk := range stream {
						require.NotEqual(t, "error", chunk.Type)
					}
				}
			}
			require.NoError(t, err)
			checkWireTemperature(t, received, 0)
		})
	}
}
func TestBuilderExplicitZeroOverridesProvider(t *testing.T) {
	for _, mode := range []string{"同步", "流式", "原生工具流"} {
		t.Run(mode, func(t *testing.T) {
			server, received := temperatureServer(t)
			p, err := providers.NewOpenAIWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL), llm.WithTemperature(0.8))
			require.NoError(t, err)
			b := builder.NewSimpleBuilder(p).WithTemperature(0)
			if mode == "原生工具流" {
				b.WithTools(tools.NewBaseTool("noop", "测试工具", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
					return &interfaces.ToolOutput{Success: true}, nil
				}))
			}
			agent, err := b.Build()
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if mode == "同步" {
				_, err = agent.AsRunnable().Invoke(ctx, "你好")
			} else {
				var stream <-chan builder.AgentEvent
				stream, err = agent.ExecuteStream(ctx, "你好")
				if err == nil {
					for event := range stream {
						require.NoError(t, event.Error)
					}
				}
			}
			require.NoError(t, err)
			checkWireTemperature(t, received, 0)
		})
	}
}
func TestJSONExplicitZeroOverridesProvider(t *testing.T) {
	server, received := temperatureServer(t)
	p, err := providers.NewOpenAIWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL), llm.WithTemperature(0.8))
	require.NoError(t, err)
	var req llm.CompletionRequest
	require.NoError(t, json.Unmarshal([]byte(`{"messages":[{"role":"user","content":"你好"}],"temperature":0}`), &req))
	_, err = p.Complete(context.Background(), &req)
	require.NoError(t, err)
	checkWireTemperature(t, received, 0)
}

func TestProviderWireExplicitZero(t *testing.T) {
	type constructor func(...llm.ClientOption) (llm.Client, error)
	providersToTest := map[string]struct {
		new      constructor
		response string
		nested   string
	}{
		"DeepSeek":    {func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewDeepSeekWithOptions(o...) }, `{"choices":[{"message":{"role":"assistant","content":"好"},"finish_reason":"stop"}]}`, ""},
		"Kimi":        {func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewKimiWithOptions(o...) }, `{"choices":[{"message":{"role":"assistant","content":"好"},"finish_reason":"stop"}]}`, ""},
		"SiliconFlow": {func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewSiliconFlowWithOptions(o...) }, `{"choices":[{"message":{"role":"assistant","content":"好"},"finish_reason":"stop"}]}`, ""},
		"Ollama":      {func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewOllamaWithOptions(o...) }, `{"choices":[{"message":{"role":"assistant","content":"好"},"finish_reason":"stop"}]}`, ""},
		"Gemini":      {func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewGeminiWithOptions(o...) }, `{"candidates":[{"content":{"role":"model","parts":[{"text":"好"}]},"finishReason":"STOP"}]}`, "generationConfig"},
	}
	for name, tc := range providersToTest {
		t.Run(name, func(t *testing.T) {
			for _, fromJSON := range []bool{false, true} {
				t.Run(fmt.Sprintf("请求覆盖=%v", fromJSON), func(t *testing.T) {
					received := make(chan map[string]any, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						var data map[string]any
						if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
							http.Error(w, err.Error(), 400)
							return
						}
						if tc.nested != "" {
							data, _ = data[tc.nested].(map[string]any)
						}
						received <- data
						w.Header().Set("Content-Type", "application/json")
						_, _ = fmt.Fprint(w, tc.response)
					}))
					defer server.Close()
					temperature := 0.0
					if fromJSON {
						temperature = 0.8
					}
					client, err := tc.new(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL), llm.WithTemperature(temperature))
					require.NoError(t, err)
					req := llm.CompletionRequest{Messages: []llm.Message{{Role: "user", Content: "你好"}}}
					if fromJSON {
						require.NoError(t, json.Unmarshal([]byte(`{"messages":[{"role":"user","content":"你好"}],"temperature":0}`), &req))
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_, err = client.Complete(ctx, &req)
					require.NoError(t, err)
					checkWireTemperature(t, received, 0)
				})
			}
		})
	}
}

func TestDefaultAndPositiveWireTemperatureUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options []llm.ClientOption
		req     float64
		want    float64
	}{
		{"默认", nil, 0, 0.7}, {"客户端", []llm.ClientOption{llm.WithTemperature(0.4)}, 0, 0.4}, {"请求", []llm.ClientOption{llm.WithTemperature(0.4)}, 0.2, 0.2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, received := temperatureServer(t)
			options := append([]llm.ClientOption{llm.WithAPIKey("test"), llm.WithBaseURL(server.URL)}, tc.options...)
			p, err := providers.NewOpenAIWithOptions(options...)
			require.NoError(t, err)
			_, err = p.Complete(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{{Role: "user", Content: "你好"}}, Temperature: tc.req})
			require.NoError(t, err)
			checkWireTemperature(t, received, tc.want)
		})
	}
}
func TestDeepSeekNativeStreamZero(t *testing.T) {
	server, received := temperatureServer(t)
	p, err := providers.NewDeepSeekWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL), llm.WithTemperature(0))
	require.NoError(t, err)
	stream, err := p.CompleteToolsStream(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{{Role: "user", Content: "你好"}}}, nil)
	require.NoError(t, err)
	for chunk := range stream {
		require.NoError(t, chunk.Error)
	}
	checkWireTemperature(t, received, 0)
}
