package remote

import (
	"context"
	"testing"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/require"
)

func TestMCP边界错误分类(t *testing.T) {
	_, err := Tools(context.Background(), nil)
	require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
	var detail *agentErrors.AgentError
	require.ErrorAs(t, err, &detail)
	require.Equal(t, "MCP session is required", detail.Message)
	require.Equal(t, "mcp_remote", detail.Component)
	require.Equal(t, "list_tools", detail.Operation)

	_, err = (&remoteTool{name: "lookup"}).Invoke(context.Background(), nil)
	require.Equal(t, agentErrors.CodeToolValidation, agentErrors.GetCode(err))
	require.ErrorAs(t, err, &detail)
	require.Equal(t, "MCP tool input is required", detail.Message)
	require.Equal(t, "lookup", detail.Context["tool_name"])
}
