package agentbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/evaluation"
	"github.com/stretchr/testify/require"
)

func TestRunWithExecutorUsesSharedMeterAndScoring(t *testing.T) {
	upstream := fakeModel(t)
	defer upstream.Close()
	c := Case{ID: "external", Question: "6*7", Answer: "42"}
	result := RunWithExecutor(context.Background(), c, testConfig(upstream.URL), "external", func(ctx context.Context, base string, cfg Config, c Case, onText, onFinal func(string)) (int32, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", strings.NewReader(`{"messages":[],"stream":false}`))
		if err != nil {
			return 0, err
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err
		}
		defer response.Body.Close()
		_, err = io.Copy(io.Discard, response.Body)
		onText("temporary")
		onFinal("42")
		return 0, err
	})
	require.True(t, result.Success, "%+v", result)
	require.Equal(t, "external", result.Framework)
	require.Equal(t, "42", result.Answer)
	require.Equal(t, 1, result.Measurements.Calls)
	require.Equal(t, 5, result.Measurements.Usage.TotalTokens)
	require.True(t, result.UsageComplete)
	failed := RunWithExecutor(context.Background(), c, testConfig(upstream.URL), "external", func(context.Context, string, Config, Case, func(string), func(string)) (int32, error) {
		return 0, fmt.Errorf("failed executor")
	})
	require.False(t, failed.Success)
	require.Contains(t, failed.Error, "failed executor")

	missingFinal := RunWithExecutor(context.Background(), c, testConfig(upstream.URL), "external", func(_ context.Context, _ string, _ Config, _ Case, onText, _ func(string)) (int32, error) {
		onText("42")
		return 0, nil
	})
	require.False(t, missingFinal.Success, "transient text cannot be scored as final output")
	require.NotEmpty(t, missingFinal.Error)
}

func TestRunWithExecutorCannotSucceedAfterDeadline(t *testing.T) {
	cfg := testConfig("http://unused.invalid")
	cfg.Timeout = time.Millisecond
	result := RunWithExecutor(context.Background(), Case{ID: "late", Question: "q", Answer: "42"}, cfg, "external", func(ctx context.Context, _ string, _ Config, _ Case, _, onFinal func(string)) (int32, error) {
		<-ctx.Done()
		onFinal("42")
		return 0, nil
	})
	require.False(t, result.Success)
	require.Contains(t, result.Error, "deadline exceeded")
}

// fakeModel 只验证真实 SDK、工具和事件链路，不产生模型质量或性能结论。
func fakeModel(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var body struct {
			Stream    bool                             `json:"stream"`
			Functions []any                            `json:"functions"`
			Tools     []any                            `json:"tools"`
			Messages  []struct{ Role, Content string } `json:"messages"`
			Model     string                           `json:"model"`
			MaxTokens int                              `json:"max_tokens"`
		}
		require.NoError(t, json.Unmarshal(data, &body))
		require.Equal(t, "test-model", body.Model)
		require.Equal(t, 128, body.MaxTokens)
		hasResult := false
		unknown := false
		for _, m := range body.Messages {
			hasResult = hasResult || m.Role == "tool" || strings.Contains(m.Content, "Untrusted tool result")
			unknown = unknown || strings.Contains(m.Content, "手续费")
		}
		call := (len(body.Functions) > 0 || len(body.Tools) > 0) && !hasResult
		answer := "42"
		if hasResult {
			answer = "30"
		}
		if unknown {
			answer = "UNKNOWN"
		}
		if !body.Stream {
			message := map[string]any{"role": "assistant", "content": "READY"}
			if call {
				message["function_call"] = map[string]any{"name": "lookup", "arguments": "{}"}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "response", "model": "test-model", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(v any) {
			data, err := json.Marshal(v)
			require.NoError(t, err)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			w.(http.Flusher).Flush()
		}
		delta := map[string]any{"role": "assistant", "content": answer}
		reason := "stop"
		if call {
			delta = map[string]any{"role": "assistant", "content": "工具调用前的说明", "tool_calls": []any{map[string]any{"index": 0, "id": "call-1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": "{}"}}}}
			reason = "tool_calls"
		}
		send(map[string]any{"id": "response", "model": "test-model", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
		send(map[string]any{"id": "response", "model": "test-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": reason}}})
		send(map[string]any{"id": "response", "model": "test-model", "choices": []any{}, "usage": map[string]int{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}})
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func testConfig(endpoint string) Config {
	return Config{APIKey: "test-key", BaseURL: endpoint, Timeout: time.Second * 5, Budget: evaluation.Budget{Model: "test-model", MaxCalls: 4, MaxOutputTokens: 128, ObservedTokens: 100, Temperature: 0.2}}
}

func TestReturnDirectlyWithOneRequest(t *testing.T) {
	upstream := fakeModel(t)
	defer upstream.Close()
	t.Run("直接返回", func(t *testing.T) {
		cfg := testConfig(upstream.URL)
		cfg.DirectTools = true
		cfg.Budget.MaxCalls = 1
		result := Run(context.Background(), Case{ID: "status", Question: "状态", Evidence: "已发货", Answer: "已发货"}, cfg)
		require.Empty(t, result.Error)
		require.True(t, result.Success, "%+v", result)
		require.Equal(t, 1, result.Measurements.Calls)
		require.EqualValues(t, 1, result.ToolCalls)
		require.True(t, result.UsageComplete)
	})
}

func TestActualSDKAndTools(t *testing.T) {
	upstream := fakeModel(t)
	defer upstream.Close()
	for _, c := range []Case{{ID: "math", Question: "17+25", Answer: "42"}, {ID: "refund", Question: "退款天数", Answer: "30", Evidence: "30 天"}, {ID: "unknown", Question: "手续费", Answer: "UNKNOWN", Evidence: "只有配送资料"}} {
		t.Run(c.ID, func(t *testing.T) {
			result := Run(context.Background(), c, testConfig(upstream.URL))
			require.Empty(t, result.Error)
			require.True(t, result.Success, "%+v", result)
			require.NotNil(t, result.FirstTextMS)
			require.True(t, result.UsageComplete, "%+v", result)
			expectedCalls := 1
			if c.Evidence != "" {
				expectedCalls = 2
			}
			require.Equal(t, expectedCalls, result.Measurements.Calls)
			require.Equal(t, result.Measurements.Calls*5, result.Measurements.Usage.TotalTokens)
			if c.Evidence != "" {
				require.EqualValues(t, 1, result.ToolCalls)
			}
		})
	}
}

func TestRunPreservesFailuresAndBudget(t *testing.T) {
	upstream := fakeModel(t)
	defer upstream.Close()
	cfg := testConfig(upstream.URL)
	cfg.Budget.MaxCalls = 1
	result := Run(context.Background(), Case{ID: "budget", Question: "退款天数", Answer: "30", Evidence: "30 天"}, cfg)
	require.False(t, result.Success)
	require.NotEmpty(t, result.Error)
	require.Equal(t, 1, result.Measurements.Calls)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = Run(ctx, Case{ID: "cancel", Question: "计算", Answer: "42"}, cfg)
	require.False(t, result.Success)
	require.NotEmpty(t, result.Error)
	require.Zero(t, result.Measurements.Calls)
}

func TestMeterNeverInventsUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[]}`)
	}))
	defer upstream.Close()
	meter := &Meter{Endpoint: upstream.URL, Budget: testConfig(upstream.URL).Budget}
	proxy := httptest.NewServer(meter)
	defer proxy.Close()
	response, err := http.Post(proxy.URL+"/chat/completions", "application/json", strings.NewReader(`{"model":"ignored"}`))
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	require.Equal(t, 1, meter.Measurements().Calls)
	require.Zero(t, meter.Measurements().UsageReports)
}

func TestTruncatedStreamCannotPassWithMatchingText(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"42\"},\"finish_reason\":null}]}\n\n")
	}))
	defer upstream.Close()
	result := Run(context.Background(), Case{ID: "truncated", Question: "计算", Answer: "42"}, testConfig(upstream.URL))
	require.False(t, result.Success)
	require.NotEmpty(t, result.Error)
	require.False(t, result.UsageComplete)
}
