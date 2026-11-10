package builder

import (
	"context"
	"errors"
	"testing"

	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func TestBuiltAgentKeepsItsConfigurationAndSession(t *testing.T) {
	ctx := context.Background()
	model := newScriptModel(answer("原会话回答"), answer("新会话回答"))
	log := session.NewMemoryStore()
	b := NewSimpleBuilder(model).WithTools(plainTool()).WithSessionID("original").
		WithMaxTokens(10).WithMetadata("owner", "original").WithSessionLog(log)
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
	require.Len(t, sessionMessages(t, log, "original"), 2, "会话日志应使用构建时的会话身份")
	_, err = log.Read(ctx, "next", 0)
	require.ErrorIs(t, err, agentErrors.ErrSessionNotFound, "尚未执行的会话没有日志")

	out, err = next.AsRunnable().Invoke(ctx, "新会话输入")
	require.NoError(t, err)
	require.Equal(t, 20, model.request(1).MaxTokens)
	require.Equal(t, "next", out.Metadata["owner"])
	require.Len(t, sessionMessages(t, log, "next"), 2)
}

// sessionMessages 返回会话日志推导出的对话（不含 system）。
func sessionMessages(t *testing.T, log session.Store, id string) []llm.Message {
	t.Helper()
	events, err := log.Read(context.Background(), id, 0)
	require.NoError(t, err)
	messages, err := session.DeriveMessages(events)
	require.NoError(t, err)
	return messages
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
