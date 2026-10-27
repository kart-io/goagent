package agentbench

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupArgumentScoringDoesNotAcceptCorrectAnswerWithWrongArguments(t *testing.T) {
	for _, provider := range []string{"openai", "deepseek"} {
		for _, tc := range []struct {
			name, arguments string
			passed          bool
		}{
			{"correct", `{"order_id":"DEMO-1001"}`, true},
			{"wrong", `{"order_id":"DEMO-9999"}`, false},
			{"missing", `{}`, false},
			{"extra", `{"order_id":"DEMO-1001","admin":true}`, false},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body struct {
						Messages []struct{ Content string }
						Tools    []struct {
							Function struct{ Parameters json.RawMessage }
						}
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						http.Error(w, "decode", 400)
						return
					}
					if len(body.Tools) != 1 {
						t.Error("missing lookup schema")
						http.Error(w, "schema", 400)
						return
					}
					assert.Contains(t, string(body.Tools[0].Function.Parameters), "order_id")
					for _, m := range body.Messages {
						assert.NotContains(t, m.Content, "expected_arguments")
						assert.NotContains(t, m.Content, "42", "answer/evidence must not be supplied before lookup")
						assert.NotContains(t, m.Content, "该无参数工具")
					}
					delta := map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "call-1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": tc.arguments}}}}
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
				c := Case{ID: "order", Question: "查询订单 DEMO-1001，原样返回工具文本", Answer: "42", Evidence: "42", Lookup: &LookupCase{
					Parameters:        json.RawMessage(`{"type":"object","properties":{"order_id":{"type":"string"}},"required":["order_id"],"additionalProperties":false}`),
					ExpectedArguments: json.RawMessage(`{"order_id":"DEMO-1001"}`),
				}}
				cfg := testConfig(server.URL)
				cfg.Provider = provider
				cfg.DirectTools = true
				result := Run(context.Background(), c, cfg)
				require.True(t, result.Success, "answer score remains separate: %+v", result)
				require.NotNil(t, result.ToolArgumentsPassed)
				require.Equal(t, tc.passed, *result.ToolArgumentsPassed)
				require.Len(t, result.ActualToolArguments, 1)
				require.JSONEq(t, tc.arguments, string(result.ActualToolArguments[0]))
				job := SuiteJob{Case: SuiteCase{Case: c, Category: "orders", DirectEligible: true}, Direct: true}
				outcome := ScoreSuiteJob(job, result)
				require.Equal(t, tc.passed, outcome.ContractPassed)
				summary := SummarizeSuite([]SuiteOutcome{outcome})
				require.Equal(t, 1, summary[0].ToolArgumentsEvaluated)
				expected := 0
				if tc.passed {
					expected = 1
				}
				require.Equal(t, expected, summary[0].ToolArgumentsPassed)
			})
		}
	}
}

func TestLookupLabelsValidatedBeforeRequests(t *testing.T) {
	for _, label := range []LookupCase{
		{Parameters: json.RawMessage(`{"type":"object","properties":{}}`)},
		{Parameters: json.RawMessage(`{"type":"array"}`), ExpectedArguments: json.RawMessage(`{}`)},
		{Parameters: json.RawMessage(`{"type":"object","properties":{}}`), ExpectedArguments: json.RawMessage(`null`)},
	} {
		c := Case{ID: "invalid", Question: "question", Answer: "answer", Evidence: "evidence", Lookup: &label}
		require.Error(t, ValidateLookupCase(c))
		result := Run(context.Background(), c, testConfig("http://127.0.0.1:1"))
		require.False(t, result.Success)
		require.Zero(t, result.Measurements.Calls)
	}
}

func TestArgumentComparisonPreservesNumbersAndMissingKeys(t *testing.T) {
	for _, tc := range []struct {
		want, got string
		equal     bool
	}{
		{`{"n":1}`, `{"n":1.0}`, true},
		{`{"n":9007199254740993}`, `{"n":9007199254740992}`, false},
		{`{"n":1}`, `{"n":"1"}`, false},
		{`{"n":null}`, `{}`, false},
		{`{"b":[true,2],"a":"x"}`, `{"a":"x","b":[true,2]}`, true},
		{`{"a":[1,2]}`, `{"a":[2,1]}`, false},
	} {
		require.Equal(t, tc.equal, equalArguments(json.RawMessage(tc.want), json.RawMessage(tc.got)), "%s vs %s", tc.want, tc.got)
	}
}
