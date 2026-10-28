package opscase

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent/examples/orchestration/internal/livecase"
	"github.com/kart-io/goagent/examples/orchestration/internal/livecase/livecasetest"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// ledgerWith 让 result 经 Dispatcher 执行一次并登记到账本（非持久调用，引用号为 E1）。
func ledgerWith(t *testing.T, result map[string]any) *Ledger {
	t.Helper()
	l := NewLedger()
	ts, err := Dispatch([]interfaces.Tool{l.Wrap(Tool("logs", "t", `{"type":"object"}`, func(context.Context, map[string]any) (any, error) { return result, nil }))}, tools.DispatchConfig{OnResult: l.OnResult})
	require.NoError(t, err)
	_, err = ts[0].Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{}})
	require.NoError(t, err)
	return l
}

func TestLedgerWrap(t *testing.T) {
	l := NewLedger()
	ts, err := Dispatch([]interfaces.Tool{l.Wrap(Tool("logs", "t", `{"type":"object"}`, func(context.Context, map[string]any) (any, error) {
		return map[string]any{"ref": "X9", "line": "200 -> 50"}, nil
	}))}, tools.DispatchConfig{OnResult: l.OnResult})
	require.NoError(t, err)
	out, err := ts[0].Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{}})
	require.NoError(t, err)
	require.Equal(t, `{"line":"200 -> 50","ref":"E1"}`, out.Result, "不转义 >；工具自带的 ref 不能改掉账本编号")
	require.Nil(t, l.Verify([]Citation{{Ref: "E1", Excerpt: "200 -> 50"}}))
	require.Equal(t, []string{"200 -> 50"}, l.Values())
	require.Equal(t, [2]int{1, 1}, [2]int{l.Len(), l.Runs()})
}

// 未经 OnResult 登记的结果不在账本里：引用一律不存在，失败是关闭的。
func TestLedgerRequiresOnResult(t *testing.T) {
	l := NewLedger()
	_, err := l.Wrap(Tool("logs", "t", `{"type":"object"}`, func(context.Context, map[string]any) (any, error) { return map[string]any{"line": "x"}, nil })).
		Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{}})
	require.NoError(t, err)
	require.Zero(t, l.Len())
	require.Contains(t, l.Verify([]Citation{{Ref: "E1", Excerpt: "x"}})[0], "不存在")
}

// 结构体按字段顺序编码；摘录按模型看到的顺序跨字段复制时也要通过。
func TestLedgerVerifyStructOrder(t *testing.T) {
	type item struct {
		Key   string `json:"key"`
		Base  string `json:"base"`
		Delta string `json:"delta"`
	}
	l := ledgerWith(t, map[string]any{"ranked": []item{{"ai", "600.00", "525.00"}}})
	require.Nil(t, l.Verify([]Citation{{Ref: "E1", Excerpt: `{"key":"ai","base":"600.00","delta":"525.00"}`}}))
	require.Nil(t, l.Verify([]Citation{{Ref: "E1", Excerpt: `key: ai, base: 600.00`}}))
	require.Nil(t, l.Verify([]Citation{{Ref: "E1", Excerpt: `{"ranked":[{"key":"ai","base":"600.00","delta":"525.00"}],"ref":"E1"}`}}), "整段照抄连 ref 一起也算原样")
	require.Len(t, l.Verify([]Citation{{Ref: "E1", Excerpt: `"base":"600.00","key":"ai"`}}), 1, "顺序不符不是原样片段")
}

func TestLedgerVerify(t *testing.T) {
	l := ledgerWith(t, map[string]any{
		"lines": []string{"10:05:12 ERROR pool exhausted (max_open=50 in_use=50)", "line one\nline two"},
		"pct":   "87.5%",
		"cat":   "ai",
		"rows":  2400000,
		"delta": "-100.00",
		"svc":   "checkout-api",
		"pool":  "31/200",
	})
	for _, ok := range []string{
		"pool exhausted (max_open=50  in_use=50)",
		`"10:05:12 ERROR pool exhausted`,
		"(max_open=50 in_use=50)", // 首尾是标点，但两侧不连着词
		"max_open=50",
		"87.5%",
		`pct: 87.5%`,
		`"pct":"87.5%"`,
		"2400000",
		"ai",
		"cat: ai",
		"-100.00",
		"delta: -100.00",
		"checkout-api",
		"31/200",
		`"pct":"87.5%","pool":"31/200"`, // 跨字段的原样片段
		`line one\nline two`,            // 照抄 JSON 转义
		"line one line two",
	} {
		require.Nil(t, l.Verify([]Citation{{Ref: "E1", Excerpt: ok}}), ok)
	}
	for _, bad := range []string{
		"in_use=5",     // 截断数字
		"max_open=5",   // 截断数字
		"7.5%",         // 截断小数
		"5%",           // 截断小数
		"87",           // 截断小数
		"100.00",       // 丢掉负号，方向反转
		"delta: 100",   // 不在结果中
		"-api",         // 截断单词：标点开头但左侧连着词
		"checkout-",    // 截断单词：标点结尾但右侧连着词
		"/200",         // 截断数字：标点开头但左侧连着数字
		"=50",          // 截断：标点开头但左侧连着词
		"lines",        // 只有字段名
		"pct:",         // 只有字段名
		`"rows":24`,    // 截断数字
		"ref E1",       // 只有引用号
		"10 05 pool",   // 改写冒号
		"pool drained", // 编造
		"ok",           // 过短，且不是完整字段值
		"87.",          // 过短
	} {
		require.Len(t, l.Verify([]Citation{{Ref: "E1", Excerpt: bad}}), 1, bad)
	}
	problems := l.Verify([]Citation{{Ref: "E2", Excerpt: "pool exhausted"}, {Ref: "E1", Excerpt: "pool drained"}})
	require.Contains(t, problems[0], "可用引用：E1")
	require.Contains(t, problems[1], `"pool drained"`, "提示里带上摘录，便于模型修正")
}

// Restore 补登的条目只接受已核对的摘录及其中首尾完整的片段，不能跨两条摘录拼接。
func TestLedgerRestore(t *testing.T) {
	l := NewLedger()
	l.Restore("Eabc123", []string{"14:10 820ms", "child span db.lock_wait p99 760ms"})
	for _, ok := range []string{"14:10 820ms", "db.lock_wait p99 760ms"} {
		require.Nil(t, l.Verify([]Citation{{Ref: "Eabc123", Excerpt: ok}}), ok)
	}
	for _, bad := range []string{"820ms child span", "14:10 820m", "p99 1.2s"} {
		require.Len(t, l.Verify([]Citation{{Ref: "Eabc123", Excerpt: bad}}), 1, bad)
	}
}

type conclusion struct {
	Cause    string     `json:"cause"`
	Evidence []Citation `json:"evidence"`
}

func answer(c conclusion) string { return "结论：" + c.Cause }

func TestSubmitter(t *testing.T) {
	l := ledgerWith(t, map[string]any{"line": "pool exhausted (max_open=50)"})
	s := &Submitter[conclusion]{Ledger: l, MaxRepairs: 1, Evidence: func(c conclusion) []Citation { return c.Evidence }, Answer: answer,
		Check: func(c conclusion) []string {
			if c.Cause == "guess" {
				return []string{"不允许猜测"}
			}
			return nil
		}}
	ts, err := Dispatch([]interfaces.Tool{s.Tool("t", `{"type":"object"}`)}, tools.DispatchConfig{OnResult: s.OnResult})
	require.NoError(t, err)
	submit := func(cause, excerpt string) (*interfaces.ToolOutput, error) {
		return ts[0].Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"cause": cause, "evidence": []any{map[string]any{"ref": "E1", "excerpt": excerpt}}}})
	}
	out, err := submit("guess", "pool exhausted")
	require.NoError(t, err)
	message, ok := tools.ArgumentErrorResult(out)
	require.True(t, ok, "核验不通过作为参数错误交还模型")
	require.Contains(t, message, "不允许猜测")
	_, err = s.Accepted()
	require.ErrorContains(t, err, "没有提交通过核验的结论")

	out, err = submit("config", "pool exhausted")
	require.NoError(t, err)
	require.Equal(t, "结论：config", out.Result, "通过时返回渲染好的最终回答")
	accepted, submissions, repairs := s.Stats()
	require.Equal(t, "config", accepted.Cause)
	require.Equal(t, [2]int{2, 1}, [2]int{submissions, repairs})
	require.Equal(t, []string{"不允许猜测"}, s.Rejections())

	limited := &Submitter[conclusion]{Ledger: l, MaxRepairs: 0, Evidence: func(c conclusion) []Citation { return c.Evidence }, Answer: answer}
	_, err = limited.Tool("t", `{"type":"object"}`).Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"cause": "x", "evidence": []any{map[string]any{"ref": "E1", "excerpt": "drained"}}}})
	require.ErrorContains(t, err, "结论核验 1 次仍未通过")
}

// agentCase 是一个最小的持久 Agent：同一轮并发调用两个数据工具，下一轮提交结论并直接结束。
type agentCase struct {
	ledger   *Ledger
	sub      *Submitter[conclusion]
	calls    atomic.Int64
	mu       sync.Mutex
	seen     []string // 提交回合看到的工具结果
	parallel int      // 传给 WithParallelToolCalls
	barrier  bool     // 两个数据工具互相等待对方开始，证明确实并发执行
	onResult func(*Ledger)
}

func (c *agentCase) respond(r *llm.CompletionRequest) llm.StreamChunk {
	steps := 0
	var results []string
	for _, m := range r.Messages {
		switch m.Role {
		case "assistant":
			steps++
		case "tool":
			results = append(results, m.Content)
		}
	}
	if steps == 0 {
		return llm.StreamChunk{ToolCalls: []llm.ToolCall{toolCall("c1", "a_query", nil), toolCall("c2", "b_query", nil)}}
	}
	c.mu.Lock()
	c.seen = results
	c.mu.Unlock()
	return BindRefs(r, llm.StreamChunk{ToolCalls: []llm.ToolCall{toolCall("c3", SubmitTool, map[string]any{"cause": "pool", "evidence": []any{
		map[string]any{"ref": "@c1", "excerpt": "alpha 1 <ok>"}, map[string]any{"ref": "@c2", "excerpt": "-2.50"}}})}})
}

func toolCall(id, name string, args map[string]any) llm.ToolCall {
	c := llm.ToolCall{ID: id, Type: "function"}
	c.Function.Name = name
	b, _ := json.Marshal(args)
	if args == nil {
		b = []byte("{}")
	}
	c.Function.Arguments = string(b)
	return c
}

func (c *agentCase) run(t *testing.T, dir string) (string, error) {
	t.Helper()
	store, err := durable.NewFileStore(dir)
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "case/agent"), store)
	c.ledger = NewLedger()
	c.sub = &Submitter[conclusion]{Ledger: c.ledger, MaxRepairs: 1, Evidence: func(x conclusion) []Citation { return x.Evidence }, Answer: answer}
	started := make(chan struct{}, 2)
	data := func(name string, result map[string]any) interfaces.Tool {
		return c.ledger.Wrap(Tool(name, "t", `{"type":"object"}`, func(ctx context.Context, _ map[string]any) (any, error) {
			if c.barrier {
				started <- struct{}{}
				deadline := time.After(5 * time.Second)
				for len(started) < 2 {
					select {
					case <-deadline:
						return nil, context.DeadlineExceeded
					case <-time.After(time.Millisecond):
					}
				}
			}
			return result, nil
		}))
	}
	onResult := c.sub.OnResult
	if c.onResult != nil {
		onResult = func(ctx context.Context, r tools.DispatchResult) error {
			if err := c.sub.OnResult(ctx, r); err != nil {
				return err
			}
			c.onResult(c.ledger)
			return nil
		}
	}
	ts, err := Dispatch([]interfaces.Tool{
		data("a_query", map[string]any{"line": "alpha 1 <ok>"}),
		data("b_query", map[string]any{"delta": "-2.50"}),
		c.sub.Tool("t", `{"type":"object"}`),
	}, tools.DispatchConfig{OnResult: onResult})
	require.NoError(t, err)
	client := livemodel.Wrap(&demomodel.Model{Calls: &c.calls, Respond: c.respond}, livemodel.Config{Provider: "scripted", Model: "v1"})
	agent, err := builder.NewSimpleBuilder(client).WithDurableScope("case").WithTools(ts...).WithReturnDirectTools(SubmitTool).
		WithParallelToolCalls(c.parallel).WithMaxIterations(3).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(ctx, "查")
	if err != nil {
		return "", err
	}
	return out.Result.(string), nil
}

// 同一轮并发执行时，引用号由执行键派生：与执行先后无关、互不相同，换目录重跑或改为顺序执行都得到同样的编号。
func TestLedgerRefsUnderConcurrency(t *testing.T) {
	concurrent := &agentCase{barrier: true, parallel: 2}
	got, err := concurrent.run(t, t.TempDir())
	require.NoError(t, err, "两个数据工具必须同时在执行，否则屏障超时")
	require.Equal(t, "结论：pool", got, "submit_conclusion 直接给出最终回答")
	require.Equal(t, int64(2), concurrent.calls.Load(), "直接返回省去总结请求")
	refs := concurrent.ledger.Refs()
	require.Len(t, refs, 2)
	require.Regexp(t, `^E[0-9a-f]{6}$`, refs[0])
	require.NotEqual(t, refs[0], refs[1])

	again := &agentCase{barrier: true, parallel: 2}
	_, err = again.run(t, t.TempDir())
	require.NoError(t, err)
	sequential := &agentCase{parallel: 1}
	_, err = sequential.run(t, t.TempDir())
	require.NoError(t, err)
	require.Equal(t, concurrent.seen, again.seen, "模型看到的工具结果（含引用号）在每次运行中相同")
	require.Equal(t, concurrent.seen, sequential.seen, "引用号与执行顺序无关")
}

// 调查中途崩溃（数据工具已写入日志、提交回合尚未请求）后恢复：已回放的结果经 OnResult 重建进账本，
// 恢复后新提交的结论能引用它们并通过核验。
func TestLedgerRebuiltAfterCrash(t *testing.T) {
	if dir := os.Getenv("OPSCASE_CRASH_DIR"); dir != "" {
		c := &agentCase{onResult: func(l *Ledger) {
			if l.Len() == 2 {
				os.Exit(livecase.ChildExit) // 两个数据工具都已落盘
			}
		}}
		_, err := c.run(t, dir)
		t.Fatalf("应在数据工具落盘后退出：%v", err)
	}
	dir := t.TempDir()
	livecasetest.Child(t, "TestLedgerRebuiltAfterCrash", "OPSCASE_CRASH_DIR="+dir)
	c := &agentCase{}
	got, err := c.run(t, dir)
	require.NoError(t, err)
	require.Equal(t, "结论：pool", got)
	require.Equal(t, int64(1), c.calls.Load(), "只请求提交回合")
	require.Equal(t, [2]int{2, 0}, [2]int{c.ledger.Len(), c.ledger.Runs()}, "账本由回放重建，数据工具没有重新执行")
	accepted, submissions, _ := c.sub.Stats()
	require.Equal(t, "pool", accepted.Cause)
	require.Equal(t, 1, submissions)
}

// Agent 结束后、宿主处理前崩溃：宿主侧没有任何待落盘的状态，同目录重跑只回放，结论从提交调用的参数恢复。
func TestSubmitterRecoversAfterAgentEnd(t *testing.T) {
	dir := t.TempDir()
	first := &agentCase{}
	answer, err := first.run(t, dir)
	require.NoError(t, err)
	// 首次运行的 Submitter 与 Ledger 就此丢弃，等同于进程在宿主读取结论前退出。

	again := &agentCase{}
	replayed, err := again.run(t, dir)
	require.NoError(t, err)
	require.Zero(t, again.calls.Load())
	require.Equal(t, answer, replayed)
	accepted, err := again.sub.Accepted()
	require.NoError(t, err)
	require.Equal(t, "pool", accepted.Cause)
	_, submissions, _ := again.sub.Stats()
	require.Zero(t, submissions+again.ledger.Runs(), "回放不执行工具，也不重新核验")
	require.Equal(t, 2, again.ledger.Len())
	require.True(t, strings.HasPrefix(replayed, "结论："))
}

func TestStrictJSON(t *testing.T) {
	type item struct {
		ID string `json:"id"`
	}
	type v struct {
		Decision string `json:"decision"`
		Items    []item `json:"items"`
	}
	var got v
	require.NoError(t, StrictJSON("```json\n{\"decision\":\"approve\",\"items\":[{\"id\":\"a\"}]}\n```", &got))
	require.Equal(t, "approve", got.Decision)
	for _, bad := range []string{
		`{"decision":"approve","extra":1}`,
		`{"decision":"approve"} 说明`,
		`{"decision":"approve"}{}`,
		`好的：{"decision":"approve"}`,
		``,
		`null`,
		`{"Decision":"approve"}`,
		`{"decision":"reject","decision":"approve"}`,
		`{"decision":"approve","items":[{"ID":"a"}]}`,
		`{"decision":"approve","items":[{"id":"a","id":"b"}]}`,
	} {
		require.Error(t, StrictJSON(bad, &got), bad)
	}
}
