package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/internal/livemodel/livemodeltest"
	"github.com/kart-io/goagent/examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent/examples/orchestration/internal/livecase"
	"github.com/kart-io/goagent/examples/orchestration/internal/livecase/livecasetest"
	"github.com/kart-io/goagent/examples/orchestration/ops/internal/opscase"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func call(id, name string, args any) llm.ToolCall {
	c := llm.ToolCall{ID: id, Type: "function"}
	c.Function.Name = name
	b, _ := json.Marshal(args)
	c.Function.Arguments = string(b)
	return c
}

func turn(calls ...llm.ToolCall) llm.StreamChunk { return llm.StreamChunk{ToolCalls: calls} }

func submit(id string, evidence ...opscase.Citation) llm.StreamChunk {
	return submitPair(id, "inventory-svc", "reserve", evidence...)
}

func submitPair(id, downstream, operation string, evidence ...opscase.Citation) llm.StreamChunk {
	return turn(call(id, "submit_conclusion", map[string]any{
		"downstream": downstream, "operation": operation, "summary": "inventory-svc.reserve 变慢拖累下单", "evidence": evidence}))
}

var (
	// 三个方向在同一轮派出，并发执行；引用号由执行键派生，与完成顺序无关。
	delegateAll = turn(
		call("d1", "delegate_lookup", map[string]string{"focus": "metrics", "question": "哪个下游调用耗时上升？"}),
		call("d2", "delegate_lookup", map[string]string{"focus": "logs", "question": "下游调用有没有超时重试？"}),
		call("d3", "delegate_lookup", map[string]string{"focus": "traces", "question": "慢链路集中在哪个下游操作？"}))
	// "@d1" 指 d1 子调查报告里第一条证据的 ref，见 opscase.BindRefs。
	latency   = opscase.Citation{Ref: "@d1", Excerpt: "14:10 820ms"}
	lockWait  = opscase.Citation{Ref: "@d3", Excerpt: "child span db.lock_wait p99 760ms"}
	missing   = opscase.Citation{Ref: "E9", Excerpt: "14:10 820ms"}
	fabricate = opscase.Citation{Ref: "@d3", Excerpt: "reserve p99 1.2s"}
	// final 是通过核验的提交直接给出的最终回答。
	final = "结论：inventory-svc\n操作：reserve\ninventory-svc.reserve 变慢拖累下单"
	// goodArgs 是每个方向的子调查按合成数据应发出的查询。
	goodArgs = map[string]map[string]string{
		"metrics": {"downstream": "inventory-svc", "metric": "latency_p99"},
		"logs":    {"downstream": "inventory-svc", "level": "ERROR"},
		"traces":  {"downstream": "inventory-svc", "operation": "reserve"},
	}
)

var focusOf = regexp.MustCompile(`只负责 (\w+) 方向`)

var refOf = regexp.MustCompile(`"ref":"(E[0-9a-f]+)"`)

// excerpts 是各方向子调查从自己结果中原样摘出的片段。
var excerpts = map[string]string{"metrics": "14:10 820ms", "logs": "deadline exceeded", "traces": "child span db.lock_wait p99 760ms"}

// scripted 按系统提示区分主 Agent 与子调查，每个请求只按本会话自己的消息应答，与并发请求的到达顺序无关：
// 主 Agent 按回合依次给出 parent，子调查先查一次自己的工具再给摘要。
// mode 让某个子调查表现异常；parentSeen 是主 Agent 最近一次请求中的全部工具结果。
type scripted struct {
	parent     []llm.StreamChunk
	mode       string // unoffered：metrics 子调查调用未授予的 logs_query；nest：再委派；mismatch：traces 先传错操作
	barrier    int    // 大于 0 时，子调查的首个请求要等到这么多个子调查都已发出请求才应答
	mu         sync.Mutex
	parentSeen []string
	childFixes int // 子调查看到 invalid_arguments 的次数
	arrived    int
	ready      chan struct{}
	timedOut   bool
}

// await 是子调查之间的屏障：子调查若被逐个执行，第一个会一直等不到其他子调查而超时。
func (s *scripted) await() {
	s.mu.Lock()
	if s.ready == nil {
		s.ready = make(chan struct{})
	}
	if s.arrived++; s.arrived == s.barrier {
		close(s.ready)
	}
	ready := s.ready
	s.mu.Unlock()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		s.mu.Lock()
		s.timedOut = true
		s.mu.Unlock()
	}
}

func (s *scripted) client() *livemodel.Client {
	var calls atomic.Int64
	return livemodel.Wrap(&demomodel.Model{Calls: &calls, Respond: s.respond}, livemodel.Config{Provider: "scripted", Model: "v1"})
}

func (s *scripted) respond(r *llm.CompletionRequest) llm.StreamChunk {
	var focus string
	var results []string
	steps := 0
	for _, m := range r.Messages {
		switch m.Role {
		case "system":
			if f := focusOf.FindStringSubmatch(m.Content); f != nil {
				focus = f[1]
			}
		case "assistant":
			steps++
		case "tool":
			results = append(results, m.Content)
		}
	}
	if focus != "" && len(results) == 0 && s.barrier > 0 {
		s.await()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if focus == "" {
		s.parentSeen = results
		return opscase.BindRefs(r, s.parent[min(steps, len(s.parent)-1)])
	}
	own := focusTool[focus]
	switch {
	case len(results) == 0 && s.mode == "unoffered" && focus == "metrics":
		return turn(call("c1", "logs_query", goodArgs["logs"]))
	case len(results) == 0 && s.mode == "nest" && focus == "metrics":
		return turn(call("c1", "delegate_lookup", map[string]string{"focus": "logs", "question": "顺便看看日志"}))
	case len(results) == 0 && s.mode == "mismatch" && focus == "traces":
		return turn(call("c1", own, map[string]string{"downstream": "payment-svc", "operation": "reserve"}))
	case len(results) == 0:
		return turn(call("c1", own, goodArgs[focus]))
	case strings.Contains(results[len(results)-1], `"invalid_arguments"`):
		s.childFixes++
		return turn(call("c2", own, goodArgs[focus]))
	}
	// 每个子调查给出一条原样摘录、一条截断的摘录和一条编造的摘录；宿主只把第一条交给主 Agent。
	ref := refOf.FindStringSubmatch(results[len(results)-1])[1]
	good := excerpts[focus]
	return llm.StreamChunk{Content: focus + " 方向：inventory-svc 异常。\n证据：" + ref + "｜" + good +
		"\n证据：" + ref + "｜" + good[:len(good)-1] + "\n证据：" + ref + "｜inventory-svc 完全不可用"}
}

func (s *scripted) parentInvalid() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.parentSeen {
		if strings.Contains(c, `"invalid_arguments"`) {
			n++
		}
	}
	return n
}

func requireScoped(t *testing.T, out Result) {
	t.Helper()
	require.Equal(t, out.LookupsCreated, out.LookupsDone, "子会话创建数等于清理数")
	for _, l := range out.Lookups {
		for _, name := range l.Calls {
			require.Equal(t, focusTool[l.Focus], name, "子会话只调用其方向被授予的工具")
		}
	}
}

// refsOf 返回各方向子会话的引用号，并检查每个子会话恰好产生一个、彼此不同。
func refsOf(t *testing.T, out Result) []string {
	t.Helper()
	var refs []string
	for _, l := range out.Lookups {
		require.Len(t, l.Refs, 1, l.Focus)
		require.Regexp(t, `^E[0-9a-f]{6}$`, l.Refs[0])
		require.NotContains(t, refs, l.Refs[0])
		refs = append(refs, l.Refs[0])
	}
	return refs
}

func TestScriptedScopedLookup(t *testing.T) {
	s := &scripted{parent: []llm.StreamChunk{delegateAll, submit("p2", latency, lockWait)}, barrier: 3}
	client, dir := s.client(), t.TempDir()
	first, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.False(t, s.timedOut, "三个子调查必须同时在执行")
	require.Equal(t, 3, first.PeakLookups, "同一轮的 delegate_lookup 并发执行，峰值受 MaxParallel 限制")
	require.Equal(t, ExpectedDownstream, first.Decision)
	require.Equal(t, final, first.Answer, "submit_conclusion 直接给出最终回答")
	require.NotNil(t, first.Accepted, "结论引用子调查内部的 ref，经共享账本核验通过")
	require.Equal(t, [2]int{1, 0}, [2]int{first.Submissions, first.Repairs})
	refs := refsOf(t, first)
	for i, l := range first.Lookups {
		require.Equal(t, Lookup{Focus: focuses[i], Calls: []string{focusTool[focuses[i]]}, Refs: refs[i : i+1]}, l, "按方向排列，与完成顺序无关")
	}
	requireScoped(t, first)
	require.Equal(t, 3, first.LookupsCreated)
	seen := strings.Join(s.parentSeen, "\n")
	require.Contains(t, seen, fmt.Sprintf(`"refs":[%q]`, refs[2]), "主 Agent 只拿到摘要与引用号")
	require.Contains(t, seen, fmt.Sprintf(`"evidence":[{"excerpt":"14:10 820ms","ref":%q}]`, refs[0]), "截断的摘录被宿主丢弃")
	require.Contains(t, seen, fmt.Sprintf(`"evidence":[{"excerpt":"child span db.lock_wait p99 760ms","ref":%q}]`, refs[2]), "主 Agent 只拿到核对通过的摘录")
	require.NotContains(t, seen, "deadline exceede\"", "截断的摘录被宿主丢弃")
	require.NotContains(t, seen, "完全不可用", "编造的摘录被宿主丢弃")
	require.NotContains(t, seen, "in 92% of slow order traces", "子调查的原始结果不进入主上下文")
	require.Equal(t, int64(8), first.NewModelCalls, "主 Agent 2 次 + 3 个子调查各 2 次")
	require.Equal(t, 8, first.Budget.ReservedRequests, "父子请求都计入根预算")

	// 换目录重跑：并发完成顺序不同，模型看到的引用号仍相同。
	other := &scripted{parent: s.parent, barrier: 3}
	second, err := Run(context.Background(), other.client(), t.TempDir())
	require.NoError(t, err)
	require.Equal(t, refs, refsOf(t, second))
	require.Equal(t, s.parentSeen, other.parentSeen)

	again, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.Zero(t, again.NewModelCalls)
	require.Zero(t, again.LookupsCreated+again.DataToolRuns+again.Submissions, "回放不新建子会话，也不执行工具")
	require.Equal(t, first.Answer, again.Answer)
	require.Equal(t, first.Budget, again.Budget)
	require.Equal(t, first.Accepted, again.Accepted, "回放时结论由提交调用恢复")
	require.Equal(t, first.Decision, again.Decision)
}

// 宿主只发布通过核验的结论：模型不提交就直接作答报错。
func TestScriptedAnswerBoundToConclusion(t *testing.T) {
	skip := &scripted{parent: []llm.StreamChunk{delegateAll, {Content: "结论：payment-svc\n猜的。"}}}
	out, err := Run(context.Background(), skip.client(), t.TempDir())
	require.ErrorContains(t, err, "没有提交通过核验的结论")
	require.Empty(t, out.Decision)
}

// operation 与 downstream 不配套（inventory-svc/authorize）时交还主模型改正。
func TestScriptedPairCheck(t *testing.T) {
	s := &scripted{parent: []llm.StreamChunk{delegateAll, submitPair("p2", "inventory-svc", "authorize", latency, lockWait), submit("p3", latency, lockWait)}}
	out, err := Run(context.Background(), s.client(), t.TempDir())
	require.NoError(t, err)
	require.Equal(t, [2]int{2, 1}, [2]int{out.Submissions, out.Repairs})
	require.Equal(t, 1, s.parentInvalid())
	require.Contains(t, out.Rejections[0], "operation authorize 不是 inventory-svc 的操作，可选：reserve,query_stock")
	require.Equal(t, ExpectedOperation, out.Accepted.Operation)
}

// 三个子调查都已写入主日志、主 Agent 尚未提交时崩溃：恢复后 delegate_lookup 从日志回放，不新建子会话；
// 账本由报告中核对过的摘录补登，恢复后的提交仍能引用子调查内部的 ref 并通过核验。
func TestScriptedRecoverAfterLookups(t *testing.T) {
	parent := []llm.StreamChunk{delegateAll, submit("p2", latency, lockWait)}
	if dir := os.Getenv("SCOPED_LOOKUP_CRASH_DIR"); dir != "" {
		var mu sync.Mutex
		done := 0
		_, err := run(context.Background(), (&scripted{parent: parent}).client(), dir, options{onResult: func(r tools.DispatchResult) {
			mu.Lock()
			defer mu.Unlock()
			if r.Name == "delegate_lookup" {
				if done++; done == 3 {
					os.Exit(livecase.ChildExit)
				}
			}
		}})
		t.Fatalf("应在三个子调查落盘后退出：%v", err)
	}
	dir := t.TempDir()
	livecasetest.Child(t, "TestScriptedRecoverAfterLookups", "SCOPED_LOOKUP_CRASH_DIR="+dir)
	s := &scripted{parent: parent}
	out, err := Run(context.Background(), s.client(), dir)
	require.NoError(t, err)
	require.Equal(t, ExpectedDownstream, out.Decision)
	require.Equal(t, [2]int{1, 0}, [2]int{out.Submissions, out.Repairs})
	require.Zero(t, out.LookupsCreated+out.DataToolRuns, "子调查从主日志回放，不重跑")
	require.Equal(t, int64(1), out.NewModelCalls, "只请求主 Agent 的提交回合")
	require.Equal(t, final, out.Answer)
}

// 子 Agent 的工具集里根本没有未授予的工具。模型仍然调用时，Builder 在执行前拒绝这一回合：
// 这是致命错误，整条链中止，错误不交给任何模型，子会话照常清理。同一轮的其他子调查不被取消，照常跑完。
func TestScriptedUnofferedTool(t *testing.T) {
	s := &scripted{parent: []llm.StreamChunk{delegateAll}, mode: "unoffered"}
	out, err := run(context.Background(), s.client(), t.TempDir(), options{})
	require.ErrorContains(t, err, `模型请求了未提供的工具 "logs_query"`)
	require.Equal(t, Lookup{Focus: "metrics"}, out.Lookups[0], "未授予的工具没有被执行")
	require.Equal(t, 2, out.DataToolRuns, "只有 logs、traces 两个子调查执行了自己的工具")
	requireScoped(t, out)
	require.Equal(t, [2]int{3, 3}, [2]int{out.LookupsCreated, out.LookupsDone})
	require.Equal(t, int64(6), out.NewModelCalls, "主 1 次 + metrics 1 次 + logs、traces 各 2 次")
	require.Empty(t, s.parentSeen)
}

// 子 Agent 被误挂上 delegate_lookup 时，MaxDepth=1 让第 2 层委派在新建子会话之前被拒。
func TestScriptedNestedRejected(t *testing.T) {
	s := &scripted{parent: []llm.StreamChunk{delegateAll}, mode: "nest"}
	out, err := run(context.Background(), s.client(), t.TempDir(), options{nest: true})
	require.ErrorIs(t, err, tools.ErrNestedExecutionLimit)
	require.Equal(t, [2]int{3, 3}, [2]int{out.LookupsCreated, out.LookupsDone}, "没有创建第 2 层子会话，出错时第 1 层也被清理")
	require.Equal(t, int64(6), out.NewModelCalls, "主 1 次 + metrics 1 次 + logs、traces 各 2 次")
	require.Empty(t, s.parentSeen, "错误不交给模型")
}

// 主 Agent 的参数错误（方向不在枚举内、引用不存在）交还主模型改正；参数不合法时不创建子会话。
func TestScriptedRepair(t *testing.T) {
	bad := turn(call("p0", "delegate_lookup", map[string]string{"focus": "deploys", "question": "最近有发布吗？"}))
	s := &scripted{parent: []llm.StreamChunk{bad, delegateAll, submit("p2", missing), submit("p3", latency, lockWait)}}
	out, err := Run(context.Background(), s.client(), t.TempDir())
	require.NoError(t, err)
	require.Equal(t, ExpectedDownstream, out.Decision)
	require.Equal(t, [2]int{2, 1}, [2]int{out.Submissions, out.Repairs})
	require.Equal(t, 2, s.parentInvalid(), "非法方向与不存在的引用各交还一次")
	require.Equal(t, 3, out.LookupsCreated)
	requireScoped(t, out)
	require.Equal(t, int64(10), out.NewModelCalls)
}

// 修复次数用尽后中止，不发布未经核验的结论。
func TestScriptedRepairLimit(t *testing.T) {
	s := &scripted{parent: []llm.StreamChunk{delegateAll, submit("p2", fabricate), submit("p3", fabricate), submit("p4", fabricate), submit("p5", fabricate)}}
	out, err := Run(context.Background(), s.client(), t.TempDir())
	require.ErrorContains(t, err, "结论核验 3 次仍未通过")
	require.Nil(t, out.Accepted)
	require.Equal(t, [2]int{3, MaxRepairs}, [2]int{out.Submissions, out.Repairs})
	require.Equal(t, MaxRepairs, s.parentInvalid())
	require.Equal(t, int64(10), out.NewModelCalls)
}

// 内层参数错误只属于内层：子 Dispatcher 交还时由子模型改正；子 Dispatcher 不交还时，
// 错误穿出子会话后整条链中止，不会被主 Dispatcher 当作主模型的参数错误交还。
func TestScriptedInnerArgumentErrorBarrier(t *testing.T) {
	t.Run("子模型改正", func(t *testing.T) {
		s := &scripted{parent: []llm.StreamChunk{delegateAll, submit("p2", latency, lockWait)}, mode: "mismatch"}
		out, err := run(context.Background(), s.client(), t.TempDir(), options{})
		require.NoError(t, err)
		require.Equal(t, 1, s.childFixes)
		require.Zero(t, s.parentInvalid())
		require.Equal(t, []string{"traces_query", "traces_query"}, out.Lookups[2].Calls)
		require.Len(t, out.Lookups[2].Refs, 1, "参数错误的调用不登记账本")
		require.Equal(t, 3, out.DataToolRuns)
		require.Equal(t, int64(9), out.NewModelCalls)
	})
	t.Run("穿出子会话即中止", func(t *testing.T) {
		s := &scripted{parent: []llm.StreamChunk{delegateAll, submit("p2", latency, lockWait)}, mode: "mismatch"}
		out, err := run(context.Background(), s.client(), t.TempDir(), options{strictChild: true})
		var argErr *tools.ArgumentError
		require.ErrorAs(t, err, &argErr, "原始错误值保持不变")
		require.Contains(t, argErr.Message, "payment-svc 没有操作 reserve")
		require.Zero(t, s.childFixes)
		require.Empty(t, s.parentSeen, "主模型没有收到 invalid_arguments")
		require.Equal(t, [2]int{3, 3}, [2]int{out.LookupsCreated, out.LookupsDone})
		require.Equal(t, int64(6), out.NewModelCalls, "主 1 次 + metrics 2 次 + logs 2 次 + traces 1 次")
	})
}

func TestLiveScopedLookup(t *testing.T) {
	client := livemodeltest.Require(t)
	dir := t.TempDir()
	first, err := Run(context.Background(), client, dir)
	t.Logf("%s/%s：%+v", client.Live.Provider, client.Live.Model, first)
	require.NoError(t, err)
	require.Equal(t, ExpectedDownstream, first.Decision, first.Answer)
	require.NotNil(t, first.Accepted, "必须提交并通过核验")
	require.Equal(t, ExpectedDownstream, first.Accepted.Downstream)
	require.Equal(t, ExpectedOperation, first.Accepted.Operation)
	require.LessOrEqual(t, first.Repairs, MaxRepairs)
	require.GreaterOrEqual(t, first.LookupsCreated, 2, "至少派出 2 个子调查")
	requireScoped(t, first)
	require.Equal(t, int(first.NewModelCalls), first.Budget.ReservedRequests, "父子请求都经过根预算")

	again, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.Zero(t, again.NewModelCalls)
	require.Zero(t, again.LookupsCreated)
	require.Equal(t, first.Answer, again.Answer)
	require.Equal(t, first.Budget, again.Budget)
}
