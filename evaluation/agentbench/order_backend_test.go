package agentbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const orderSchema = `{"type":"object","properties":{"order_id":{"type":"string"}},"required":["order_id"],"additionalProperties":false}`

func orderCase() Case {
	return Case{ID: "tenant-order", Question: "查询订单 shared，原样返回工具文本。", Answer: "A_RESULT", Lookup: &LookupCase{
		Parameters: json.RawMessage(orderSchema), ExpectedArguments: json.RawMessage(`{"order_id":"shared"}`),
		Orders: &OrderLookupBackend{TenantID: "tenant-A", Records: []OrderLookupRecord{
			{TenantID: "tenant-A", OrderID: "shared", Text: "A_RESULT"},
			{TenantID: "tenant-A", OrderID: "other", Text: "OTHER_RESULT"},
			{TenantID: "tenant-B", OrderID: "shared", Text: "FOREIGN_SECRET"},
			{TenantID: "tenant-B", OrderID: "B-only", Text: "FOREIGN_SECRET"},
		}},
	}}
}

func TestTenantLookupUsesActualArgumentsAndNeverScoringLabels(t *testing.T) {
	for _, tc := range []struct {
		args, want string
		invalid    bool
	}{
		{`{"order_id":"shared"}`, "A_RESULT", false},
		{`{"order_id":"other"}`, "OTHER_RESULT", false},
		{`{"order_id":"B-only"}`, "NOT_FOUND", false},
		{`{"order_id":"missing"}`, "NOT_FOUND", false},
		{`{"order_id":"shared","tenant_id":"tenant-B"}`, "", true},
		{`{"order_id":1001}`, "", true}, {`{}`, "", true},
	} {
		t.Run(tc.args, func(t *testing.T) {
			c := orderCase()
			tool := &lookup{parameters: c.Lookup.Parameters, orders: c.Lookup.Orders}
			var args map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.args), &args))
			out, err := tool.Invoke(context.Background(), &interfaces.ToolInput{Args: args})
			if tc.invalid {
				require.Error(t, err)
				require.Nil(t, out)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, out.Result)
			require.NotContains(t, fmt.Sprint(out.Result), "FOREIGN_SECRET")
		})
	}
	// Equal order IDs resolve to different records under a different host context.
	c := orderCase()
	c.Lookup.Orders.TenantID = "tenant-B"
	text, missed, err := c.Lookup.Orders.query(context.Background(), map[string]any{"order_id": "shared"})
	require.NoError(t, err)
	require.False(t, missed)
	require.Equal(t, "FOREIGN_SECRET", text)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = c.Lookup.Orders.query(ctx, map[string]any{"order_id": "shared"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestTenantLookupNativePipeline(t *testing.T) {
	for _, provider := range []string{"openai", "deepseek"} {
		for _, id := range []string{"shared", "other", "B-only", "missing"} {
			t.Run(provider+"/"+id, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if !assert.NoError(t, err) {
						http.Error(w, "read", 400)
						return
					}
					for _, label := range []string{"expected_arguments", "A_RESULT", "OTHER_RESULT", "FOREIGN_SECRET", "tenant-A"} {
						assert.NotContains(t, string(body), label)
					}
					arguments, _ := json.Marshal(map[string]string{"order_id": id})
					frame := map[string]any{"choices": []any{map[string]any{"index": 0, "finish_reason": "tool_calls", "delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "query-1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": string(arguments)}}}}}}}
					data, _ := json.Marshal(frame)
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n", data)
				}))
				defer server.Close()
				c := orderCase()
				cfg := testConfig(server.URL)
				cfg.Provider = provider
				cfg.DirectTools = true
				result := Run(context.Background(), c, cfg)
				require.Empty(t, result.Error)
				require.Equal(t, "tenant_orders", result.LookupBackend)
				require.NotContains(t, result.Answer, "FOREIGN_SECRET")
				require.Equal(t, id == "shared", result.Success)
				out := ScoreSuiteJob(SuiteJob{Case: SuiteCase{Case: c, Category: "orders", DirectEligible: true}, Direct: true}, result)
				require.Equal(t, id == "shared", out.ContractPassed)
				require.Equal(t, "tenant_orders", SummarizeSuite([]SuiteOutcome{out})[0].LookupBackend)
				if id == "B-only" || id == "missing" {
					require.Equal(t, "NOT_FOUND", result.Answer)
					require.EqualValues(t, 1, result.LookupMisses)
				}
			})
		}
	}
}

func TestTenantLookupDatasetValidation(t *testing.T) {
	for _, mutate := range []func(*Case){
		func(c *Case) { c.Evidence = "mixed" },
		func(c *Case) { c.Lookup.Orders.TenantID = "" },
		func(c *Case) { c.Lookup.Orders.Records = append(c.Lookup.Orders.Records, c.Lookup.Orders.Records[0]) },
		func(c *Case) {
			c.Lookup.Parameters = json.RawMessage(`{"type":"object","properties":{"order_id":{"type":"string"}}}`)
		},
		func(c *Case) { c.Lookup.ExpectedArguments = json.RawMessage(`{"tenant_id":"tenant-A"}`) },
	} {
		c := orderCase()
		mutate(&c)
		result := Run(context.Background(), c, testConfig("http://127.0.0.1:1"))
		require.NotEmpty(t, result.Error)
		require.Zero(t, result.Measurements.Calls)
	}
	data, err := os.ReadFile("testdata/tenant-order-cases.json")
	require.NoError(t, err)
	cases, err := DecodeSuite(data)
	require.NoError(t, err)
	require.Len(t, cases, 6)
}
