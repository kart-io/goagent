package builder

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// 同轮并发执行时 OnResult 推迟到整轮结束后按调用顺序触发：首次执行与恢复回放的触发顺序都等于
// 模型给出的顺序，按到达顺序编号的宿主账本在恢复后与首次一致。
type arrivalLedger struct {
	mu    sync.Mutex
	refs  map[string]string // ExecutionKey -> 引用号
	order []string          // 到达顺序（调用 id）
}

func (l *arrivalLedger) record(_ context.Context, r tools.DispatchResult) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.refs[r.ExecutionKey]; !ok {
		l.refs[r.ExecutionKey] = fmt.Sprintf("E%d", len(l.refs)+1)
		l.order = append(l.order, r.Args["id"].(string))
	}
	return nil
}

func TestOnResultReplayOrderAfterConcurrentRun(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store)
	// id 越小越慢：首次完成顺序为 4,3,2,1,0。
	var active, peak atomic.Int64
	tool := tools.NewBaseTool("probe", "probe", argSchema, func(_ context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		now := active.Add(1)
		defer active.Add(-1)
		for p := peak.Load(); now > p && !peak.CompareAndSwap(p, now); p = peak.Load() {
		}
		var n int
		_, _ = fmt.Sscanf(in.Args["id"].(string), "%d", &n)
		time.Sleep(time.Duration(50-n*10) * time.Millisecond)
		return &interfaces.ToolOutput{Success: true, Result: "r"}, nil
	})
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: probeCalls("0", "1", "2", "3", "4")}},
		[]*llm.StreamChunk{{Delta: "汇总"}, {Done: true, FinishReason: "stop"}},
	)
	process := func() *arrivalLedger {
		ledger := &arrivalLedger{refs: map[string]string{}}
		reg := tools.NewRegistry()
		require.NoError(t, reg.Register(tool))
		d, err := tools.NewDispatcher(reg, tools.DispatchConfig{MaxConcurrency: 4, OnResult: ledger.record})
		require.NoError(t, err)
		agent, err := NewSimpleBuilder(model).WithDurableScope("p").WithTools(d.Tools()...).WithParallelToolCalls(5).WithMaxIterations(3).Build()
		require.NoError(t, err)
		out, err := agent.AsRunnable().Invoke(ctx, "并发")
		require.NoError(t, err)
		require.Equal(t, "汇总", out.Result)
		return ledger
	}
	first := process()
	require.Equal(t, int64(4), peak.Load(), "首次执行确实并发，峰值受 MaxConcurrency 约束")
	callOrder := []string{"0", "1", "2", "3", "4"}
	require.Equal(t, callOrder, first.order, "首次执行按调用顺序触发，与完成顺序无关")

	for range 30 {
		replay := process()
		require.Equal(t, callOrder, replay.order, "恢复回放同样按调用顺序触发")
		require.Equal(t, first.refs, replay.refs, "恢复后账本编号与首次一致")
	}
	require.Len(t, model.requests, 2, "恢复时模型回合全部回放")
}
