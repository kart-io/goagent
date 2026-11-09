package builder

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/stretchr/testify/require"
)

// 隐藏可选工具接口，验证只有必需 Client 接口的模型也能保存会话。
type plainSessionClient struct{ llm.Client }

func TestPlainSessionHistoryReplayAndFork(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "普通客户端", true: "工具流客户端"}[native], func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			store, err := session.NewFileStore(dir)
			require.NoError(t, err)
			model := mocks.NewScriptedClient(mocks.TextReply("第一轮", "记住了"), mocks.TextReply("第二轮", "小明"), mocks.TextReply("分支", "分支回答"))
			var client llm.Client = model
			if !native {
				client = plainSessionClient{model}
			}
			build := func(id string, log session.Store) *SimpleAgent {
				t.Helper()
				a, e := NewSimpleBuilder(client).WithSessionID(id).WithSessionLog(log).WithDurableScope("聊天/v1").Build()
				require.NoError(t, e)
				return a
			}
			run := func(a *SimpleAgent, log session.Store, id, key, input string) *AgentOutput {
				t.Helper()
				bound := durable.WithJournal(durable.WithExecutionKey(ctx, key), session.DurableStore(log, id))
				out, e := a.AsRunnable().Invoke(bound, input)
				require.NoError(t, e)
				return out
			}
			a := build("聊天", store)
			require.Equal(t, "记住了", run(a, store, "聊天", "轮一", "我叫小明").Result)
			before, err := store.Read(ctx, "聊天", 0)
			require.NoError(t, err)
			// 重建存储和 Agent 后回放，不重复请求模型或追加日志。
			reopened, err := session.NewFileStore(dir)
			require.NoError(t, err)
			require.Equal(t, "记住了", run(build("聊天", reopened), reopened, "聊天", "轮一", "我叫小明").Result)
			after, err := reopened.Read(ctx, "聊天", 0)
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Len(t, model.Requests(), 1)
			require.Equal(t, "小明", run(build("聊天", reopened), reopened, "聊天", "轮二", "我叫什么").Result)
			require.Equal(t, []llm.Message{llm.UserMessage("我叫小明"), {Role: "assistant", Content: "记住了"}, llm.UserMessage("我叫什么")}, model.Requests()[1].Messages)
			require.NoError(t, reopened.Fork(ctx, "聊天", "分支", before[len(before)-1].Seq))
			require.Equal(t, "分支回答", run(build("分支", reopened), reopened, "分支", "分支轮", "换一个问题").Result)
			require.Equal(t, []llm.Message{llm.UserMessage("我叫小明"), {Role: "assistant", Content: "记住了"}, llm.UserMessage("换一个问题")}, model.Requests()[2].Messages)
		})
	}
}

func TestPlainSessionFailureDoesNotCompleteTurn(t *testing.T) {
	ctx := context.Background()
	store := session.NewMemoryStore()
	model := mocks.NewScriptedClient(mocks.ScriptedRule{Name: "失败", Respond: func(context.Context, *llm.CompletionRequest) (mocks.ScriptedReply, error) {
		return mocks.ScriptedReply{}, errors.New("模型暂时失败")
	}}, mocks.TextReply("恢复", "正常回答"))
	a, err := NewSimpleBuilder(plainSessionClient{model}).WithSessionID("聊天").WithSessionLog(store).Build()
	require.NoError(t, err)
	_, err = a.AsRunnable().Invoke(ctx, "失败的输入")
	require.ErrorContains(t, err, "模型暂时失败")
	events, err := store.Read(ctx, "聊天", 0)
	require.NoError(t, err)
	_, open := session.OpenTurn(events)
	require.True(t, open)
	out, err := a.AsRunnable().Invoke(ctx, "新输入")
	require.NoError(t, err)
	require.Equal(t, "正常回答", out.Result)
	require.Equal(t, []llm.Message{llm.UserMessage("新输入")}, model.Requests()[1].Messages)
	events, err = store.Read(ctx, "聊天", 0)
	require.NoError(t, err)
	require.Equal(t, constants.SessionEventTurnEnd, events[len(events)-1].Type)
}

type failPlainSessionAppend struct {
	session.Store
	typ    string
	failed atomic.Bool
}

func (s *failPlainSessionAppend) Append(ctx context.Context, id string, expected int64, events ...session.Event) (int64, error) {
	for _, event := range events {
		if event.Type == s.typ && s.failed.CompareAndSwap(false, true) {
			return expected, errors.New("会话提交失败")
		}
	}
	return s.Store.Append(ctx, id, expected, events...)
}

func TestPlainSessionRecoversLogFailureWithoutRepeatingModel(t *testing.T) {
	for _, typ := range []string{constants.SessionEventUserMessage, constants.SessionEventTurnEnd} {
		t.Run(typ, func(t *testing.T) {
			store := &failPlainSessionAppend{Store: session.NewMemoryStore(), typ: typ}
			model := mocks.NewScriptedClient(mocks.TextReply("唯一请求", "已回答"))
			a, err := NewSimpleBuilder(plainSessionClient{model}).WithSessionID("会话").WithSessionLog(store).WithDurableScope("v1").Build()
			require.NoError(t, err)
			ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "执行"), session.DurableStore(store, "会话"))
			_, err = a.AsRunnable().Invoke(ctx, "问题")
			require.ErrorContains(t, err, "会话提交失败")
			if typ == constants.SessionEventUserMessage {
				require.Empty(t, model.Requests(), "输入没有提交时不能调用模型")
			} else {
				require.Len(t, model.Requests(), 1)
			}
			out, err := a.AsRunnable().Invoke(ctx, "问题")
			require.NoError(t, err)
			require.Equal(t, "已回答", out.Result)
			require.Len(t, model.Requests(), 1, "模型结果已持久化时只补会话终态")
			events, err := store.Read(ctx, "会话", 0)
			require.NoError(t, err)
			messages, err := session.DeriveMessages(events)
			require.NoError(t, err)
			require.Equal(t, []llm.Message{llm.UserMessage("问题"), {Role: "assistant", Content: "已回答"}}, messages)
			_, open := session.OpenTurn(events)
			require.False(t, open)
		})
	}
}
