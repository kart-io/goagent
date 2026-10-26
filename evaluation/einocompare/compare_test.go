package einocompare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/kart-io/goagent/evaluation"
	bench "github.com/kart-io/goagent/evaluation/agentbench"
	"github.com/stretchr/testify/require"
)

// Real SDKs against one controlled upstream: this checks protocols, not quality.
func TestFrameworksShareEvidenceBudgetAndUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
			Messages  []struct{ Role, Content string }
			Tools     []any
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if body.Model != "test-model" || body.MaxTokens != 128 {
			http.Error(w, "unequal budget", 400)
			return
		}
		hasTool := false
		for _, m := range body.Messages {
			hasTool = hasTool || m.Role == "tool"
		}
		delta := map[string]any{"role": "assistant", "content": "42"}
		reason := "stop"
		if len(body.Tools) > 0 && !hasTool {
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "call-1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": "{}"}}}}
			reason = "tool_calls"
		}
		if hasTool {
			delta["content"] = "甲乙\n原文"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, frame := range []any{
			map[string]any{"id": "test", "model": "test-model", "choices": []any{map[string]any{"index": 0, "delta": delta}}},
			map[string]any{"id": "test", "model": "test-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": reason}}},
			map[string]any{"id": "test", "model": "test-model", "choices": []any{}, "usage": map[string]int{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}},
		} {
			data, _ := json.Marshal(frame)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	for _, framework := range []string{"goagent", "eino"} {
		for _, mode := range []string{"plain", "tools", "direct", "budget"} {
			t.Run(framework+"/"+mode, func(t *testing.T) {
				cfg := bench.Config{APIKey: "test", BaseURL: upstream.URL, Timeout: 5 * time.Second, Budget: evaluation.Budget{Model: "test-model", MaxCalls: 2, MaxOutputTokens: 128, Temperature: 0.2}}
				c := bench.Case{ID: mode, Question: "question", Answer: "42"}
				if mode != "plain" {
					c.Evidence = "甲乙\n原文"
					c.Answer = c.Evidence
				}
				cfg.DirectTools = mode == "direct"
				if mode == "budget" {
					cfg.Budget.MaxCalls = 1
				}
				result := Run(context.Background(), framework, c, cfg)
				if mode == "budget" {
					require.False(t, result.Success)
					require.NotEmpty(t, result.Error)
					require.Equal(t, 1, result.Measurements.Calls)
					return
				}
				require.True(t, result.Success, "%+v", result)
				require.NotNil(t, result.FirstTextMS, "direct tool text must participate in first-text timing")
				require.Equal(t, framework, result.Framework)
				calls := 1
				if mode == "tools" {
					calls = 2
				}
				require.Equal(t, calls, result.Measurements.Calls)
				require.True(t, result.UsageComplete, "%+v", result)
				require.Equal(t, calls*5, result.Measurements.Usage.TotalTokens)
				if mode != "plain" {
					require.EqualValues(t, 1, result.ToolCalls)
				}
			})
		}
	}
}

func TestEvidenceToolMatchesLookupAdditionalArguments(t *testing.T) {
	lookup := &evidenceTool{evidence: "original"}
	var executable tool.InvokableTool = lookup
	text, err := executable.InvokableRun(context.Background(), `{"query":"question"}`)
	require.NoError(t, err)
	require.Equal(t, "original", text)
	require.EqualValues(t, 1, lookup.calls.Load())
}

func TestComparePreservesUpstreamFailureAndCancellation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unavailable", 503) }))
	defer upstream.Close()
	cfg := bench.Config{APIKey: "test", BaseURL: upstream.URL, Timeout: time.Second, Budget: evaluation.Budget{Model: "test-model", MaxCalls: 1, MaxOutputTokens: 128}}
	for _, framework := range []string{"eino", "goagent"} {
		result := Run(context.Background(), framework, bench.Case{ID: "fail", Question: "q", Answer: "a"}, cfg)
		require.False(t, result.Success)
		require.NotEmpty(t, result.Error)
		require.Equal(t, 1, result.Measurements.Calls)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result = Run(ctx, framework, bench.Case{ID: "cancel", Question: "q", Answer: "a"}, cfg)
		require.False(t, result.Success)
		require.NotEmpty(t, result.Error)
	}
	result := Run(context.Background(), "unknown", bench.Case{}, cfg)
	require.True(t, strings.Contains(result.Error, "unknown"))
}
