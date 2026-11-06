package multiagent

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/workflow"
)

func mergeLogs(_ context.Context, base record, outs []record) (record, error) {
	merged := cloneRecord(base)
	for _, o := range outs {
		merged.Log = append(merged.Log, o.Log[len(base.Log):]...)
	}
	return merged, nil
}

func lookups(t *testing.T) Step[record] {
	t.Helper()
	p, err := ParallelStep("lookup", ParallelConfig[record]{Steps: []Step[record]{mark("kb"), mark("order"), mark("ship")}, Clone: cloneRecord, Merge: mergeLogs})
	require.NoError(t, err)
	return p
}

func TestParallelStepValidation(t *testing.T) {
	inner := lookups(t)
	cfg := func(steps ...Step[record]) ParallelConfig[record] {
		return ParallelConfig[record]{Steps: steps, Clone: cloneRecord, Merge: mergeLogs}
	}
	for name, p := range map[string]struct {
		name string
		cfg  ParallelConfig[record]
	}{
		"名称无效":    {"", cfg(mark("a"))},
		"没有分支":    {"p", cfg()},
		"缺 Clone": {"p", ParallelConfig[record]{Steps: []Step[record]{mark("a")}, Merge: mergeLogs}},
		"缺 Merge": {"p", ParallelConfig[record]{Steps: []Step[record]{mark("a")}, Clone: cloneRecord}},
		"分支重名":    {"p", cfg(mark("a"), mark("a"))},
		"分支缺 Run": {"p", cfg(Step[record]{Name: "a"})},
		"嵌套并行":    {"p", cfg(mark("a"), inner)},
	} {
		_, err := ParallelStep(p.name, p.cfg)
		require.Error(t, err, name)
	}
}

func TestParallelStepRunCannotBeInvokedDirectly(t *testing.T) {
	_, err := lookups(t).Run.Invoke(context.Background(), record{})
	require.ErrorIs(t, err, errParallelRun)
}

// single 用 AddStep 构造 START → s → END。
func single[T any](t *testing.T, cfg workflow.Config, s Step[T]) *workflow.Graph[T] {
	t.Helper()
	g := workflow.NewGraph[T](cfg)
	require.NoError(t, AddStep(g, s))
	require.NoError(t, g.AddEdge(workflow.START, s.Name))
	require.NoError(t, g.AddEdge(s.Name, workflow.END))
	return g
}

func TestParallelStepCopiesSteps(t *testing.T) {
	steps := []Step[record]{mark("kb"), mark("order")}
	p, err := ParallelStep("lookup", ParallelConfig[record]{Steps: steps, Clone: cloneRecord, Merge: mergeLogs})
	require.NoError(t, err)
	steps[0] = mark("changed")
	run, err := single(t, workflow.Config{}, p).Compile(context.Background())
	require.NoError(t, err)
	out, err := run.Invoke(context.Background(), record{})
	require.NoError(t, err)
	require.Equal(t, []string{"kb", "order"}, out.Log)
}

// handGraph 用 AddStep 手写 START → triage → lookup → END。
func handGraph(t *testing.T) *workflow.Graph[record] {
	t.Helper()
	g := workflow.NewGraph[record]()
	require.NoError(t, AddStep(g, mark("triage")))
	require.NoError(t, AddStep(g, lookups(t)))
	require.NoError(t, g.AddEdge(workflow.START, "triage"))
	require.NoError(t, g.AddEdge("triage", "lookup"))
	require.NoError(t, g.AddEdge("lookup", workflow.END))
	return g
}

func TestAddStepRegistersPlainAndParallelSteps(t *testing.T) {
	run, err := handGraph(t).Compile(context.Background())
	require.NoError(t, err)
	out, err := run.Invoke(context.Background(), record{})
	require.NoError(t, err)
	require.Equal(t, []string{"triage", "kb", "order", "ship"}, out.Log)

	e := newEnv(t, "add-step")
	snap, err := compile(t, e, handGraph(t), "v1").Run(context.Background(), e.id, record{}, "")
	require.NoError(t, err)
	require.Equal(t, out.Log, snap.State.Log)
	require.Equal(t, 2, snap.Steps, "整组并行计 1 步")
}

func TestAddStepValidation(t *testing.T) {
	require.Error(t, AddStep[record](nil, mark("a")))
	g := workflow.NewGraph[record]()
	require.Error(t, AddStep(g, Step[record]{Name: "a"}))
	require.NoError(t, AddStep(g, mark("a")))
	require.ErrorContains(t, AddStep(g, mark("a")), "duplicate node")
	require.ErrorContains(t, AddStep(g, Step[record]{Name: "a", Run: lookups(t).Run, parallel: lookups(t).parallel}), "duplicate node")
}

func parallelGraph(t *testing.T, cfg ParallelConfig[record]) *core.StreamRunnable[record, record] {
	t.Helper()
	p, err := ParallelStep("p", cfg)
	require.NoError(t, err)
	run, err := single(t, workflow.Config{}, p).Compile(context.Background())
	require.NoError(t, err)
	return run
}

func TestParallelMergeErrorFailsNode(t *testing.T) {
	run := parallelGraph(t, ParallelConfig[record]{
		Steps: []Step[record]{mark("a")}, Clone: cloneRecord,
		Merge: func(context.Context, record, []record) (record, error) { return record{}, errors.New("合并失败") },
	})
	_, err := run.Invoke(context.Background(), record{})
	require.ErrorContains(t, err, `node "p"`)
	require.ErrorContains(t, err, "合并失败")
}

func TestParallelClonePanicBecomesError(t *testing.T) {
	run := parallelGraph(t, ParallelConfig[record]{
		Steps: []Step[record]{mark("a")}, Merge: mergeLogs,
		Clone: func(record) record { panic("clone 崩溃") },
	})
	_, err := run.Invoke(context.Background(), record{})
	var panicErr *agentErrors.PanicError
	require.ErrorAs(t, err, &panicErr)
}

func TestParallelBranchFailureCancelsSiblings(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	slow := Step[record]{Name: "slow", Run: core.NewRunnableFunc(func(ctx context.Context, r record) (record, error) {
		close(started)
		select {
		case <-ctx.Done():
			close(canceled)
			return r, ctx.Err()
		case <-time.After(10 * time.Second):
			return r, nil
		}
	})}
	// broken 等 slow 开始后才失败，保证取消的是正在运行的兄弟分支。
	broken := Step[record]{Name: "broken", Run: core.NewRunnableFunc(func(context.Context, record) (record, error) {
		<-started
		return record{}, errors.New("订单服务不可用")
	})}
	run := parallelGraph(t, ParallelConfig[record]{Steps: []Step[record]{slow, broken}, Clone: cloneRecord, Merge: mergeLogs})
	_, err := run.Invoke(context.Background(), record{})
	require.ErrorContains(t, err, `parallel branch "broken"`)
	require.ErrorContains(t, err, "订单服务不可用")
	select {
	case <-canceled:
	default:
		t.Fatal("失败分支返回后，图应已取消并等待兄弟分支结束")
	}
}

func TestDurableParallelDoesNotRerunCompletedBranches(t *testing.T) {
	e := newEnv(t, "parallel-restart")
	var kb, order atomic.Int32
	failOrder := atomic.Bool{}
	failOrder.Store(true)
	count := func(name string, n *atomic.Int32, fail *atomic.Bool) Step[record] {
		return Step[record]{Name: name, Run: core.NewRunnableFunc(func(_ context.Context, r record) (record, error) {
			n.Add(1)
			if fail != nil && fail.Load() {
				return r, errors.New("暂时失败")
			}
			r.Log = append(r.Log, name)
			return r, nil
		})}
	}
	build := func() *workflow.Graph[record] {
		p, err := ParallelStep("lookup", ParallelConfig[record]{Steps: []Step[record]{count("kb", &kb, nil), count("order", &order, &failOrder)}, Clone: cloneRecord, Merge: mergeLogs})
		require.NoError(t, err)
		return single(t, workflow.Config{MaxConcurrency: 1}, p)
	}
	_, err := compile(t, e, build(), "v1").Run(context.Background(), e.id, record{}, "")
	require.ErrorContains(t, err, "暂时失败")
	failOrder.Store(false)
	e.restart()
	snap, err := compile(t, e, build(), "v1").Run(context.Background(), e.id, record{}, "")
	require.NoError(t, err)
	require.Equal(t, []string{"kb", "order"}, snap.State.Log)
	require.EqualValues(t, 1, kb.Load(), "已保存输出的分支不重跑")
	require.EqualValues(t, 2, order.Load())
}

func TestDurableParallelRejectsBranchOutputThatCannotRoundTrip(t *testing.T) {
	type money struct{ Amount float64 }
	bad := Step[money]{Name: "bad", Run: core.NewRunnableFunc(func(context.Context, money) (money, error) { return money{Amount: math.Inf(1)}, nil })}
	p, err := ParallelStep("p", ParallelConfig[money]{
		Steps: []Step[money]{bad}, Clone: func(m money) money { return m },
		Merge: func(_ context.Context, m money, _ []money) (money, error) { return m, nil },
	})
	require.NoError(t, err)
	g := single(t, workflow.Config{}, p)
	e := newEnv(t, "parallel-json")
	d, err := g.CompileDurable(context.Background(), workflow.DurableConfig{Store: e.store, Version: "v1"})
	require.NoError(t, err)
	_, err = d.Run(context.Background(), e.id, money{}, "")
	var jsonErr *json.UnsupportedValueError
	require.ErrorAs(t, err, &jsonErr)
}
