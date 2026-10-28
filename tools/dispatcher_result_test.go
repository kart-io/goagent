package tools

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
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

// evidenceLedger 按结果到达顺序分配引用号，只存在于进程内存中。
type evidenceLedger struct {
	mu   sync.Mutex
	refs map[string]string
}

func (l *evidenceLedger) record(_ context.Context, r DispatchResult) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.refs[r.ExecutionKey]; !ok {
		l.refs[r.ExecutionKey] = fmt.Sprintf("E%d", len(l.refs)+1)
	}
	return nil
}

// 崩溃恢复后旧调用从日志回放，回调仍按顺序重建账本，新调用不会与旧引用号冲突。
func TestDispatcherOnResultRebuildsLedgerAfterRestart(t *testing.T) {
	ctx := journalCtx(t)
	calls := map[string]int{}
	var callsMu sync.Mutex
	tool := NewBaseTool("search", "t", idSchema, func(_ context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		callsMu.Lock()
		calls[in.Args["id"].(string)]++
		callsMu.Unlock()
		return &interfaces.ToolOutput{Success: true, Result: in.Args["id"]}, nil
	})
	process := func() (*Dispatcher, *evidenceLedger) {
		ledger := &evidenceLedger{refs: map[string]string{}}
		return argDispatcher(t, DispatchConfig{OnResult: ledger.record}, tool), ledger
	}
	d1, ledger1 := process()
	_, err := d1.Invoke(ctx, ToolRequest{Name: "search", Input: callInput("a", "c1")})
	require.NoError(t, err)
	require.Len(t, ledger1.refs, 1)

	// 重启：账本为空，Agent 恢复时先回放 c1，再执行新的 c2。
	d2, ledger2 := process()
	for _, call := range [][2]string{{"a", "c1"}, {"b", "c2"}} {
		_, err = d2.Invoke(ctx, ToolRequest{Name: "search", Input: callInput(call[0], call[1])})
		require.NoError(t, err)
	}
	require.Equal(t, map[string]int{"a": 1, "b": 1}, calls)
	require.Equal(t, ledger1.refs, map[string]string{keyOf(ledger2, "E1"): "E1"})
	require.ElementsMatch(t, []string{"E1", "E2"}, values(ledger2.refs))

	// 并发调用会并发触发回调，账本加锁后引用号仍唯一。
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := range 8 {
		wg.Go(func() {
			_, e := d2.Invoke(ctx, ToolRequest{Name: "search", Input: callInput(fmt.Sprint(i), fmt.Sprintf("p%d", i))})
			errs <- e
		})
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	require.Len(t, ledger2.refs, 10)
	seen := map[string]bool{}
	for _, ref := range ledger2.refs {
		require.False(t, seen[ref], "引用号 %s 重复", ref)
		seen[ref] = true
	}
}

func keyOf(l *evidenceLedger, ref string) string {
	for k, v := range l.refs {
		if v == ref {
			return k
		}
	}
	return ""
}

func values(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
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
