package toolbox

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/mcp/core"
	"github.com/kart-io/goagent/testing/panictest"
)

func newPanicMCPTool(name string) *MockTool {
	tool := NewMockTool(name, "test")
	tool.executeFunc = func(context.Context, map[string]interface{}) (*core.ToolResult, error) { panic("mcp boom") }
	return tool
}

func requireMCPPanic(t *testing.T, err error) {
	t.Helper()
	var p *agentErrors.PanicError
	require.True(t, errors.As(err, &p), "应返回 PanicError：%v", err)
	require.Equal(t, "mcp boom", p.Value)
}

// ExecuteBatch 与 ExecuteWithTimeout 在 goroutine 中执行工具：panic 转为该调用的 PanicError，进程不得退出，其他调用照常完成。
func TestToolBoxGoroutinePanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		tb := NewStandardToolBox()
		require.NoError(t, tb.Register(NewMockTool("ok", "test")))
		require.NoError(t, tb.Register(newPanicMCPTool("boom")))
		results, err := tb.ExecuteBatch(context.Background(), []*core.ToolCall{
			{ToolName: "ok", Input: map[string]interface{}{"input": "x"}},
			{ToolName: "boom", Input: map[string]interface{}{"input": "x"}},
		})
		requireMCPPanic(t, err)
		require.NotNil(t, results[0])

		_, err = NewStandardExecutor().ExecuteWithTimeout(context.Background(), newPanicMCPTool("boom"), &core.ToolCall{ToolName: "boom", Input: map[string]interface{}{"input": "x"}})
		requireMCPPanic(t, err)
	})
}
