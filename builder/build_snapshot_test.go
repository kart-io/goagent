package builder

import (
	"context"
	"errors"
	"testing"

	"github.com/kart-io/goagent/memory"
	"github.com/stretchr/testify/require"
)

func TestBuiltAgentKeepsItsConfigurationAndSession(t *testing.T) {
	ctx := context.Background()
	model := newScriptModel(answer("原会话回答"), answer("新会话回答"))
	mem := memory.NewInMemoryManager(nil)
	b := NewSimpleBuilder(model).WithTools(plainTool()).WithSessionID("original").
		WithMaxTokens(10).WithMetadata("owner", "original").WithMemory(mem)
	original, err := b.Build()
	require.NoError(t, err)

	b.WithSessionID("next").WithMaxTokens(20).WithMetadata("owner", "next")
	next, err := b.Build()
	require.NoError(t, err)
	out, err := original.AsRunnable().Invoke(ctx, "原会话输入")
	require.NoError(t, err)
	require.Equal(t, 10, model.request(0).MaxTokens, "后续构建配置不能追溯修改已有 Agent")
	require.Equal(t, "original", out.Metadata["owner"])
	require.Equal(t, "original", original.GetMetrics()["session_id"])
	history, err := mem.GetConversationHistory(ctx, "original", 0)
	require.NoError(t, err)
	require.Len(t, history, 2, "记忆应使用构建时的会话身份")
	history, err = mem.GetConversationHistory(ctx, "next", 0)
	require.NoError(t, err)
	require.Empty(t, history)

	out, err = next.AsRunnable().Invoke(ctx, "新会话输入")
	require.NoError(t, err)
	require.Equal(t, 20, model.request(1).MaxTokens)
	require.Equal(t, "next", out.Metadata["owner"])
	history, err = mem.GetConversationHistory(ctx, "next", 0)
	require.NoError(t, err)
	require.Len(t, history, 2)
}

func TestBuiltAgentDoesNotAcquireLaterBuilderHooks(t *testing.T) {
	model := newScriptModel(answer("正常回答"))
	b := NewSimpleBuilder(model).WithTools(plainTool())
	agent, err := b.Build()
	require.NoError(t, err)
	later := errors.New("后续构建专用钩子")
	b.WithRequestHook(func(context.Context, *ModelRequest) error { return later })
	out, err := agent.AsRunnable().Invoke(context.Background(), "原配置输入")
	require.NoError(t, err, "已构建 Agent 不应获取后加的钩子")
	require.Equal(t, "正常回答", out.Result)

	next, err := b.Build()
	require.NoError(t, err)
	_, err = next.AsRunnable().Invoke(context.Background(), "新配置输入")
	require.ErrorIs(t, err, later)
}
