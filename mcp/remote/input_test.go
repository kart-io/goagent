package remote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/interfaces"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func inputServer(t *testing.T, old bool) (context.Context, string, *atomic.Int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	var opts mcp.ServerOptions
	if old {
		opts.SupportedProtocolVersions = []string{"2025-11-25"}
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "人工输入回归", Version: "1"}, &opts)
	calls := &atomic.Int32{}
	srv.AddTool(&mcp.Tool{Name: "review", InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		// 每次续接必须携带最初的业务参数，不能把输入回应替换成新调用。
		if string(req.Params.Arguments) != `{"id":9007199254740993}` {
			return nil, errors.New("业务参数丢失或改变")
		}
		state := req.Params.RequestState
		if state != "" {
			r, ok := req.Params.InputResponses["answer"].(*mcp.ElicitResult)
			if !ok || r.Action != "accept" {
				return nil, errors.New("输入回应丢失")
			}
		}
		if state == "second" || (old && state == "first") {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "完成"}}}, nil
		}
		next := "first"
		if state == "first" {
			next = "second"
		}
		return &mcp.CallToolResult{RequestState: next, InputRequests: mcp.InputRequestMap{"answer": &mcp.ElicitParams{Mode: "form", Message: "请确认", RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{}}}}}, nil
	})
	h := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: !old, JSONResponse: true}))
	t.Cleanup(h.Close)
	return ctx, h.URL, calls
}

func inputAnswer() mcp.InputResponseMap {
	return mcp.InputResponseMap{"answer": &mcp.ElicitResult{Action: "accept", Content: map[string]any{}}}
}
func inputArgs() *interfaces.ToolInput {
	return &interfaces.ToolInput{Args: map[string]any{"id": json.Number("9007199254740993")}}
}

func TestInputCheckpointCanResumeAcrossConnections(t *testing.T) {
	ctx, endpoint, calls := inputServer(t, false)
	s, err := Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint})
	require.NoError(t, err)
	adapted, err := Tools(ctx, s)
	require.NoError(t, err)
	_, err = adapted[0].Invoke(ctx, inputArgs())
	var pending *InputRequiredError
	require.ErrorAs(t, err, &pending)
	require.Equal(t, "first", pending.RequestState)
	raw, err := json.Marshal(pending)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	var restored InputRequiredError
	require.NoError(t, json.Unmarshal(raw, &restored))
	s, err = Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint})
	require.NoError(t, err)
	defer s.Close()
	_, err = restored.Continue(ctx, s, mcp.InputResponseMap{"wrong": &mcp.ElicitResult{Action: "accept"}})
	require.Error(t, err)
	require.Equal(t, int32(1), calls.Load())
	_, err = restored.Continue(ctx, s, inputAnswer())
	require.ErrorAs(t, err, &pending)
	require.Equal(t, "second", pending.RequestState)
	result, err := pending.Continue(ctx, s, inputAnswer())
	require.NoError(t, err)
	require.Equal(t, "完成", result.Content[0].(*mcp.TextContent).Text)
	require.Equal(t, int32(3), calls.Load())
}

func TestInputHandlerBoundedAndPreservesCheckpointOnError(t *testing.T) {
	ctx, endpoint, _ := inputServer(t, false)
	s, err := Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint})
	require.NoError(t, err)
	defer s.Close()
	for _, tc := range []struct {
		name    string
		rounds  int
		failure error
		pending string
	}{
		{"轮数上限", 1, nil, "second"},
		{"宿主错误", 2, errors.New("人工输入暂不可用"), "first"},
		{"宿主取消", 2, context.Canceled, "first"},
		{"两轮成功", 2, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapted, err := Tools(ctx, s, WithInputHandler(func(context.Context, *InputRequiredError) (mcp.InputResponseMap, error) {
				return inputAnswer(), tc.failure
			}, tc.rounds))
			require.NoError(t, err)
			out, err := adapted[0].Invoke(ctx, inputArgs())
			if tc.pending == "" {
				require.NoError(t, err)
				require.True(t, out.Success)
				return
			}
			var pending *InputRequiredError
			require.ErrorAs(t, err, &pending)
			require.Equal(t, tc.pending, pending.RequestState)
			if tc.failure != nil {
				require.ErrorIs(t, err, tc.failure)
			}
		})
	}
}

func TestLegacyElicitationUsesHostHandler(t *testing.T) {
	ctx, endpoint, _ := inputServer(t, true)
	var rounds atomic.Int32
	s, err := ConnectWithOptions(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, &mcp.ClientOptions{
		ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			rounds.Add(1)
			return &mcp.ElicitResult{Action: "accept", Content: map[string]any{}}, nil
		},
	})
	require.NoError(t, err)
	defer s.Close()
	adapted, err := Tools(ctx, s)
	require.NoError(t, err)
	out, err := adapted[0].Invoke(ctx, inputArgs())
	require.NoError(t, err)
	require.True(t, out.Success)
	require.Equal(t, int32(1), rounds.Load())
	require.Equal(t, "完成", out.Result.(*mcp.CallToolResult).Content[0].(*mcp.TextContent).Text)
}
