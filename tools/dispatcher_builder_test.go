package tools_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func TestBuilderUsesNativeDispatcherContract(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				args := `{\"order\":{\"id\":\"order-1\"},\"amount\":1}`
				if invalid {
					args = `{\"order\":{\"id\":\"order-1\"},\"amount\":1,\"extra\":true}`
				}
				fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"refund\",\"arguments\":\"%s\"}}]}}]}\n\n", args)
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			client, err := providers.NewDeepSeekWithOptions(llm.WithAPIKey("offline"), llm.WithModel("offline"), llm.WithBaseURL(server.URL))
			require.NoError(t, err)
			tool := &dispatchTestTool{name: "refund", schema: strictOrderSchema, run: func(_ context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				if in.ExecutionKey != "host-effect" {
					return nil, fmt.Errorf("missing host key")
				}
				return &interfaces.ToolOutput{Success: true, Result: "receipt"}, nil
			}}
			d := newTestDispatcher(t, tool, tools.DispatchConfig{})
			agent, err := builder.NewSimpleBuilder(client).WithTools(d.Tools()...).WithReturnDirectTools("refund").WithMaxIterations(1).Build()
			require.NoError(t, err)
			out, err := agent.AsRunnable().Invoke(durable.WithExecutionKey(context.Background(), "host-effect"), "issue the reviewed refund")
			if invalid {
				require.Error(t, err)
				require.Zero(t, tool.calls.Load())
			} else {
				require.NoError(t, err)
				require.Equal(t, "receipt", out.Result)
				require.EqualValues(t, 1, tool.calls.Load())
			}
			require.EqualValues(t, 1, requests.Load())
		})
	}
}
