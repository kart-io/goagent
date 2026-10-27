package agentbench_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/evaluation"
	bench "github.com/kart-io/goagent/evaluation/agentbench"
	"github.com/stretchr/testify/require"
)

// Run the refund fixture through the real SDK and tool loop with a local model.
// The model reads request messages, never answer labels.
func TestRefundScenario(t *testing.T) {
	data, err := os.ReadFile("testdata/refund-cases.json")
	require.NoError(t, err)
	cases, err := bench.DecodeSuite(data)
	require.NoError(t, err)
	for _, tc := range []struct {
		name       string
		repeats    int
		incomplete bool
	}{
		{name: "complete", repeats: 3},
		{name: "missing_finish_reason", repeats: 1, incomplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				serveRefundModel(w, r, tc.incomplete)
			}))
			defer upstream.Close()
			cfg := bench.Config{APIKey: "local-demo-only", BaseURL: upstream.URL, Timeout: 5 * time.Second,
				Budget: evaluation.Budget{Model: "refund-demo-model", MaxCalls: 2, MaxOutputTokens: 512}}
			var outcomes []bench.SuiteOutcome
			calls := 0
			for _, job := range bench.SuiteJobs(cases, tc.repeats) {
				cfg.DirectTools = job.Direct
				result := bench.Run(context.Background(), job.Case.Case, cfg)
				outcome := bench.ScoreSuiteJob(job, result)
				require.True(t, result.UsageComplete)
				if tc.incomplete {
					require.False(t, outcome.ContractPassed)
					require.False(t, result.Success)
					require.Contains(t, result.Error, "unexpected EOF")
					require.Zero(t, result.ToolCalls)
					require.Equal(t, 1, result.Measurements.Calls)
				} else {
					require.True(t, outcome.ContractPassed, "%+v", outcome)
					require.EqualValues(t, 1, result.ToolCalls)
					expectedCalls := 2
					if job.Direct {
						expectedCalls = 1
					}
					require.Equal(t, expectedCalls, result.Measurements.Calls)
					if job.Repeat == 0 {
						t.Logf("%s direct=%t: answer=%q, model_calls=%d", job.Case.ID, job.Direct, result.Answer, result.Measurements.Calls)
					}
				}
				calls += result.Measurements.Calls
				outcomes = append(outcomes, outcome)
			}
			require.Len(t, outcomes, 4*tc.repeats)
			require.EqualValues(t, calls, requests.Load())
			summaries := bench.SummarizeSuite(outcomes)
			require.Len(t, summaries, 4)
			for _, summary := range summaries {
				require.Equal(t, tc.repeats, summary.Total)
				if tc.incomplete {
					require.Zero(t, summary.Passed)
				} else {
					require.Equal(t, tc.repeats, summary.ContractPassed)
				}
			}
		})
	}
}

func serveRefundModel(w http.ResponseWriter, r *http.Request, incomplete bool) {
	var body struct {
		Messages []struct{ Role, Content string }
		Tools    []struct {
			Function struct{ Name string }
		}
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var question, evidence string
	for _, message := range body.Messages {
		if message.Role == "user" {
			question = message.Content
		}
		if message.Role == "tool" {
			evidence = message.Content
		}
	}
	delta := map[string]any{"role": "assistant"}
	reason := "stop"
	if evidence == "" {
		if len(body.Tools) != 1 || body.Tools[0].Function.Name != "lookup" {
			http.Error(w, "expected lookup tool before answering", http.StatusBadRequest)
			return
		}
		delta["tool_calls"] = []any{map[string]any{"index": 0, "id": "refund-lookup", "type": "function",
			"function": map[string]any{"name": "lookup", "arguments": "{}"}}}
		reason = "tool_calls"
	} else if strings.Contains(question, "原样返回") {
		delta["content"] = evidence
	} else {
		var fields map[string]any
		if err := json.Unmarshal([]byte(evidence), &fields); err != nil {
			http.Error(w, "expected structured refund evidence", http.StatusBadRequest)
			return
		}
		field := "refund_window_days"
		if strings.Contains(question, "international_refund_fee") {
			field = "international_refund_fee"
		}
		delta["content"] = "UNKNOWN"
		if value, ok := fields[field]; ok {
			delta["content"] = fmt.Sprint(value)
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	frames := []any{map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}}}
	if !incomplete {
		frames = append(frames, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": reason}}})
	}
	// Fixed fake usage verifies accounting only; it is not real token consumption.
	frames = append(frames, map[string]any{"choices": []any{}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}})
	for _, frame := range frames {
		data, _ := json.Marshal(frame)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}
