package builder

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/memory"
	storemem "github.com/kart-io/goagent/store/memory"
)

func TestLongTermMemoryRecallsAndEnqueues(t *testing.T) {
	log := session.NewMemoryStore()
	m, err := memory.New(memory.Config{Store: storemem.New()})
	require.NoError(t, err)
	ctx := memory.WithNamespace(context.Background(), "t1", "alice")
	_, err = m.Remember(ctx, memory.Record{Content: "用户喜欢喝美式咖啡", Importance: 0.8})
	require.NoError(t, err)

	_, err = NewSimpleBuilder(newScriptModel()).WithTools(plainTool()).WithLongTermMemory(m).Build()
	require.Error(t, err, "长期记忆需要会话日志")

	model := newScriptModel(answer("推荐美式"), failing(errors.New("模型不可用")), answer("好的"))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithSessionLog(log).WithSessionID("s").WithLongTermMemory(m).WithMaxIterations(2).Build()
	require.NoError(t, err)

	_, err = agent.AsRunnable().Invoke(ctx, "推荐一杯咖啡")
	require.NoError(t, err)
	msgs := model.request(0).Messages
	recall, input := msgs[len(msgs)-2], msgs[len(msgs)-1]
	require.True(t, strings.HasPrefix(recall.Content, memory.RecallPrefix), "召回块插在本轮输入之前")
	require.Equal(t, "user", recall.Role, "召回内容不作为 system 指令")
	require.Contains(t, recall.Content, "美式咖啡")
	require.Equal(t, "推荐一杯咖啡", input.Content)

	events, err := log.Read(ctx, "s", 0)
	require.NoError(t, err)
	start := events[0].Seq
	for _, ev := range events {
		if ev.Type == "turn/start" {
			start = ev.Seq
			break
		}
	}
	job, ok, err := m.Job(ctx, "s/"+strconv.FormatInt(start, 10))
	require.NoError(t, err)
	require.True(t, ok, "轮次结束后登记抽取任务")
	require.Equal(t, []string{"t1", "alice"}, job.Namespace)
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
		if ev.Type != "turn/start" {
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
