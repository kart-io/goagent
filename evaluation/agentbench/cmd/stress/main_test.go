package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	bench "github.com/kart-io/goagent/evaluation/agentbench"
	"github.com/stretchr/testify/require"
)

func TestPreviewKeepsConfiguredBudgetWithoutModelRequests(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "preview must not call model", 500)
	}))
	defer server.Close()
	t.Setenv("DEEPSEEK_API_KEY", "preview-secret")
	t.Setenv("DEEPSEEK_BASE_URL", server.URL)
	out := filepath.Join(t.TempDir(), "preview")
	require.NoError(t, run([]string{"-dataset", "../../testdata/parameter-cases.json", "-provider", "deepseek", "-model", "demo", "-mode", "all", "-repeats", "3", "-max-calls", "4", "-max-output-tokens", "2048", "-token-stop", "9000", "-out", out}))
	data, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	require.NoError(t, err)
	var plan struct {
		Runs     int          `json:"planned_runs"`
		Requests int          `json:"maximum_upstream_requests"`
		Config   bench.Config `json:"config"`
	}
	require.NoError(t, json.Unmarshal(data, &plan))
	require.Equal(t, 6, plan.Runs)
	require.Equal(t, 24, plan.Requests)
	require.Equal(t, "deepseek", plan.Config.Provider)
	require.Equal(t, 4, plan.Config.Budget.MaxCalls)
	require.Equal(t, 2048, plan.Config.Budget.MaxOutputTokens)
	require.Equal(t, 9000, plan.Config.Budget.ObservedTokens)
	require.NotContains(t, string(data), "preview-secret")
	require.NotContains(t, string(data), server.URL)
	require.NoFileExists(t, filepath.Join(out, "results.jsonl"))
	require.Zero(t, requests.Load())
}

func TestInvalidBudgetOrProviderDoesNotCreateOutputs(t *testing.T) {
	for _, args := range [][]string{{"-max-calls", "0"}, {"-max-output-tokens", "0"}, {"-token-stop", "-1"}, {"-provider", "unknown"}} {
		out := filepath.Join(t.TempDir(), "invalid")
		require.Error(t, run(append(args, "-out", out)))
		require.NoDirExists(t, out)
	}
}

func TestGeminiDoesNotFallbackToOtherProviderCredentials(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "wrong-provider-key")
	t.Setenv("DEEPSEEK_API_KEY", "wrong-provider-key")
	out := filepath.Join(t.TempDir(), "results")
	require.ErrorContains(t, run([]string{"-live", "-provider", "gemini-openai", "-model", "gemini-2.5-flash", "-out", out}), "API Key")
	require.NoDirExists(t, out)
}

func TestProviderCLIReportsParameterFailures(t *testing.T) {
	for _, provider := range []struct{ name, env string }{{"deepseek", "DEEPSEEK"}, {"gemini-openai", "GEMINI"}} {
		t.Run(provider.name, func(t *testing.T) {
			for _, correct := range []bool{true, false} {
				t.Run(fmt.Sprint(correct), func(t *testing.T) {
					var requests atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests.Add(1)
						var body struct {
							Model     string `json:"model"`
							MaxTokens int    `json:"max_tokens"`
						}
						if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "native-model" || body.MaxTokens != 1024 || r.Header.Get("Authorization") != "Bearer provider-test" {
							http.Error(w, "wrong provider configuration", 400)
							return
						}
						args := `{"order_id":"DEMO-1001"}`
						if !correct {
							args = `{"order_id":"wrong"}`
						}
						delta := map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "lookup-1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": args}}}}
						w.Header().Set("Content-Type", "text/event-stream")
						for _, frame := range []any{
							map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}},
							map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}}},
							map[string]any{"choices": []any{}, "usage": map[string]int{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}},
						} {
							data, _ := json.Marshal(frame)
							fmt.Fprintf(w, "data: %s\n\n", data)
						}
						fmt.Fprint(w, "data: [DONE]\n\n")
					}))
					defer server.Close()
					t.Setenv(provider.env+"_API_KEY", "provider-test")
					t.Setenv(provider.env+"_BASE_URL", server.URL)
					t.Setenv(provider.env+"_MODEL", "native-model")
					t.Setenv("OPENAI_API_KEY", "must-not-leak")
					t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:1")
					t.Setenv("OPENAI_MODEL", "wrong-provider-model")
					out := filepath.Join(t.TempDir(), "results")
					err := run([]string{"-live", "-provider", provider.name, "-dataset", "../../testdata/parameter-cases.json", "-mode", "contract", "-repeats", "2", "-workers", "1", "-max-calls", "1", "-max-output-tokens", "1024", "-out", out})
					if correct {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
					require.EqualValues(t, 2, requests.Load())
					data, err := os.ReadFile(filepath.Join(out, "summary.json"))
					require.NoError(t, err)
					var summary struct {
						Completed int                  `json:"completed"`
						Failures  int                  `json:"contract_failures"`
						Groups    []bench.SuiteSummary `json:"groups"`
					}
					require.NoError(t, json.Unmarshal(data, &summary))
					require.Equal(t, 2, summary.Completed)
					require.Len(t, summary.Groups, 1)
					require.Equal(t, 2, summary.Groups[0].Passed, "answer correctness must be separate from parameter correctness")
					require.Equal(t, 2, summary.Groups[0].ToolArgumentsEvaluated)
					if correct {
						require.Zero(t, summary.Failures)
						require.Equal(t, 2, summary.Groups[0].ToolArgumentsPassed)
					} else {
						require.Equal(t, 2, summary.Failures)
						require.Zero(t, summary.Groups[0].ToolArgumentsPassed)
					}
				})
			}

		})
	}
}
