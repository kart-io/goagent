package builder

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
	storemem "github.com/kart-io/goagent/store/memory"
	"github.com/kart-io/goagent/tools"
)

func newTestMemory(t *testing.T, ctx context.Context, contents ...string) (*memory.Memory, []string) {
	t.Helper()
	fs, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	m, err := memory.New(memory.Config{Store: storemem.New(), Durable: fs})
	require.NoError(t, err)
	var ids []string
	for _, c := range contents {
		r, err := m.Remember(ctx, memory.Record{Content: c, Importance: 0.8})
		require.NoError(t, err)
		ids = append(ids, r.ID)
	}
	return m, ids
}

func recallIn(msgs []llm.Message) (llm.Message, bool) {
	for _, msg := range msgs {
		if msg.Role == "user" && strings.HasPrefix(msg.Content, memory.RecallPrefix) {
			return msg, true
		}
	}
	return llm.Message{}, false
}

func TestLongTermMemoryRecallsAndEnqueues(t *testing.T) {
	log := session.NewMemoryStore()
	ctx := memory.WithNamespace(context.Background(), "t1", "alice")
	m, _ := newTestMemory(t, ctx, "用户喜欢喝美式咖啡")

	_, err := NewSimpleBuilder(newScriptModel()).WithTools(plainTool()).WithLongTermMemory(m).Build()
	require.Error(t, err, "长期记忆需要会话日志")

	model := newScriptModel(callTool("c1", "lookup"), answer("推荐美式"), failing(errors.New("模型不可用")), answer("好的"))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithSessionLog(log).WithSessionID("s").WithLongTermMemory(m).WithMaxIterations(2).Build()
	require.NoError(t, err)

	_, err = agent.AsRunnable().Invoke(ctx, "推荐一杯咖啡")
	require.NoError(t, err)
	for i := range 2 {
		msgs := model.request(i).Messages
		recall := msgs[len(msgs)-1]
		require.True(t, strings.HasPrefix(recall.Content, memory.RecallPrefix), "第 %d 步的召回块在请求末尾（没有运行上下文）", i)
		require.Equal(t, "user", recall.Role, "召回内容不作为 system 指令")
		require.Contains(t, recall.Content, "美式咖啡")
	}
	require.Equal(t, "推荐一杯咖啡", model.request(0).Messages[len(model.request(0).Messages)-2].Content, "召回块在本轮输入之后")

	events, err := log.Read(ctx, "s", 0)
	require.NoError(t, err)
	history, err := session.DeriveMessages(events)
	require.NoError(t, err)
	_, found := recallIn(history)
	require.False(t, found, "召回块不进入会话历史")
	for _, ev := range events {
		require.NotContains(t, string(ev.Data), "美式咖啡", "会话日志中没有召回正文")
	}

	start := events[0].Seq
	for _, ev := range events {
		if ev.Type == constants.SessionEventTurnStart {
			start = ev.Seq
			var d session.TurnStart
			require.NoError(t, json.Unmarshal(ev.Data, &d))
			require.JSONEq(t, `{"write":["t1","alice"],"read":[["t1","alice"]],"scope":"0","seq":1}`, string(d.Memory), "轮次绑定记入 turn/start，含作用域与提交序号")
			break
		}
	}
	job, ok, err := m.Job(ctx, "s/"+strconv.FormatInt(start, 10))
	require.NoError(t, err)
	require.True(t, ok, "轮次结束后登记抽取任务")
	require.Equal(t, []string{"t1", "alice"}, job.Namespace)
	require.Equal(t, "0", job.Scope, "任务绑定轮次开始时的作用域")
	require.False(t, job.Aborted)

	_, err = agent.AsRunnable().Invoke(ctx, "第二个问题")
	require.Error(t, err)
	_, err = agent.AsRunnable().Invoke(ctx, "第三个问题")
	require.NoError(t, err)
	jobs := 0
	aborted := false
	events, err = log.Read(ctx, "s", 0)
	require.NoError(t, err)
	for _, ev := range events {
		if ev.Type != constants.SessionEventTurnStart {
			continue
		}
		if j, ok, err := m.Job(ctx, "s/"+strconv.FormatInt(ev.Seq, 10)); err == nil && ok {
			jobs++
			aborted = aborted || j.Aborted
		}
	}
	require.Equal(t, 3, jobs, "失败的轮次在下一轮开始时以中止登记")
	require.True(t, aborted)

	_, err = agent.AsRunnable().Invoke(context.Background(), "没有命名空间")
	require.Error(t, err, "没有命名空间时拒绝读写长期记忆")
}

// 中止轮次的任务按它开始时的绑定登记，不随下一轮的命名空间改变。
func TestLongTermMemoryAbortedTurnUsesStartBinding(t *testing.T) {
	log := session.NewMemoryStore()
	alice := memory.WithNamespace(context.Background(), "t1", "alice")
	bob := memory.WithNamespace(context.Background(), "t1", "bob")
	m, _ := newTestMemory(t, alice)
	model := newScriptModel(failing(errors.New("模型不可用")), answer("好的"))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithSessionLog(log).WithSessionID("s").WithLongTermMemory(m).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(alice, "第一轮")
	require.Error(t, err)
	_, err = agent.AsRunnable().Invoke(bob, "第二轮")
	require.NoError(t, err)
	first := sessionEvents(t, log, "s")[0].Seq
	job, ok, err := m.Job(alice, "s/"+strconv.FormatInt(first, 10))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []string{"t1", "alice"}, job.Namespace)
	require.True(t, job.Aborted)
}

// 写入一条升级前的召回块：推导历史时被去掉。
func TestLongTermMemoryFiltersLegacyRecallBlocks(t *testing.T) {
	log := session.NewMemoryStore()
	ctx := memory.WithNamespace(context.Background(), "t1", "alice")
	m, _ := newTestMemory(t, ctx)
	var legacy []session.Event
	for _, item := range []struct {
		typ  string
		data any
	}{
		{constants.SessionEventTurnStart, session.TurnStart{Input: "旧问题"}},
		{constants.SessionEventUserMessage, session.MessageData{Message: llm.UserMessage(memory.RecallPrefix + "旧召回")}},
		{constants.SessionEventUserMessage, session.MessageData{Message: llm.UserMessage("旧问题")}},
		{constants.SessionEventAssistantMessage, session.MessageData{Message: llm.Message{Role: "assistant", Content: "旧回答"}}},
		{constants.SessionEventTurnEnd, session.TurnEnd{Answer: "旧回答"}},
	} {
		ev, err := session.NewEvent(item.typ, item.data)
		require.NoError(t, err)
		legacy = append(legacy, ev)
	}
	_, err := log.Append(ctx, "s", 0, legacy...)
	require.NoError(t, err)

	model := newScriptModel(answer("好的"))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithSessionLog(log).WithSessionID("s").WithLongTermMemory(m).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(ctx, "新问题")
	require.NoError(t, err)
	for _, msg := range model.request(0).Messages {
		require.NotContains(t, msg.Content, "旧召回")
	}
}

func TestLongTermMemoryRejectsResponseCache(t *testing.T) {
	ctx := memory.WithNamespace(context.Background(), "t1", "alice")
	m, _ := newTestMemory(t, ctx)
	_, err := NewSimpleBuilder(newScriptModel()).WithTools(plainTool()).WithSessionLog(session.NewMemoryStore()).WithSessionID("s").
		WithMiddleware(middleware.NewCacheMiddleware(time.Minute)).WithLongTermMemory(m).Build()
	require.Error(t, err)
	require.Contains(t, err.Error(), "CacheMiddleware")
}

// memory_save 写入轮次绑定的写命名空间。
func TestLongTermMemoryToolsUseTurnBinding(t *testing.T) {
	ctx := memory.WithNamespace(context.Background(), "t1", "alice")
	m, _ := newTestMemory(t, ctx)
	model := newScriptModel(callToolWith("c1", constants.ToolMemorySave, `{"content":"用户住在杭州"}`), answer("记住了"))
	agent, err := NewSimpleBuilder(model).WithTools(m.Tools()...).WithSessionLog(session.NewMemoryStore()).WithSessionID("s").WithLongTermMemory(m).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(ctx, "记住我住在杭州")
	require.NoError(t, err)
	got, err := m.Recall(ctx, memory.Query{Text: "杭州"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "用户住在杭州", got[0].Content)
}

// crashOn 在第一次保存内容含 match（操作 ID）的快照时报错，模拟进程在该操作提交前中断。
type crashOn struct {
	durable.Store
	match   string
	crashed bool
}

func (s *crashOn) Save(ctx context.Context, name string, v *durable.Snapshot) error {
	if !s.crashed && strings.Contains(string(v.Data), s.match) {
		s.crashed = true
		return errors.New("crash")
	}
	return s.Store.Save(ctx, name, v)
}

// 持久运行在第二次模型请求前中断；恢复前遗忘了召回中的记录：已完成的步骤按意图回放，恢复后的请求不含被遗忘的内容，
// 持久日志中只有引用。
func TestLongTermMemoryDurableResumeDropsForgotten(t *testing.T) {
	store := session.NewMemoryStore()
	ns := memory.WithNamespace(context.Background(), "t1", "alice")
	m, ids := newTestMemory(t, ns, "用户喜欢喝美式咖啡")
	crash := &crashOn{match: "model/turn/1"}
	run := func(ctx context.Context, model *scriptModel) (*AgentOutput, error) {
		dispatched, err := dispatcherTools(tools.DispatchConfig{}, plainTool())
		require.NoError(t, err)
		agent, err := NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(store).WithDurableScope("memory").
			WithTools(dispatched...).WithLongTermMemory(m).WithMaxIterations(4).Build()
		require.NoError(t, err)
		crash.Store = session.DurableStore(store, "s")
		return agent.AsRunnable().Invoke(durable.WithJournal(durable.WithExecutionKey(ctx, "run-1"), crash), "推荐一杯咖啡")
	}
	first := newScriptModel(callTool("c1", "lookup"), answer("推荐美式"))
	_, err := run(ns, first)
	require.ErrorContains(t, err, "crash")
	_, found := recallIn(first.request(0).Messages)
	require.True(t, found, "首次执行的请求带召回块")

	_, err = run(memory.WithNamespace(context.Background(), "t1", "bob"), newScriptModel(answer("x")))
	require.ErrorIs(t, err, agentErrors.ErrMemoryNamespaceChanged, "恢复时换了命名空间")

	require.NoError(t, m.Forget(ns, ids[0]))
	resumed := newScriptModel(answer("推荐美式"))
	out, err := run(ns, resumed)
	require.NoError(t, err, "第 0 步的意图不因召回内容被删除而改变")
	require.Equal(t, "推荐美式", out.Result)
	require.Equal(t, 1, resumed.count(), "第 0 步从日志回放")
	_, found = recallIn(resumed.request(0).Messages)
	require.False(t, found, "被遗忘的记录不再发给模型")

	for _, ev := range sessionEvents(t, store, "s") {
		require.NotContains(t, string(ev.Data), "美式咖啡", "会话日志与持久日志都没有召回正文")
	}
}

// G0 写下的持久运行（召回快照为 memory-recall/1，轮次绑定没有作用域）在 G1 下恢复：意图不变，已完成的第 0 步从日志回放，
// 召回为空。日志由 a4f09197 运行与上面相同的用例、在第 1 步崩溃后导出。
func TestLongTermMemoryResumesG0Journal(t *testing.T) {
	data, err := os.ReadFile("testdata/g0_durable_recall_session.json")
	require.NoError(t, err)
	var events []session.Event
	require.NoError(t, json.Unmarshal(data, &events))
	store := session.NewMemoryStore()
	_, err = store.Append(context.Background(), "s", 0, events...)
	require.NoError(t, err)
	require.Equal(t, events, sessionEvents(t, store, "s"), "按原序号载入")

	ns := memory.WithNamespace(context.Background(), "t1", "alice")
	m, _ := newTestMemory(t, ns, "用户喜欢喝美式咖啡")
	dispatched, err := dispatcherTools(tools.DispatchConfig{}, plainTool())
	require.NoError(t, err)
	resumed := newScriptModel(answer("推荐美式"))
	agent, err := NewSimpleBuilder(resumed).WithSessionID("s").WithSessionLog(store).WithDurableScope("memory").
		WithTools(dispatched...).WithLongTermMemory(m).WithMaxIterations(4).Build()
	require.NoError(t, err)
	journal := session.DurableStore(store, "s")
	out, err := agent.AsRunnable().Invoke(durable.WithJournal(durable.WithExecutionKey(ns, "run-1"), journal), "推荐一杯咖啡")
	require.NoError(t, err, "G0 日志的意图在 G1 下不变")
	require.Equal(t, "推荐美式", out.Result)
	require.Equal(t, 1, resumed.count(), "第 0 步从日志回放")
	_, found := recallIn(resumed.request(0).Messages)
	require.False(t, found, "memory-recall/1 的快照恢复时召回为空")
}

func TestRecallIntentDependsOnlyOnJournal(t *testing.T) {
	history := []llm.Message{llm.UserMessage("问题")}
	ctxMsg := llm.SystemMessage("运行上下文")
	with := func(text string) []llm.Message {
		msgs := append([]llm.Message{}, history...)
		if text != "" {
			msgs = append(msgs, llm.UserMessage(text))
		}
		return append(msgs, ctxMsg)
	}
	a := recallIntent(with("召回 A"), recallSlot{text: "召回 A", placeholder: "P", index: 1})
	b := recallIntent(with("召回 B"), recallSlot{text: "召回 B", placeholder: "P", index: 1})
	gone := recallIntent(with(""), recallSlot{placeholder: "P", index: 1})
	require.Equal(t, a, b, "同一子集配不同正文，意图相同")
	require.Equal(t, a, gone, "恢复时召回消息消失，意图不变")
	require.Equal(t, []llm.Message{history[0], llm.UserMessage("P"), ctxMsg}, a)
	require.Equal(t, with(""), recallIntent(with(""), recallSlot{index: 1}), "子集为空时没有占位消息")
}
