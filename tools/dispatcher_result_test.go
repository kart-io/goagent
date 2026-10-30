package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/observe"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func journalCtx(t *testing.T) context.Context {
	t.Helper()
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	return durable.WithJournal(durable.WithExecutionKey(context.Background(), "exec"), store)
}

func callInput(id, callID string) *interfaces.ToolInput {
	return &interfaces.ToolInput{CallID: callID, Args: map[string]any{"id": id}}
}

func TestDispatcherOnResultFreshAndReplay(t *testing.T) {
	ctx := journalCtx(t)
	calls := 0
	tool := NewBaseTool("lookup", "t", idSchema, func(_ context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls++
		return &interfaces.ToolOutput{Success: true, Result: map[string]any{"id": in.Args["id"], "score": 7}}, nil
	})
	var results []DispatchResult
	d := argDispatcher(t, DispatchConfig{OnResult: func(_ context.Context, r DispatchResult) error {
		results = append(results, r)
		return nil
	}}, tool)
	for range 2 {
		out, err := d.Invoke(ctx, ToolRequest{Name: "lookup", Input: callInput("a", "c1")})
		require.NoError(t, err)
		require.True(t, out.Success)
	}
	require.Equal(t, 1, calls)
	require.Len(t, results, 2)
	require.False(t, results[0].Replayed)
	require.True(t, results[1].Replayed)
	require.Equal(t, results[0].Output, results[1].Output, "首次与回放都从日志解码")
	for _, r := range results {
		require.Equal(t, "lookup", r.Name)
		require.Equal(t, "c1", r.CallID)
		require.Equal(t, map[string]any{"id": "a"}, r.Args)
		require.NotEmpty(t, r.ExecutionKey)
		require.Equal(t, results[0].ExecutionKey, r.ExecutionKey)
	}

	// 未带 CallID 的调用不写日志，同样触发且 Replayed 为假。
	results = nil
	_, err := d.Invoke(ctx, ToolRequest{Name: "lookup", Input: &interfaces.ToolInput{Args: map[string]any{"id": "b"}}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.False(t, results[0].Replayed)
}

func TestDispatcherOnResultArgumentError(t *testing.T) {
	ctx := journalCtx(t)
	tool := NewBaseTool("op", "t", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return nil, &ArgumentError{Message: "id 不存在"}
	})
	var results []DispatchResult
	d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true, OnResult: func(_ context.Context, r DispatchResult) error {
		results = append(results, r)
		return nil
	}}, tool)
	for range 2 {
		out, err := modelCall(ctx, d, "op", callInput("bad", "c1"))
		requireArgumentResult(t, out, err, "id 不存在")
	}
	// Schema 校验失败的结果同样触发。
	out, err := modelCall(ctx, d, "op", &interfaces.ToolInput{CallID: "c2", Args: map[string]any{}})
	require.NoError(t, err)
	require.False(t, out.Success)
	require.Len(t, results, 3)
	require.Equal(t, []bool{false, true, false}, []bool{results[0].Replayed, results[1].Replayed, results[2].Replayed})
	for _, r := range results {
		require.False(t, r.Output.Success)
	}
}

func TestDispatcherOnResultErrorAborts(t *testing.T) {
	ctx := journalCtx(t)
	calls := 0
	tool := NewBaseTool("op", "t", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls++
		return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
	})
	fail := true
	var replays []bool
	d := argDispatcher(t, DispatchConfig{OnResult: func(_ context.Context, r DispatchResult) error {
		replays = append(replays, r.Replayed)
		if fail {
			return errors.New("ledger unavailable")
		}
		return nil
	}}, tool)
	var events []string
	out, err := d.invoke(ctx, ToolRequest{Name: "op", Input: callInput("a", "c1")}, func(e *core.StreamEvent) error {
		events = append(events, e.Type)
		return nil
	}, false)
	require.Nil(t, out)
	require.EqualError(t, err, "ledger unavailable")
	require.Equal(t, []string{"tool_start"}, events, "回调失败时不发 tool_end")

	// 日志已记下结果；宿主恢复后回放再次触发回调，工具不重复执行。
	fail = false
	out, err = d.Invoke(ctx, ToolRequest{Name: "op", Input: callInput("a", "c1")})
	require.NoError(t, err)
	require.Equal(t, "ok", out.Result)
	require.Equal(t, 1, calls)
	require.Equal(t, []bool{false, true}, replays)
}

func TestDispatcherOnResultPanicReported(t *testing.T) {
	tool := NewBaseTool("op", "t", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true}, nil
	})
	d := argDispatcher(t, DispatchConfig{OnResult: func(context.Context, DispatchResult) error { panic("boom") }}, tool)
	out, err := d.Invoke(context.Background(), ToolRequest{Name: "op", Input: callInput("a", "")})
	require.Nil(t, out)
	require.EqualError(t, err, `工具 "op" 的结果回调 发生 panic`)
	var panicErr *agentErrors.PanicError
	require.ErrorAs(t, err, &panicErr)
	require.Equal(t, "boom", panicErr.Value)
}

// evidenceLedger 从 ExecutionKey 派生引用号，只存在于进程内存中。宿主自行并发调用 Invoke 时回调到达顺序
// 不确定，按到达顺序编号在恢复回放后会错位；ExecutionKey 由父键和 CallID 派生、重启后不变，编号因此稳定。
type evidenceLedger struct {
	mu   sync.Mutex
	refs map[string]string
}

func (l *evidenceLedger) record(_ context.Context, r DispatchResult) error {
	sum := sha256.Sum256([]byte(r.ExecutionKey))
	ref := "E" + hex.EncodeToString(sum[:3])
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, existing := range l.refs {
		if existing == ref && key != r.ExecutionKey {
			return fmt.Errorf("引用号 %s 冲突", ref)
		}
	}
	l.refs[r.ExecutionKey] = ref
	return nil
}

// 崩溃恢复后旧调用从日志回放，并发回放的到达顺序与首次不同，账本仍重建出相同的映射，新调用不会与旧引用号冲突。
func TestDispatcherOnResultRebuildsLedgerAfterRestart(t *testing.T) {
	ctx := journalCtx(t)
	calls := map[string]int{}
	var callsMu sync.Mutex
	tool := NewBaseTool("search", "t", idSchema, func(_ context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		id := in.Args["id"].(string)
		callsMu.Lock()
		calls[id]++
		callsMu.Unlock()
		// id 越小越慢，让首次并发执行的完成顺序与发起顺序相反。
		var n int
		_, _ = fmt.Sscanf(id, "%d", &n)
		time.Sleep(time.Duration(8-n) * 3 * time.Millisecond)
		return &interfaces.ToolOutput{Success: true, Result: id}, nil
	})
	process := func() (*Dispatcher, *evidenceLedger) {
		ledger := &evidenceLedger{refs: map[string]string{}}
		return argDispatcher(t, DispatchConfig{MaxConcurrency: 8, OnResult: ledger.record}, tool), ledger
	}
	invokeAll := func(d *Dispatcher, n int) {
		var wg sync.WaitGroup
		errs := make(chan error, n)
		for i := range n {
			wg.Go(func() {
				_, e := d.Invoke(ctx, ToolRequest{Name: "search", Input: callInput(fmt.Sprint(i), fmt.Sprintf("p%d", i))})
				errs <- e
			})
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			require.NoError(t, e)
		}
	}
	d1, ledger1 := process()
	invokeAll(d1, 6)
	require.Len(t, ledger1.refs, 6)

	// 重启：账本为空，Agent 恢复时并发回放 p0..p5，再执行新的 p6、p7。
	d2, ledger2 := process()
	invokeAll(d2, 8)
	for i := range 8 {
		require.Equal(t, 1, calls[fmt.Sprint(i)], "回放不重复执行工具")
	}
	for key, ref := range ledger1.refs {
		require.Equal(t, ref, ledger2.refs[key], "恢复后同一调用得到同一引用号")
	}
	require.Len(t, ledger2.refs, 8)
	seen := map[string]bool{}
	for _, ref := range ledger2.refs {
		require.False(t, seen[ref], "引用号 %s 重复", ref)
		seen[ref] = true
	}
}

type timeoutTool struct {
	*BaseTool
	timeout time.Duration
}

func (t *timeoutTool) DispatchTimeout() time.Duration { return t.timeout }

// remainingTool 报告工具开始执行时 ctx 剩余的时间。
func remainingTool(name string, timeout time.Duration, block <-chan struct{}) *timeoutTool {
	return &timeoutTool{BaseTool: NewBaseTool(name, "t", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return nil, errors.New("no deadline")
		}
		remaining := time.Until(deadline)
		if block != nil {
			<-block
		}
		return &interfaces.ToolOutput{Success: true, Result: remaining}, nil
	}), timeout: timeout}
}

func TestDispatcherToolTimeoutOverridesDefault(t *testing.T) {
	d := argDispatcher(t, DispatchConfig{Timeout: time.Second},
		remainingTool("long", time.Hour, nil), remainingTool("short", 50*time.Millisecond, nil), remainingTool("default", 0, nil))
	remaining := func(name string) time.Duration {
		out, err := d.Invoke(context.Background(), ToolRequest{Name: name, Input: &interfaces.ToolInput{Args: map[string]any{}}})
		require.NoError(t, err)
		return out.Result.(time.Duration)
	}
	require.Greater(t, remaining("long"), time.Minute, "工具声明可放宽 Dispatcher 默认值")
	require.LessOrEqual(t, remaining("short"), 50*time.Millisecond, "工具声明也可收紧")
	got := remaining("default")
	require.LessOrEqual(t, got, time.Second)
	require.Greater(t, got, 500*time.Millisecond, "未声明时沿用 Dispatcher.Timeout")

	// 声明的超时真正生效。
	slow := &timeoutTool{BaseTool: NewBaseTool("slow", "t", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}), timeout: 20 * time.Millisecond}
	d = argDispatcher(t, DispatchConfig{Timeout: time.Hour}, slow)
	start := time.Now()
	_, err := d.Invoke(context.Background(), ToolRequest{Name: "slow", Input: &interfaces.ToolInput{Args: map[string]any{}}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 10*time.Second)

	_, err = NewDispatcher(func() *Registry {
		r := NewRegistry()
		require.NoError(t, r.Register(remainingTool("neg", -time.Second, nil)))
		return r
	}(), DispatchConfig{})
	require.ErrorContains(t, err, "timeout must not be negative")
}

// 超时从取得槽位开始计时，排队时间不消耗工具的执行预算。
func TestDispatcherTimeoutExcludesQueueing(t *testing.T) {
	release := make(chan struct{})
	d := argDispatcher(t, DispatchConfig{MaxConcurrency: 1, Timeout: 400 * time.Millisecond},
		remainingTool("hold", time.Minute, release), remainingTool("queued", 0, nil))
	held := make(chan error, 1)
	go func() {
		_, err := d.Invoke(context.Background(), ToolRequest{Name: "hold", Input: &interfaces.ToolInput{Args: map[string]any{}}})
		held <- err
	}()
	type result struct {
		out *interfaces.ToolOutput
		err error
	}
	queued := make(chan result, 1)
	go func() {
		// 确保 hold 先占住槽位。
		time.Sleep(20 * time.Millisecond)
		out, err := d.Invoke(context.Background(), ToolRequest{Name: "queued", Input: &interfaces.ToolInput{Args: map[string]any{}}})
		queued <- result{out, err}
	}()
	time.Sleep(500 * time.Millisecond)
	close(release)
	require.NoError(t, <-held)
	r := <-queued
	require.NoError(t, r.err)
	out := r.out
	require.Greater(t, out.Result.(time.Duration), 200*time.Millisecond, "排队约 480ms 后仍拿到完整的 400ms 预算")
}

func TestRunnableToolTimeout(t *testing.T) {
	cfg := runnableConfig()
	cfg.Timeout = -time.Second
	_, err := NewRunnableTool(numberRun(new(int), false), cfg)
	require.ErrorContains(t, err, "timeout")

	cfg.Timeout = time.Hour
	var remaining time.Duration
	run := core.NewStreamRunnable(core.StreamModeFinal, func(ctx context.Context, n int, emit func(core.StreamChunk[int]) error) error {
		deadline, _ := ctx.Deadline()
		remaining = time.Until(deadline)
		return emit(core.StreamChunk[int]{Done: true, Data: n})
	})
	tool, err := NewRunnableTool(run, cfg)
	require.NoError(t, err)
	require.Equal(t, time.Hour, tool.(DispatchTimeoutProvider).DispatchTimeout())
	d := argDispatcher(t, DispatchConfig{}, tool)
	_, err = d.Invoke(context.Background(), ToolRequest{Name: "increment", Input: &interfaces.ToolInput{Args: map[string]any{"n": float64(1)}}})
	require.NoError(t, err)
	require.Greater(t, remaining, time.Minute, "子 Agent 不再受 30 秒默认值限制")
}

// DeferResults 推迟 OnResult 到 deliver：调用方可按自己的顺序触发；失败的调用没有回调；工具内部的嵌套调用不受影响。
func TestDispatcherDeferResults(t *testing.T) {
	var got []string
	record := func(_ context.Context, r DispatchResult) error {
		got = append(got, r.Name+":"+r.Args["id"].(string))
		return nil
	}
	inner := argDispatcher(t, DispatchConfig{OnResult: record}, NewBaseTool("inner", "t", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
	}))
	outer := argDispatcher(t, DispatchConfig{OnResult: record}, NewBaseTool("outer", "t", idSchema, func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		if in.Args["id"] == "bad" {
			return nil, errors.New("失败")
		}
		return inner.Invoke(ctx, ToolRequest{Name: "inner", Input: callInput(in.Args["id"].(string), "")})
	}))
	var delivers []func() error
	for _, id := range []string{"a", "bad", "b"} {
		ctx, deliver := DeferResults(context.Background())
		_, err := outer.Invoke(ctx, ToolRequest{Name: "outer", Input: callInput(id, "")})
		if id == "bad" {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
		delivers = append(delivers, deliver)
	}
	require.Equal(t, []string{"inner:a", "inner:b"}, got, "嵌套调用即时回调，外层回调尚未触发")
	for i := len(delivers) - 1; i >= 0; i-- {
		require.NoError(t, delivers[i]())
	}
	require.Equal(t, []string{"inner:a", "inner:b", "outer:b", "outer:a"}, got, "外层回调按 deliver 顺序触发，失败调用没有回调")
}

// 同一 ctx 下多次调用 Dispatcher：deliver 按调用成功的先后触发全部回调，再次 deliver 不重复触发。
func TestDispatcherDeferResultsSameContextSequential(t *testing.T) {
	var got []string
	d := argDispatcher(t, DispatchConfig{OnResult: func(_ context.Context, r DispatchResult) error {
		got = append(got, r.Args["id"].(string))
		if r.Args["id"] == "a" {
			return errors.New("回调失败")
		}
		return nil
	}}, NewBaseTool("t", "t", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
	}))
	ctx, deliver := DeferResults(context.Background())
	for _, id := range []string{"a", "b"} {
		_, err := d.Invoke(ctx, ToolRequest{Name: "t", Input: callInput(id, "")})
		require.NoError(t, err)
	}
	require.Empty(t, got, "deliver 前不触发回调")
	require.ErrorContains(t, deliver(), "回调失败", "返回最靠前的错误")
	require.Equal(t, []string{"a", "b"}, got, "前一个回调失败不跳过后续回调，按追加顺序触发")
	require.NoError(t, deliver())
	require.Equal(t, []string{"a", "b"}, got, "回调只触发一次")
}

// 同一 ctx 下并发调用 Dispatcher：-race 下无竞争，每个成功调用恰好一次回调。
func TestDispatcherDeferResultsSameContextConcurrent(t *testing.T) {
	const n = 32
	var mu sync.Mutex
	got := map[string]int{}
	d := argDispatcher(t, DispatchConfig{OnResult: func(_ context.Context, r DispatchResult) error {
		mu.Lock()
		defer mu.Unlock()
		got[r.Args["id"].(string)]++
		return nil
	}}, NewBaseTool("t", "t", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
	}))
	ctx, deliver := DeferResults(context.Background())
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := d.Invoke(ctx, ToolRequest{Name: "t", Input: callInput(fmt.Sprintf("id-%d", i), "")})
			require.NoError(t, err)
		}()
	}
	wg.Wait()
	require.NoError(t, deliver())
	require.Len(t, got, n)
	for id, c := range got {
		require.Equal(t, 1, c, id)
	}
}

// AsRunnable 的事件随 runnable 的流结束，DeferResults 对它不生效：结果回调与 tool_end 在返回前即时交付。
func TestDispatcherDeferResultsRunnableDeliversImmediately(t *testing.T) {
	var got []string
	d := argDispatcher(t, DispatchConfig{OnResult: func(_ context.Context, r DispatchResult) error {
		got = append(got, r.Args["id"].(string))
		return nil
	}}, NewBaseTool("t", "t", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
	}))
	ctx, deliver := DeferResults(context.Background())
	_, err := d.AsRunnable().Invoke(ctx, ToolRequest{Name: "t", Input: callInput("a", "")})
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, got, "Invoke 返回前已触发回调")
	require.NoError(t, deliver(), "没有推迟的回调，deliver 不会向已结束的流发事件")

	ctx, deliver = DeferResults(context.Background())
	chunks, err := d.AsRunnable().Stream(ctx, ToolRequest{Name: "t", Input: callInput("b", "")})
	require.NoError(t, err)
	var events []string
	for chunk := range chunks {
		require.NoError(t, chunk.Error)
		if chunk.Event != nil {
			events = append(events, chunk.Event.Type)
		}
	}
	require.Equal(t, []string{"tool_start", "tool_end"}, events, "tool_end 仍在流内发出")
	require.NoError(t, deliver())
	require.Equal(t, []string{"a", "b"}, got)
}

type observeCollector struct {
	mu     sync.Mutex
	events []string
}

func (c *observeCollector) Observe(_ context.Context, e observe.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e.Type)
}

func (c *observeCollector) types() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.events)
}

// 推迟的调用由 deliver 结束观测：回调失败时观测以 tool_error 结束，成功时以 tool_end 结束。
func TestDispatcherDeferResultsObservationEndsAtDeliver(t *testing.T) {
	d := argDispatcher(t, DispatchConfig{OnResult: func(_ context.Context, r DispatchResult) error {
		if r.Args["id"] == "bad" {
			return errors.New("ledger unavailable")
		}
		return nil
	}}, NewBaseTool("t", "t", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
	}))
	for _, tc := range []struct{ id, end string }{{"bad", "tool_error"}, {"good", "tool_end"}} {
		sink := &observeCollector{}
		ctx, deliver := DeferResults(observe.WithObserver(context.Background(), sink))
		var events []string
		_, err := d.invoke(ctx, ToolRequest{Name: "t", Input: callInput(tc.id, "")}, func(e *core.StreamEvent) error {
			events = append(events, e.Type)
			return nil
		}, false)
		require.NoError(t, err)
		require.Equal(t, []string{"tool_start"}, sink.types(), "%s：deliver 前观测尚未结束", tc.id)
		err = deliver()
		require.Equal(t, []string{"tool_start", tc.end}, sink.types(), tc.id)
		if tc.end == "tool_error" {
			require.EqualError(t, err, "ledger unavailable")
			require.Equal(t, []string{"tool_start"}, events, "回调失败时不发 tool_end")
		} else {
			require.NoError(t, err)
			require.Equal(t, []string{"tool_start", "tool_end"}, events)
		}
	}
}

// 未 deliver 的推迟调用在 DeferResults 的 ctx 结束时放弃交付：观测以 ctx 的错误结束，回调不再触发，deliver 报告该错误。
func TestDispatcherDeferResultsAbandonedOnCancel(t *testing.T) {
	fired := 0
	d := argDispatcher(t, DispatchConfig{OnResult: func(context.Context, DispatchResult) error {
		fired++
		return nil
	}}, NewBaseTool("t", "t", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
	}))
	sink := &observeCollector{}
	parent, cancel := context.WithCancel(observe.WithObserver(context.Background(), sink))
	ctx, deliver := DeferResults(parent)
	_, err := d.Invoke(ctx, ToolRequest{Name: "t", Input: callInput("a", "")})
	require.NoError(t, err)
	cancel()
	require.Eventually(t, func() bool { return len(sink.types()) == 2 }, time.Second, time.Millisecond)
	require.Equal(t, []string{"tool_start", "tool_error"}, sink.types())
	require.ErrorIs(t, deliver(), context.Canceled)
	require.Zero(t, fired, "放弃交付的调用不触发回调")
	require.NoError(t, deliver(), "已报告的放弃不重复报告")
}
