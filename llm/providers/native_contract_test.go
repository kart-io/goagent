package providers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/stretchr/testify/require"
)

func contractProvider(t *testing.T, name, endpoint string) llm.ToolStreamClient {
	t.Helper()
	opts := []llm.ClientOption{llm.WithAPIKey("contract-test"), llm.WithBaseURL(endpoint), llm.WithModel("contract-model"), llm.WithTimeout(3 * time.Second), llm.WithCustomHeaders(map[string]string{"X-Contract": "native"})}
	if name == "deepseek" {
		client, err := providers.NewDeepSeekWithOptions(opts...)
		require.NoError(t, err)
		return client
	}
	client, err := providers.NewOpenAIWithOptions(opts...)
	require.NoError(t, err)
	return client
}

type contractTool struct{ calls atomic.Int32 }

func (*contractTool) Name() string        { return "lookup" }
func (*contractTool) Description() string { return "lookup" }
func (*contractTool) ArgsSchema() string  { return `{"type":"object","properties":{}}` }
func (t *contractTool) Invoke(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	t.calls.Add(1)
	return &interfaces.ToolOutput{Success: true, Result: "42"}, nil
}

func TestNativeProvidersShareToolCompletionContract(t *testing.T) {
	for _, name := range []string{"openai", "deepseek"} {
		for _, mode := range []string{"loop", "direct", "incomplete", "truncated"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					var req struct {
						Model    string        `json:"model"`
						Messages []llm.Message `json:"messages"`
					}
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						http.Error(w, err.Error(), 400)
						return
					}
					if req.Model != "contract-model" || r.Header.Get("Authorization") != "Bearer contract-test" {
						http.Error(w, "configuration lost", 400)
						return
					}
					if name == "deepseek" && r.Header.Get("X-Contract") != "native" {
						http.Error(w, "custom header lost", 400)
						return
					}
					hasResult := false
					for i, message := range req.Messages {
						if message.Role == "tool" {
							hasResult = true
							if i == 0 || message.Content != "42" || message.ToolCallID != "lookup-1" || req.Messages[i-1].ReasoningContent != "check evidence" {
								http.Error(w, "tool continuation lost", 400)
								return
							}
						}
					}
					delta := map[string]any{"role": "assistant", "content": "42"}
					reason := "stop"
					if !hasResult {
						delta = map[string]any{"role": "assistant", "reasoning_content": "check evidence", "tool_calls": []any{map[string]any{"index": 0, "id": "lookup-1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": "{}"}}}}
						reason = "tool_calls"
					}
					if mode == "truncated" {
						reason = "length"
					}
					frames := []any{map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}}}
					if mode != "incomplete" {
						frames = append(frames, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": reason}}})
					}
					frames = append(frames, map[string]any{"choices": []any{}, "usage": map[string]int{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}})
					w.Header().Set("Content-Type", "text/event-stream")
					for _, frame := range frames {
						data, _ := json.Marshal(frame)
						fmt.Fprintf(w, "data: %s\n\n", data)
					}
					fmt.Fprint(w, "data: [DONE]\n\n")
				}))
				defer server.Close()
				client := contractProvider(t, name, server.URL)
				require.True(t, llm.HasStreaming(client))
				require.True(t, llm.HasToolStreaming(client))
				require.True(t, llm.HasToolCalling(client))
				require.NotNil(t, llm.AsToolStreamClient(client))
				checker := client.(llm.CapabilityChecker)
				require.True(t, checker.HasCapability(llm.CapabilityStructuredStreaming))
				require.True(t, checker.HasCapability(llm.CapabilityToolStreaming))
				tool := &contractTool{}
				b := builder.NewSimpleBuilder(client).WithTools(tool).WithMaxIterations(2)
				if mode == "direct" {
					b.WithReturnDirectTools("lookup").WithMaxIterations(1)
				}
				agent, err := b.Build()
				require.NoError(t, err)
				out, err := agent.ExecuteWithTools(context.Background(), "question")
				if mode == "incomplete" || mode == "truncated" {
					require.Error(t, err)
					require.Zero(t, tool.calls.Load())
					require.EqualValues(t, 1, calls.Load())
					return
				}
				require.NoError(t, err)
				require.Equal(t, "42", out.Result)
				expected := 2
				if mode == "direct" {
					expected = 1
				}
				require.EqualValues(t, expected, calls.Load())
				require.Equal(t, expected, out.Metadata["model_calls"])
				require.Equal(t, true, out.Metadata["usage_complete"])
				require.Equal(t, 5*expected, out.TokenUsage.TotalTokens)
				require.EqualValues(t, 1, tool.calls.Load())
			})
		}
	}
}

func TestNativeProvidersCancelStructuredStream(t *testing.T) {
	for _, name := range []string{"openai", "deepseek"} {
		t.Run(name, func(t *testing.T) {
			closed := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(closed)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			stream, err := contractProvider(t, name, server.URL).ChatStream(ctx, []llm.Message{llm.UserMessage("question")})
			require.NoError(t, err)
			select {
			case chunk := <-stream:
				require.Equal(t, "first", chunk.Delta)
			case <-ctx.Done():
				t.Fatal("no first chunk")
			}
			cancel()
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("upstream did not stop")
			}
			for range stream {
			}
		})
	}
}
