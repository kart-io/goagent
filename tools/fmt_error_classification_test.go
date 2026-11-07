package tools

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func TestDispatcher对象参数错误分类(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(NewBaseTool("lookup", "查询", `{"type":"object","properties":{"id":{"type":"integer"}}}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		t.Fatal("无效参数不得执行工具")
		return nil, nil
	})))
	dispatcher, err := NewDispatcher(registry, DispatchConfig{})
	require.NoError(t, err)
	_, err = dispatcher.Invoke(context.Background(), ToolRequest{Name: "lookup"})
	require.Equal(t, agentErrors.CodeToolValidation, agentErrors.GetCode(err))
	var detail *agentErrors.AgentError
	require.ErrorAs(t, err, &detail)
	require.Equal(t, `tool "lookup" requires object arguments`, detail.Message)
	require.Equal(t, "lookup", detail.Context["tool_name"])
	_, marked := detail.ModelArgumentMessage()
	require.False(t, marked, "宿主调用缺少对象参数不新增模型参数恢复标记")
	_, err = dispatcher.Invoke(context.Background(), ToolRequest{Name: "lookup", Input: &interfaces.ToolInput{Args: map[string]any{"id": json.Number("1.0000000000000001")}}})
	var argument *ArgumentError
	require.ErrorAs(t, err, &argument)
	require.Equal(t, agentErrors.CodeToolValidation, agentErrors.GetCode(argument.Err))
	require.Equal(t, `tool "lookup" arguments: validating /id: 1.0000000000000001 is not an integer`, argument.Message, "模型参数消息保留正文，不暴露分类上下文")

	recovered, err := NewDispatcher(registry, DispatchConfig{ReturnArgumentErrors: true})
	require.NoError(t, err)
	out, err := modelCall(context.Background(), recovered, "lookup", &interfaces.ToolInput{Args: map[string]any{"id": json.Number("1.0000000000000001")}})
	requireArgumentResult(t, out, err, argument.Message)

	calls := 0
	cfg := runnableConfig()
	cfg.ArgsSchema = `{"type":"object","properties":{"n":{"type":"integer"}}}`
	tool, err := NewRunnableTool(numberRun(&calls, false), cfg)
	require.NoError(t, err)
	_, err = tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"n": json.Number("1.0000000000000001")}})
	require.ErrorAs(t, err, &argument)
	require.Equal(t, `validating /n: 1.0000000000000001 is not an integer`, argument.Message)
	require.Equal(t, agentErrors.CodeToolValidation, agentErrors.GetCode(argument.Err))
	require.Zero(t, calls)
}
