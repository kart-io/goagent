package protocol_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/kart-io/goagent/management/mcpserver"
	mcpremote "github.com/kart-io/goagent/mcp/remote"
)

func TestMCPToolArgumentsFollowInputCap(t *testing.T) {
	big := strings.Repeat("x", 2<<20)
	for _, tc := range []struct {
		name     string
		limit    int64
		input    string
		rejected bool
	}{
		{"显式上限拒绝超长参数", 64, strings.Repeat("x", 128), true},
		{"0 仍按默认 1MiB 拒绝", 0, big, true},
		{"不限时接受超过 1MiB 的参数", mg.UnlimitedRequestBytes, big, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testContext(t)
			f := managementtest.NewFixture(t)
			se := f.ReadySession(t)
			srv, err := mcpserver.New(f.Service, mcpserver.Config{Agents: []mcpserver.Agent{{ID: se.AgentID, Name: "assistant"}}, PollInterval: time.Millisecond, MaxInputBytes: tc.limit, ActorFromRequest: func(context.Context, *mcp.CallToolRequest) (mg.Actor, error) { return f.Actor, nil }})
			require.NoError(t, err)
			h := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{JSONResponse: true, MaxRequestBodyBytes: -1}))
			defer h.Close()
			client, err := mcpremote.ConnectWithOptions(ctx, &mcp.StreamableClientTransport{Endpoint: h.URL}, nil)
			require.NoError(t, err)
			defer client.Close()
			r, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "assistant", Arguments: map[string]any{"message_id": "m", "session_id": se.ID, "input": tc.input}})
			require.Equal(t, tc.rejected, err != nil || r.IsError, "err=%v", err)
		})
	}
}
