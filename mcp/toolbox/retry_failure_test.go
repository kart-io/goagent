package toolbox

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/mcp/core"
)

// 工具每次都以 Success=false 返回而不报错时，ExecuteWithRetry 应返回带原因的错误，而不是值为 nil 的 *AgentError。
func TestExecuteWithRetryFailureWithoutError(t *testing.T) {
	tool := NewMockTool("fail", "test")
	tool.executeFunc = func(context.Context, map[string]interface{}) (*core.ToolResult, error) {
		return &core.ToolResult{Success: false, Error: "远端拒绝"}, nil
	}
	result, err := NewStandardExecutor().ExecuteWithRetry(context.Background(), tool,
		&core.ToolCall{ToolName: "fail", Input: map[string]interface{}{"input": "x"}}, 0)
	require.Error(t, err)
	var agentErr *agentErrors.AgentError
	require.ErrorAs(t, err, &agentErr)
	require.NotNil(t, agentErr, "错误不能是值为 nil 的 *AgentError")
	require.Contains(t, err.Error(), "远端拒绝")
	require.False(t, result.Success)
}

func TestExecuteWithRetryNegativeRetries(t *testing.T) {
	result, err := NewStandardExecutor().ExecuteWithRetry(context.Background(), NewMockTool("t", "test"),
		&core.ToolCall{ToolName: "t", Input: map[string]interface{}{"input": "x"}}, -1)
	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "maxRetries=-1")
}
