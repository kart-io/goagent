package builder

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

const plainQuery = "推荐一杯咖啡"

func runPlainRecall(t *testing.T, ns context.Context, key string, log session.Store, journal durable.Store, client llm.Client, m *memory.Memory) error {
	t.Helper()
	a, e := NewSimpleBuilder(client).WithSessionID("s").WithSessionLog(log).WithDurableScope("plain").WithInterruptedModelResends(1).WithLongTermMemory(m).Build()
	require.NoError(t, e)
	_, e = a.AsRunnable().Invoke(durable.WithJournal(durable.WithExecutionKey(ns, key), journal), plainQuery)
	return e
}

func plainMode(t *testing.T, log session.Store) string {
	t.Helper()
	for _, ev := range sessionEvents(t, log, "s") {
		if ev.Type == constants.SessionEventTurnStart {
			var d struct {
				Mode string `json:"recall_mode"`
			}
			require.NoError(t, json.Unmarshal(ev.Data, &d))
			return d.Mode
		}
	}
	t.Fatal("缺少轮次开始记录")
	return ""
}

func TestPlainRecallUsesReferencesAndDropsForgotten(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{true: "支持工具流", false: "普通客户端"}[native], func(t *testing.T) {
			ns := memory.WithNamespace(context.Background(), "t", "alice")
			m, ids := newTestMemory(t, ns, "用户喜欢喝美式咖啡")
			log := session.NewMemoryStore()
			journal := session.DurableStore(log, "s")
			model := mocks.NewScriptedClient(mocks.TextReply("首次", "好"), mocks.TextReply("删除后", "好"))
			var client llm.Client = model
			if !native {
				client = plainSessionClient{model}
			}
			require.NoError(t, runPlainRecall(t, ns, "one", log, journal, client, m))
			require.Equal(t, "plain-memory/1", plainMode(t, log))
			msg, ok := recallIn(model.Requests()[0].Messages)
			require.True(t, ok)
			require.Contains(t, msg.Content, "美式咖啡")
			for _, ev := range sessionEvents(t, log, "s") {
				require.NotContains(t, string(ev.Data), "美式咖啡")
			}
			require.NoError(t, m.Forget(ns, ids[0]))
			require.NoError(t, runPlainRecall(t, ns, "one", log, journal, client, m))
			require.Len(t, model.Requests(), 1)
			require.NoError(t, runPlainRecall(t, ns, "two", log, journal, client, m))
			_, ok = recallIn(model.Requests()[1].Messages)
			require.False(t, ok)
		})
	}
}

func TestPlainNoneCannotUpgrade(t *testing.T) {
	log := session.NewMemoryStore()
	journal := session.DurableStore(log, "s")
	ctx := context.Background()
	model := mocks.NewScriptedClient(mocks.TextReply("首次", "好"))
	require.NoError(t, runPlainRecall(t, ctx, "one", log, journal, model, nil))
	require.Equal(t, "plain-none/1", plainMode(t, log))
	m, _ := newTestMemory(t, memory.WithNamespace(ctx, "t", "alice"), "美式咖啡")
	require.NoError(t, runPlainRecall(t, ctx, "one", log, journal, model, m))
	require.Len(t, model.Requests(), 1)
}

type missingPlainOp struct {
	durable.Store
	missing map[string]error
}

func (s missingPlainOp) Load(ctx context.Context, id string) (*durable.Snapshot, error) {
	if err := s.missing[id]; err != nil {
		return nil, err
	}
	return s.Store.Load(ctx, id)
}
func plainOp(id string) string {
	return durable.OperationStorageID(durable.ChildKey(durable.ChildKey("one", "agent/plain"), id))
}

func TestNewPlainRecallProtocolMissingRecordCannotFallback(t *testing.T) {
	ns := memory.WithNamespace(context.Background(), "t", "alice")
	m, _ := newTestMemory(t, ns, "美式咖啡")
	log := session.NewMemoryStore()
	journal := session.DurableStore(log, "s")
	model := mocks.NewScriptedClient(mocks.TextReply("首次", "好"))
	require.NoError(t, runPlainRecall(t, ns, "one", log, journal, model, m))
	for _, other := range []string{"model/answer/0", "memory/use/0"} {
		broken := missingPlainOp{journal, map[string]error{plainOp("memory/recall"): os.ErrNotExist, plainOp(other): os.ErrNotExist}}
		err := runPlainRecall(t, ns, "one", log, broken, model, m)
		require.True(t, agentErrors.IsCode(err, agentErrors.CodeStateValidation), "%v", err)
	}
	cause := errors.New("读取失败")
	require.ErrorIs(t, runPlainRecall(t, ns, "one", log, missingPlainOp{journal, map[string]error{plainOp("memory/recall"): cause}}, model, m), cause)
	require.Error(t, runPlainRecall(t, ns, "one", log, journal, model, nil))
	require.Len(t, model.Requests(), 1)
}

func legacyPlainReplay(t *testing.T, filename string, requests int) {
	ctx := memory.WithNamespace(context.Background(), "t", "alice")
	log := session.NewMemoryStore()
	data, err := os.ReadFile("testdata/" + filename + ".json")
	require.NoError(t, err)
	var events []session.Event
	require.NoError(t, json.Unmarshal(data, &events))
	_, err = log.Append(ctx, "s", 0, events...)
	require.NoError(t, err)
	journal := session.DurableStore(log, "s")
	m, _ := newTestMemory(t, ctx, "美式咖啡")
	model := mocks.NewScriptedClient(mocks.TextReply("恢复", "好"))
	require.NoError(t, runPlainRecall(t, ctx, "one", log, journal, model, m))
	require.Len(t, model.Requests(), requests)
	if requests > 0 {
		require.Equal(t, []llm.Message{llm.UserMessage(plainQuery)}, model.Requests()[0].Messages)
	}
	for _, id := range []string{"memory/recall", "memory/use/0"} {
		_, err = journal.Load(ctx, plainOp(id))
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}
func TestLegacyPlainCompletedTurnReplaysWithoutMemoryOps(t *testing.T) {
	legacyPlainReplay(t, "legacy_plain_completed", 0)
}
func TestLegacyPlainInterruptedTurnKeepsOriginalIntent(t *testing.T) {
	legacyPlainReplay(t, "legacy_plain_interrupted", 1)
}

func TestPlainRecallInterruptedTurnHydratesRecordedRefs(t *testing.T) {
	ns := memory.WithNamespace(context.Background(), "t", "alice")
	m, ids := newTestMemory(t, ns, "用户喜欢喝美式咖啡")
	log := session.NewMemoryStore()
	journal := session.DurableStore(log, "s")
	interrupted := mocks.NewScriptedClient(mocks.ScriptedRule{Name: "中断", Respond: func(context.Context, *llm.CompletionRequest) (mocks.ScriptedReply, error) {
		return mocks.ScriptedReply{}, context.Canceled
	}})
	require.Error(t, runPlainRecall(t, ns, "one", log, journal, interrupted, m))
	first, ok := recallIn(interrupted.Requests()[0].Messages)
	require.True(t, ok)
	require.Contains(t, first.Content, "美式咖啡")
	require.NoError(t, m.Forget(ns, ids[0]))
	_, err := m.Remember(ns, memory.Record{Content: "用户喜欢喝拿铁咖啡", Importance: 0.9})
	require.NoError(t, err)
	resumed := mocks.NewScriptedClient(mocks.TextReply("恢复", "好"))
	require.NoError(t, runPlainRecall(t, ns, "one", log, journal, resumed, m))
	require.Equal(t, []llm.Message{llm.UserMessage(plainQuery)}, resumed.Requests()[0].Messages)
}

func TestPlainNoneRecoveryPreservesUserHistory(t *testing.T) {
	ctx := context.Background()
	log := session.NewMemoryStore()
	journal := session.DurableStore(log, "s")
	model := mocks.NewScriptedClient(mocks.TextReply("首次", "好"), mocks.TextReply("第二轮", "好"))
	a, err := NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(log).WithDurableScope("plain").Build()
	require.NoError(t, err)
	_, err = a.AsRunnable().Invoke(durable.WithJournal(durable.WithExecutionKey(ctx, "prior"), journal), memory.RecallPrefix+"用户自己的文字")
	require.NoError(t, err)
	require.NoError(t, runPlainRecall(t, ctx, "one", log, journal, model, nil))
	m, _ := newTestMemory(t, memory.WithNamespace(ctx, "t", "alice"), "咖啡")
	require.NoError(t, runPlainRecall(t, ctx, "one", log, journal, model, m))
	require.Len(t, model.Requests(), 2)
}

func TestPlainRecallModeCannotSwitchToNativeOnResume(t *testing.T) {
	ctx := context.Background()
	log := session.NewMemoryStore()
	journal := session.DurableStore(log, "s")
	client := mocks.NewScriptedClient(mocks.TextReply("纯聊天", "好"))
	require.NoError(t, runPlainRecall(t, ctx, "one", log, journal, client, nil))
	offered, err := dispatcherTools(tools.DispatchConfig{}, plainTool())
	require.NoError(t, err)
	a, err := NewSimpleBuilder(client).WithSessionID("s").WithSessionLog(log).WithDurableScope("plain").WithTools(offered...).Build()
	require.NoError(t, err)
	_, err = a.AsRunnable().Invoke(durable.WithJournal(durable.WithExecutionKey(ctx, "one"), journal), plainQuery)
	require.ErrorContains(t, err, "纯聊天轮次")
	require.Len(t, client.Requests(), 1)
}
