package remote

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestOfficialSDKHTTPInteroperability(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	type input struct {
		Text string `json:"text"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "echo text"}, func(ctx context.Context, req *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
		if in.Text == "fail" {
			return nil, nil, fmt.Errorf("deliberate failure")
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: in.Text}}}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL})
	require.NoError(t, err)
	defer session.Close()
	tools, err := Tools(ctx, session)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	require.Contains(t, tools[0].ArgsSchema(), "text")
	out, err := tools[0].Invoke(ctx, &interfaces.ToolInput{Args: map[string]interface{}{"text": "hello"}})
	require.NoError(t, err)
	require.True(t, out.Success)
	result := out.Result.(*mcp.CallToolResult)
	require.Equal(t, "hello", result.Content[0].(*mcp.TextContent).Text)
	_, err = tools[0].Invoke(ctx, &interfaces.ToolInput{Args: map[string]interface{}{"text": "fail"}})
	require.Error(t, err)
	canceled, stop := context.WithCancel(ctx)
	stop()
	_, err = tools[0].Invoke(canceled, &interfaces.ToolInput{Args: map[string]interface{}{"text": "hello"}})
	require.Error(t, err)
}

func TestOfficialSDKStdioInteroperability(t *testing.T) {
	if os.Getenv("GOAGENT_MCP_CHILD") == "1" {
		server := mcp.NewServer(&mcp.Implementation{Name: "stdio-test", Version: "1"}, nil)
		mcp.AddTool(server, &mcp.Tool{Name: "ping"}, func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "pong"}}}, nil, nil
		})
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			os.Exit(1)
		}
		os.Exit(0) // Keep test harness output off the protocol's stdout.
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// ctx 超时后杀掉子进程；WaitDelay 保证子进程残留的管道不会让 Wait 一直阻塞
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOfficialSDKStdioInteroperability$")
	cmd.Env = append(os.Environ(), "GOAGENT_MCP_CHILD=1")
	cmd.WaitDelay = time.Second
	session, err := Connect(ctx, &mcp.CommandTransport{Command: cmd})
	require.NoError(t, err)
	defer session.Close()
	tools, err := Tools(ctx, session)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	result, err := tools[0].Invoke(ctx, &interfaces.ToolInput{Args: map[string]interface{}{}})
	require.NoError(t, err)
	require.Equal(t, "pong", result.Result.(*mcp.CallToolResult).Content[0].(*mcp.TextContent).Text)
}

func TestToolOptions(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	for _, name := range []string{"echo", "hidden"} {
		mcp.AddTool(server, &mcp.Tool{Name: name}, func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, any, error) {
			return nil, nil, fmt.Errorf("deliberate failure")
		})
	}
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true}))
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL})
	require.NoError(t, err)
	defer session.Close()
	adapted, err := Tools(ctx, session,
		WithToolName(func(name string) (string, bool) { return "mcp__srv__" + name, name != "hidden" }),
		WithToolErrorsAsRejected(),
		WithDurablePolicy(durable.OperationPolicy{MaxAttempts: 1}))
	require.NoError(t, err)
	require.Len(t, adapted, 1)
	require.Equal(t, "mcp__srv__echo", adapted[0].Name())
	require.Equal(t, 1, adapted[0].(tools.DurablePolicyProvider).DurableOperationPolicy().MaxAttempts)
	out, err := adapted[0].Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{}})
	require.NoError(t, err)
	msg, rejected := tools.RejectedResult(out)
	require.True(t, rejected)
	require.Contains(t, msg, "deliberate failure")
}
