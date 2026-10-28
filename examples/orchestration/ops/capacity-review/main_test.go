package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/internal/livemodel/livemodeltest"
	"github.com/kart-io/goagent/examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

// scripted 按系统提示区分初筛与深评；默认给出与规则一致的回答，测试用 screen/deep 覆盖。
type scripted struct {
	screen func(batch []Workload) string
	deep   map[string]string
	mu     sync.Mutex
	deepOn []string
}

func (s *scripted) client() *livemodel.Client {
	var calls atomic.Int64
	return livemodel.Wrap(&demomodel.Model{Calls: &calls, Respond: s.respond}, livemodel.Config{Provider: "scripted", Model: "v1"})
}

func (s *scripted) respond(r *llm.CompletionRequest) llm.StreamChunk {
	input := r.Messages[len(r.Messages)-1].Content
	if strings.Contains(r.Messages[0].Content, "初筛助手") {
		var in struct{ Workloads []Workload }
		if err := json.Unmarshal([]byte(input), &in); err != nil {
			panic(err)
		}
		if s.screen != nil {
			return llm.StreamChunk{Content: s.screen(in.Workloads)}
		}
		return llm.StreamChunk{Content: agreeScreen(in.Workloads)}
	}
	var in struct{ Workload Workload }
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		panic(err)
	}
	s.mu.Lock()
	s.deepOn = append(s.deepOn, in.Workload.ID)
	s.mu.Unlock()
	if text, ok := s.deep[in.Workload.ID]; ok {
		return llm.StreamChunk{Content: text}
	}
	_, p := Rule(in.Workload)
	return llm.StreamChunk{Content: deepJSON(in.Workload.ID, "confirm", p.Request, p.Limit, "P99 与峰值均有余量")}
}

func screenJSON(items ...map[string]any) string {
	b, _ := json.Marshal(map[string]any{"items": items})
	return string(b)
}

func screenEntry(w Workload, direction string) map[string]any {
	return map[string]any{"id": w.ID, "direction": direction, "evidence_complete": w.Evidence}
}

// agreeScreen 给出与规则同方向的初筛（证据不足项给 keep）。
func agreeScreen(batch []Workload) string {
	var items []map[string]any
	for _, w := range batch {
		action, _ := Rule(w)
		if action == Insufficient {
			action = Keep
		}
		items = append(items, screenEntry(w, action))
	}
	return screenJSON(items...)
}

func deepJSON(id, verdict string, request, limit int, reason string) string {
	return fmt.Sprintf(`{"id":%q,"verdict":%q,"request":%d,"limit":%d,"reason":%q}`, id, verdict, request, limit, reason)
}

func finals(out Result) map[string]string {
	m := map[string]string{}
	for _, it := range out.Items {
		m[it.ID] = it.Final
	}
	return m
}

func item(out Result, id string) Item {
	for _, it := range out.Items {
		if it.ID == id {
			return it
		}
	}
	panic(id)
}

// 不变量：可执行项满足三条件并通过护栏；其余计划为空；keep/证据不足永不可执行。
func requireInvariants(t *testing.T, out Result) {
	t.Helper()
	require.Len(t, out.Items, len(Workloads))
	for i, it := range out.Items {
		w := Workloads[i]
		switch {
		case it.Final == Execute:
			require.True(t, w.Evidence, it.ID)
			require.Contains(t, []string{ScaleDown, ScaleUp}, it.Rule, it.ID)
			require.Equal(t, it.Rule, it.Screen, it.ID)
			require.Equal(t, "confirm", it.Verdict, it.ID)
			require.NotNil(t, it.Plan, it.ID)
			require.Empty(t, Guard(w, it.Rule, *it.RulePlan, *it.Plan), it.ID)
		default:
			require.Nil(t, it.Plan, it.ID)
		}
		if it.Rule == Keep {
			require.Equal(t, Keep, it.Final, it.ID)
		}
		if it.Rule == Insufficient {
			require.Equal(t, ManualReview, it.Final, it.ID)
		}
	}
	require.Equal(t, out.ChildrenCreated, out.ChildrenClosed, "子会话创建数等于清理数")
	require.Equal(t, int(out.NewModelCalls), out.Budget.ReservedRequests, "全部请求都经过根预算")
}

var normal = map[string]string{
	"cap-a": Execute, "cap-b": Execute, "cap-c": Keep, "cap-d": Execute, "cap-e": ManualReview, "cap-f": Execute,
	"cap-g": Keep, "cap-h": Execute, "cap-i": ManualReview, "cap-j": Keep, "cap-k": Execute, "cap-l": ManualReview,
}

func TestRulesAndGuard(t *testing.T) {
	want := map[string]string{"cap-a": ScaleDown, "cap-b": ScaleUp, "cap-c": Keep, "cap-d": ScaleDown, "cap-e": Insufficient, "cap-f": ScaleUp,
		"cap-g": Keep, "cap-h": ScaleDown, "cap-i": Insufficient, "cap-j": Keep, "cap-k": ScaleUp, "cap-l": Insufficient}
	for _, w := range Workloads {
		action, plan := Rule(w)
		require.Equal(t, want[w.ID], action, w.ID)
		if plan != nil && (action == ScaleDown || action == ScaleUp) {
			require.Empty(t, Guard(w, action, *plan, *plan), "规则建议本身必须通过护栏：%s", w.ID)
		}
	}
	// 扩容只由一项触发时，另一项不低于当前值，规则计划仍须通过自己的护栏。
	for w, want := range map[Workload]Plan{
		{"probe-p99", 500, 2000, 480, 600, true}:  {600, 2000},  // P99 触发；Max×1.3=800 低于当前 limit
		{"probe-max", 1000, 1000, 300, 950, true}: {1000, 1250}, // Max 触发；P99×1.25=400 低于当前 request
	} {
		action, plan := Rule(w)
		require.Equal(t, ScaleUp, action, w.ID)
		require.Equal(t, want, *plan, w.ID)
		require.Empty(t, Guard(w, action, *plan, *plan), w.ID)
	}
	a, ruleA := Workloads[0], Plan{300, 650} // request 1000 limit 2000 P99 220 Max 500
	for plan, msg := range map[Plan]string{
		{200, 650}: "request 低于 P99", {300, 400}: "limit 低于 Max", {300, 4100}: "超过当前值 2 倍",
		{600, 550}: "limit 小于 request", {1100, 1500}: "缩容却调高", {250, 650}: "缩容比规则更激进",
		{300, 600}: "缩容比规则更激进", {1000, 2000}: "计划与当前值相同",
	} {
		require.Contains(t, Guard(a, ScaleDown, ruleA, plan), msg, plan)
	}
	b, ruleB := Workloads[1], Plan{600, 1250} // request 500 limit 1000 P99 480 Max 960
	for plan, msg := range map[Plan]string{
		{450, 1100}: "扩容却调低", {650, 1250}: "扩容比规则更激进", {600, 1300}: "扩容比规则更激进", {500, 1000}: "计划与当前值相同",
	} {
		require.Contains(t, Guard(b, ScaleUp, ruleB, plan), msg, plan)
	}
	require.Empty(t, Guard(a, ScaleDown, ruleA, Plan{500, 1000}), "介于规则值与当前值之间的保守计划可以通过")
	require.Empty(t, Guard(b, ScaleUp, ruleB, Plan{500, 1100}), "request 不变、limit 上调的部分扩容可以通过")
}

func TestScriptedNormalMatrixAndReplay(t *testing.T) {
	client, dir := (&scripted{}).client(), t.TempDir()
	first, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	requireInvariants(t, first)
	require.Equal(t, normal, finals(first))
	require.Empty(t, first.BatchErrors)
	require.Equal(t, &Plan{300, 650}, item(first, "cap-a").Plan)
	require.Equal(t, 6, first.DeepReviews)
	require.Equal(t, int64(6), first.ChildrenCreated)
	require.Equal(t, int64(8), first.NewModelCalls, "2 次初筛 + 6 次深评")

	again, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.Zero(t, again.NewModelCalls)
	require.Zero(t, again.ChildrenCreated, "回放不新建子会话")
	require.Equal(t, first.Items, again.Items)
	require.Equal(t, first.Budget, again.Budget)
}

// 第 1 批初筛违反合同时整批作废：本批可执行项降为人工复核且不深评，第 2 批照常。
func TestScriptedScreenBatchVoid(t *testing.T) {
	cases := map[string]func([]Workload) string{
		"漏掉 id": func(b []Workload) string { return agreeScreen(b[:5]) },
		"重复 id": func(b []Workload) string {
			return screenJSON(screenEntry(b[0], ScaleDown), screenEntry(b[0], ScaleDown), screenEntry(b[1], ScaleUp),
				screenEntry(b[2], Keep), screenEntry(b[3], ScaleDown), screenEntry(b[4], Keep))
		},
		"多出 id": func(b []Workload) string {
			return agreeScreen(append(append([]Workload{}, b...), Workload{ID: "cap-x", Evidence: true}))
		},
		"证据不完整却给 scale": func(b []Workload) string {
			var items []map[string]any
			for _, w := range b {
				action, _ := Rule(w)
				if action == Insufficient {
					action = ScaleDown
				}
				items = append(items, screenEntry(w, action))
			}
			return screenJSON(items...)
		},
		"非法 JSON": func([]Workload) string { return "初筛结果：大部分可以缩容。" },
		"字段名大小写不符": func(b []Workload) string {
			return strings.Replace(agreeScreen(b), `"id"`, `"ID"`, 1)
		},
		"evidence_complete 为 null": func(b []Workload) string {
			return strings.Replace(agreeScreen(b), `"evidence_complete":true`, `"evidence_complete":null`, 1)
		},
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			s := &scripted{screen: func(b []Workload) string {
				if b[0].ID == "cap-a" {
					return bad(b)
				}
				return agreeScreen(b)
			}}
			out, err := Run(context.Background(), s.client(), t.TempDir())
			require.NoError(t, err)
			requireInvariants(t, out)
			require.Len(t, out.BatchErrors, 1)
			require.Contains(t, out.BatchErrors[0], "初筛第 1 批作废")
			for _, id := range []string{"cap-a", "cap-b", "cap-d", "cap-f"} {
				require.Equal(t, ManualReview, item(out, id).Final, id)
				require.Contains(t, item(out, id).Reasons, "缺少有效初筛")
			}
			require.Equal(t, Execute, item(out, "cap-h").Final)
			require.Equal(t, Execute, item(out, "cap-k").Final)
			require.ElementsMatch(t, []string{"cap-h", "cap-k"}, s.deepOn, "作废批次的项不深评")
			require.Equal(t, int64(4), out.NewModelCalls)
		})
	}
}

// 深评的各种越界都把该项降为人工复核并清空计划，其他项不受影响。
func TestScriptedDeepDowngrade(t *testing.T) {
	t.Run("比规则更激进或空操作", func(t *testing.T) {
		s := &scripted{deep: map[string]string{
			"cap-a": deepJSON("cap-a", "confirm", 220, 500, "按 P99 与峰值压到底"), // 规则 300/650
			"cap-b": deepJSON("cap-b", "confirm", 500, 1000, "维持现状即可"),      // 与当前值相同
			"cap-d": deepJSON("cap-d", "confirm", 1000, 2000, "先缩一半更稳妥"),    // 介于规则与当前值之间，保留
		}}
		out, err := Run(context.Background(), s.client(), t.TempDir())
		require.NoError(t, err)
		requireInvariants(t, out)
		require.Equal(t, ManualReview, item(out, "cap-a").Final)
		require.Nil(t, item(out, "cap-a").Plan)
		require.Contains(t, strings.Join(item(out, "cap-a").Reasons, "；"), "缩容比规则更激进")
		require.Equal(t, ManualReview, item(out, "cap-b").Final)
		require.Nil(t, item(out, "cap-b").Plan)
		require.Contains(t, strings.Join(item(out, "cap-b").Reasons, "；"), "计划与当前值相同")
		require.Equal(t, Execute, item(out, "cap-d").Final)
		require.Equal(t, &Plan{1000, 2000}, item(out, "cap-d").Plan)
	})

	s := &scripted{deep: map[string]string{
		"cap-a": deepJSON("cap-a", "confirm", 200, 650, "可进一步压缩"),
		"cap-b": deepJSON("cap-b", "confirm", 600, 2500, "留足余量"),
		"cap-d": deepJSON("cap-d", "confirm", 400, 1200, "确认后执行 kubectl apply -f capacity.yaml"), // reason 从不执行，只看数值
		"cap-f": deepJSON("cap-x", "confirm", 500, 950, "同意"),
		"cap-h": "同意规则建议。",
		"cap-k": deepJSON("cap-k", "reject", 0, 0, "峰值波动大"),
	}}
	out, err := Run(context.Background(), s.client(), t.TempDir())
	require.NoError(t, err)
	requireInvariants(t, out)
	want := map[string]string{
		"cap-a": "request 低于 P99", "cap-b": "超过当前值 2 倍",
		"cap-f": "深评回显 id", "cap-h": "深评输出非法", "cap-k": "深评 reject",
	}
	for id, reason := range want {
		it := item(out, id)
		require.Equal(t, ManualReview, it.Final, id)
		require.Contains(t, strings.Join(it.Reasons, "；"), reason, id)
	}
	require.Equal(t, Execute, item(out, "cap-d").Final, "reason 文本不影响结论")
	require.Equal(t, &Plan{400, 1200}, item(out, "cap-d").Plan)
	require.Equal(t, [2]int64{6, 6}, [2]int64{out.ChildrenCreated, out.ChildrenClosed})
}

// keep 项即使初筛给出 scale_*，也不会进入深评，更不会变成可执行。
func TestScriptedKeepNeverExecutable(t *testing.T) {
	s := &scripted{screen: func(b []Workload) string {
		var items []map[string]any
		for _, w := range b {
			action, _ := Rule(w)
			switch action {
			case Insufficient:
				action = Keep
			case Keep:
				action = ScaleDown
			}
			items = append(items, screenEntry(w, action))
		}
		return screenJSON(items...)
	}}
	out, err := Run(context.Background(), s.client(), t.TempDir())
	require.NoError(t, err)
	requireInvariants(t, out)
	require.Equal(t, normal, finals(out))
	for _, id := range []string{"cap-c", "cap-g", "cap-j"} {
		require.Equal(t, ScaleDown, item(out, id).Screen)
		require.NotContains(t, s.deepOn, id)
	}
}

// 额度按配置预先分配：初筛占 2 次，剩余额度按 id 顺序分给深评，其余项确定地降为“预算不足”。
func TestScriptedBudgetLimit(t *testing.T) {
	s := &scripted{}
	out, err := run(context.Background(), s.client(), t.TempDir(), 5)
	require.NoError(t, err)
	requireInvariants(t, out)
	for _, id := range []string{"cap-a", "cap-b", "cap-d"} {
		require.Equal(t, Execute, item(out, id).Final, id)
	}
	for _, id := range []string{"cap-f", "cap-h", "cap-k"} {
		require.Equal(t, ManualReview, item(out, id).Final, id)
		require.Contains(t, item(out, id).Reasons, "预算不足，未深评")
	}
	require.Equal(t, int64(5), out.NewModelCalls)

	// 额度只够第 1 批初筛：第 2 批被根预算拒绝而作废，整体无可执行项。
	s = &scripted{}
	out, err = run(context.Background(), s.client(), t.TempDir(), 1)
	require.NoError(t, err)
	requireInvariants(t, out)
	require.Len(t, out.BatchErrors, 1)
	require.Contains(t, out.BatchErrors[0], "初筛第 2 批作废")
	require.Contains(t, out.BatchErrors[0], "budget exceeded")
	for _, f := range finals(out) {
		require.NotEqual(t, Execute, f)
	}
	require.Equal(t, int64(1), out.NewModelCalls)
	require.Empty(t, s.deepOn)
}

func TestLiveCapacityReview(t *testing.T) {
	client := livemodeltest.Require(t)
	dir := t.TempDir()
	first, err := Run(context.Background(), client, dir)
	t.Logf("%s/%s：%+v", client.Live.Provider, client.Live.Model, first)
	require.NoError(t, err)
	requireInvariants(t, first)
	require.LessOrEqual(t, first.ChildrenCreated, int64(first.DeepReviews))
	// 降级本身是安全的，但 AI 全部失败时“通过”没有意义：要求初筛都有效、至少进入一次深评。
	require.Empty(t, first.BatchErrors)
	require.Positive(t, first.DeepReviews)

	again, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.Zero(t, again.NewModelCalls)
	require.Zero(t, again.ChildrenCreated)
	require.Equal(t, first.Items, again.Items)
	require.Equal(t, first.Budget, again.Budget)
}
