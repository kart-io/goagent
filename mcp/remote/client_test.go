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

	"github.com/kart-io/goagent/interfaces"
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
	cmd := exec.Command(os.Args[0], "-test.run=^TestOfficialSDKStdioInteroperability$")
	cmd.Env = append(os.Environ(), "GOAGENT_MCP_CHILD=1")
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
