package builder

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/cache"
	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/tools"
)

// cacheModel 按调用序号给出回合并记录请求；id 非空时声明 CacheIdentity，使调用可缓存。
type cacheModel struct {
	*MockLLMClient
	id    string
	reply func(n int) ([]*llm.StreamChunk, error)
	// block 非空时在给出回合前调用，用于模拟进行中的调用。
	block func(ctx context.Context)
	mu    sync.Mutex
	reqs  []*llm.CompletionRequest
}

func newCacheModel(id string, turns ...[]*llm.StreamChunk) *cacheModel {
	return &cacheModel{MockLLMClient: NewMockLLMClient(), id: id, reply: func(n int) ([]*llm.StreamChunk, error) {
		if n >= len(turns) {
			return nil, errors.New("发生了多余模型请求")
		}
		return turns[n], nil
	}}
}

func (m *cacheModel) CacheIdentity() string { return m.id }

func (m *cacheModel) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.reqs)
}

func (m *cacheModel) request(i int) *llm.CompletionRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reqs[i]
}

func (m *cacheModel) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return m.CompleteToolsStream(ctx, req, nil)
}

func (m *cacheModel) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	m.mu.Lock()
	copied := *req
	copied.Messages = slices.Clone(req.Messages)
	m.reqs = append(m.reqs, &copied)
	n := len(m.reqs) - 1
	m.mu.Unlock()
	if m.block != nil {
		m.block(ctx)
	}
	chunks, err := m.reply(n)
	if err != nil {
		return nil, err
	}
	out := make(chan *llm.StreamChunk, len(chunks))
	for _, c := range chunks {
		copied := *c
		out <- &copied
	}
	close(out)
	return out, nil
}

func textTurn(text string) []*llm.StreamChunk {
	return []*llm.StreamChunk{{Delta: text}, {Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: 5}}}
}

func toolTurn(id, name, args string) []*llm.StreamChunk {
	return []*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall(id, name, args)}, Usage: &llm.Usage{TotalTokens: 5}}}
}

// countingCache 在 SimpleCache 外记录各操作次数，并可注入后端错误。
type countingCache struct {
	*cache.SimpleCache
	gets, sets, deletes atomic.Int64
	getErr, setErr      error
	mu                  sync.Mutex
	keys                []string
}

func newCountingCache() *countingCache {
	return &countingCache{SimpleCache: cache.NewSimpleCache(time.Hour, cache.WithoutBackgroundCleanup())}
}

func (c *countingCache) Get(ctx context.Context, key string) (interface{}, error) {
	c.gets.Add(1)
	if c.getErr != nil {
		return nil, c.getErr
	}
	return c.SimpleCache.Get(ctx, key)
}

func (c *countingCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	c.sets.Add(1)
	c.record(key)
	if c.setErr != nil {
		return c.setErr
	}
	return c.SimpleCache.Set(ctx, key, value, ttl)
}

func (c *countingCache) SetTagged(ctx context.Context, key string, value []byte, ttl time.Duration, tags ...string) error {
	c.sets.Add(1)
	c.record(key)
	if c.setErr != nil {
		return c.setErr
	}
	return c.SimpleCache.SetTagged(ctx, key, value, ttl, tags...)
}

func (c *countingCache) Delete(ctx context.Context, key string) error {
	c.deletes.Add(1)
	return c.SimpleCache.Delete(ctx, key)
}

func (c *countingCache) record(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = append(c.keys, key)
}

func (c *countingCache) written() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.keys)
}

func (c *countingCache) size() int64 { return c.GetStats().Size }

func scopeOf(id string) func(context.Context) (string, bool) {
	return func(context.Context) (string, bool) { return id, id != "" }
}

// cacheSink 记录观测事件。
type cacheSink struct {
	mu     sync.Mutex
	events []observe.Event
}

func (s *cacheSink) Observe(_ context.Context, e observe.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *cacheSink) find(typ string) (observe.Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.events {
		if e.Type == typ {
			return e, true
		}
	}
	return observe.Event{}, false
}

// cacheRunSpec 描述一次执行的配置；两次执行之间的差异决定是否命中。
type cacheRunSpec struct {
	identity string
	scope    string
	session  string
	temp     *float64
	tools    []interfaces.Tool
	log      session.Store
}

func schemaTool(schema string) interfaces.Tool {
	return tools.NewBaseTool("lookup", "test", schema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "found"}, nil
	})
}

func TestModelCacheRun(t *testing.T) {
	half, other, zero := 0.5, 0.7, 0.0
	sharedLog := session.NewMemoryStore()
	cases := []struct {
		name          string
		first, second func(*cacheRunSpec)
		calls         int
		sets, gets    int64
	}{
		{name: "完全相同的请求命中", calls: 1, sets: 1, gets: 2},
		{name: "隔离身份不同", second: func(s *cacheRunSpec) { s.scope = "u2" }, calls: 2, sets: 2, gets: 2},
		{name: "会话不同", second: func(s *cacheRunSpec) { s.session = "s2" }, calls: 2, sets: 2, gets: 2},
		{name: "Scope 返回 false 不查也不写", first: func(s *cacheRunSpec) { s.scope = "" }, second: func(s *cacheRunSpec) { s.scope = "" }, calls: 2},
		{name: "相同输入但历史不同", first: func(s *cacheRunSpec) { s.log = sharedLog }, second: func(s *cacheRunSpec) { s.log = sharedLog }, calls: 2, sets: 2, gets: 2},
		{name: "客户端身份不同", second: func(s *cacheRunSpec) { s.identity = "dep-2" }, calls: 2, sets: 2, gets: 2},
		{name: "温度不同", first: func(s *cacheRunSpec) { s.temp = &half }, second: func(s *cacheRunSpec) { s.temp = &other }, calls: 2, sets: 2, gets: 2},
		{name: "显式温度 0 与未设置", second: func(s *cacheRunSpec) { s.temp = &zero }, calls: 2, sets: 2, gets: 2},
		{name: "增加工具", second: func(s *cacheRunSpec) { s.tools = []interfaces.Tool{plainTool()} }, calls: 2, sets: 2, gets: 2},
		{name: "客户端无法解析模型名不缓存", first: func(s *cacheRunSpec) { s.identity = "" }, second: func(s *cacheRunSpec) { s.identity = "" }, calls: 2},
		{name: "工具 schema 只是键顺序不同仍命中",
			first:  func(s *cacheRunSpec) { s.tools = []interfaces.Tool{schemaTool(`{"type":"object","properties":{}}`)} },
			second: func(s *cacheRunSpec) { s.tools = []interfaces.Tool{schemaTool(`{"properties":{},"type":"object"}`)} },
			calls:  1, sets: 1, gets: 2},
		{name: "工具 schema 无法解析不缓存",
			first:  func(s *cacheRunSpec) { s.tools = []interfaces.Tool{schemaTool(`{`)} },
			second: func(s *cacheRunSpec) { s.tools = []interfaces.Tool{schemaTool(`{`)} },
			calls:  2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newCountingCache()
			calls, hits := 0, 0
			for i, change := range []func(*cacheRunSpec){tc.first, tc.second} {
				spec := cacheRunSpec{identity: "dep", scope: "u1", session: "s"}
				if change != nil {
					change(&spec)
				}
				model := newCacheModel(spec.identity, textTurn("答案"))
				b := NewSimpleBuilder(model).WithSessionID(spec.session).WithTools(spec.tools...).
					WithModelCache(ModelCache{Cache: c, TTL: time.Hour, Scope: scopeOf(spec.scope), TagKey: testTagKey})
				if spec.temp != nil {
					b = b.WithTemperature(*spec.temp)
				}
				if spec.log != nil {
					b = b.WithSessionLog(spec.log)
				}
				agent, err := b.Build()
				require.NoError(t, err)
				out, err := agent.AsRunnable().Invoke(context.Background(), "问题")
				require.NoError(t, err, "第 %d 次执行", i)
				require.Equal(t, "答案", out.Result)
				calls += model.calls()
				if n, ok := out.Metadata["model_cache_hits"].(int64); ok {
					hits += int(n)
				}
			}
			require.Equal(t, tc.calls, calls, "模型调用次数")
			require.Equal(t, 2-tc.calls, hits, "命中次数")
			require.Equal(t, tc.sets, c.sets.Load(), "写入次数")
			require.Equal(t, tc.gets, c.gets.Load(), "查找次数")
		})
	}
}

// 命中时：模型不被调用，会话日志与未命中时逐事件相同，事件流含 model_cached 与一整段 text，计数不含命中。
func TestModelCacheRunHitOutput(t *testing.T) {
	c := newCountingCache()
	type result struct {
		out    *AgentOutput
		events []AgentEvent
		log    []session.Event
		calls  int
	}
	run := func() result {
		model := newCacheModel("dep", textTurn("完整的答案"))
		log := session.NewMemoryStore()
		agent, err := NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(log).WithTools(plainTool()).
			WithModelCache(ModelCache{Cache: c, TTL: time.Hour, Scope: scopeOf("u1"), TagKey: testTagKey}).Build()
		require.NoError(t, err)
		stream, err := agent.ExecuteStream(context.Background(), "问题")
		require.NoError(t, err)
		var r result
		for e := range stream {
			r.events = append(r.events, e)
			if e.Type == "done" {
				r.out = e.Output
			}
			require.NotEqual(t, "error", e.Type, e.Message)
		}
		r.log, r.calls = sessionEvents(t, log, "s"), model.calls()
		return r
	}
	miss, hit := run(), run()
	require.Equal(t, 1, miss.calls)
	require.Zero(t, hit.calls, "命中时不调用模型")

	stripTime := func(events []session.Event) []session.Event {
		out := slices.Clone(events)
		for i := range out {
			out[i].Time = time.Time{}
		}
		return out
	}
	missLog, hitLog := stripTime(miss.log), stripTime(hit.log)
	require.Equal(t, len(missLog), len(hitLog))
	for i := range missLog {
		if missLog[i].Type == constants.SessionEventTurnEnd {
			// 命中不消耗 token，turn/end 的用量为空；其余内容相同。
			var a, b session.TurnEnd
			require.NoError(t, json.Unmarshal(missLog[i].Data, &a))
			require.NoError(t, json.Unmarshal(hitLog[i].Data, &b))
			require.Equal(t, 5, a.Usage.TotalTokens)
			require.Zero(t, b.Usage.TotalTokens)
			a.Usage, b.Usage = nil, nil
			require.Equal(t, a, b)
			continue
		}
		require.Equal(t, missLog[i], hitLog[i], "第 %d 条会话日志事件", i)
	}

	var steps, texts []string
	for _, e := range hit.events {
		switch e.Type {
		case "step":
			steps = append(steps, e.Step)
		case "text":
			texts = append(texts, e.Delta)
		}
	}
	require.Contains(t, steps, "model_cached")
	require.NotContains(t, steps, "model_replayed")
	require.Equal(t, []string{"完整的答案"}, texts, "命中时发一整段 text")

	require.Equal(t, 1, miss.out.Metadata["model_calls"])
	require.Equal(t, int64(0), miss.out.Metadata["model_cache_hits"])
	require.Equal(t, 0, hit.out.Metadata["model_calls"], "model_calls 不计命中")
	require.Equal(t, int64(1), hit.out.Metadata["model_cache_hits"])
	require.Equal(t, true, miss.out.Metadata["usage_complete"])
	require.Equal(t, miss.out.Metadata["usage_complete"], hit.out.Metadata["usage_complete"])
}

// 带预算的持久运行：命中不预留不记账，恢复时从日志回放、不查缓存也不报 "usage has no reservation"。
func TestModelCacheRunDurableBudget(t *testing.T) {
	c := newCountingCache()
	mc := ModelCache{Cache: c, TTL: time.Hour, Scope: scopeOf("u1"), TagKey: testTagKey}
	run := func(store durable.Store, key string, model *cacheModel) (*AgentOutput, budget.Snapshot) {
		ctrl, err := budget.NewDurable(budget.Config{RootKey: "root-" + key, Version: "v1", MaxModelRequests: 5}, store)
		require.NoError(t, err)
		ctx := durable.WithJournal(durable.WithExecutionKey(budget.WithController(context.Background(), ctrl), key), store)
		agent, err := NewSimpleBuilder(model).WithDurableScope("cache").WithSessionID("s").WithModelCache(mc).Build()
		require.NoError(t, err)
		out, err := agent.AsRunnable().Invoke(ctx, "问题")
		require.NoError(t, err)
		snap, err := ctrl.Snapshot(context.Background())
		require.NoError(t, err)
		return out, snap
	}
	missStore, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	hitStore, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)

	model := newCacheModel("dep", textTurn("答案"), textTurn("不应再请求"))
	_, missSnap := run(missStore, "e1", model)
	require.Equal(t, 1, model.calls())
	require.Equal(t, 1, missSnap.ReservedRequests)
	require.EqualValues(t, 1, c.sets.Load())

	out, hitSnap := run(hitStore, "e2", model)
	require.Equal(t, "答案", out.Result)
	require.Equal(t, 1, model.calls(), "命中不调用模型")
	require.Zero(t, hitSnap.ReservedRequests, "命中不预留")
	require.Zero(t, hitSnap.KnownUsageEntries+hitSnap.UnknownUsageEntries, "命中不记账")
	gets := c.gets.Load()

	again, replaySnap := run(hitStore, "e2", model)
	require.Equal(t, "答案", again.Result)
	require.Equal(t, 1, model.calls())
	require.Equal(t, gets, c.gets.Load(), "恢复时从日志回放，不查缓存")
	require.EqualValues(t, 1, c.sets.Load(), "回放不写缓存")
	require.Equal(t, hitSnap, replaySnap, "预算只算真实调用")
}

// 不满足写入条件的回答不写入缓存。
func TestModelCacheRunNotCached(t *testing.T) {
	state := &llm.ProviderState{}
	require.NoError(t, json.Unmarshal([]byte(`{"gemini":{"parts":[]}}`), state))
	cases := []struct {
		name     string
		turns    [][]*llm.StreamChunk
		fail     error
		tools    bool
		feedback bool
		cancel   bool
		sets     int64
	}{
		{name: "出错", fail: errors.New("模型不可用")},
		{name: "截断", turns: [][]*llm.StreamChunk{{{Delta: "半"}, {Done: true, FinishReason: "length"}}}},
		{name: "取消", cancel: true},
		{name: "空回答", turns: [][]*llm.StreamChunk{{{Done: true, FinishReason: "stop"}}}},
		{name: "带 ProviderState", turns: [][]*llm.StreamChunk{{{Delta: "答"}, {Done: true, FinishReason: "stop", ProviderState: state}}}},
		{name: "调用不存在的工具（反馈模式）", tools: true, feedback: true,
			turns: [][]*llm.StreamChunk{toolTurn("c1", "missing", "{}"), textTurn("答")}, sets: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newCountingCache()
			model := newCacheModel("dep", tc.turns...)
			if tc.fail != nil {
				model.reply = func(int) ([]*llm.StreamChunk, error) { return nil, tc.fail }
			}
			ctx := context.Background()
			if tc.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				model.block = func(context.Context) { cancel() }
				model.reply = func(int) ([]*llm.StreamChunk, error) { return []*llm.StreamChunk{{Delta: "半"}}, nil }
			}
			b := NewSimpleBuilder(model).WithSessionID("s").WithMaxIterations(3).
				WithModelCache(ModelCache{Cache: c, TTL: time.Hour, Scope: scopeOf("u1"), TagKey: testTagKey})
			if tc.tools {
				b = b.WithTools(plainTool())
			}
			if tc.feedback {
				b = b.WithToolErrorFeedback()
			}
			agent, err := b.Build()
			require.NoError(t, err)
			_, _ = agent.AsRunnable().Invoke(ctx, "问题")
			require.Equal(t, tc.sets, c.sets.Load())
			if tc.sets > 0 {
				require.Equal(t, 2, model.calls())
			}
		})
	}
}

// 缓存值损坏或不能通过校验时按未命中处理：调用模型并删除该条目。
func TestModelCacheRunCorruptEntry(t *testing.T) {
	bad, err := encodeCachedTurn(&llm.StreamChunk{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c1", "missing", "{}")}})
	require.NoError(t, err)
	cases := []struct {
		name  string
		value any
	}{
		{"非法字节", []byte("garbage")},
		{"未知版本", []byte("mc2\n{}")},
		{"非法 JSON", []byte("mc1\n{")},
		{"不是字节", 42},
		{"调用不存在的工具", bad},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newCountingCache()
			model := newCacheModel("dep", textTurn("答案"), textTurn("答案"), textTurn("多余"))
			agent, err := NewSimpleBuilder(model).WithSessionID("s").WithTools(plainTool()).
				WithModelCache(ModelCache{Cache: c, TTL: time.Hour, Scope: scopeOf("u1"), TagKey: testTagKey}).Build()
			require.NoError(t, err)
			_, err = agent.AsRunnable().Invoke(context.Background(), "问题")
			require.NoError(t, err)
			keys := c.written()
			require.Len(t, keys, 1)
			require.NoError(t, c.SimpleCache.Set(context.Background(), keys[0], tc.value, time.Hour))

			out, err := agent.AsRunnable().Invoke(context.Background(), "问题")
			require.NoError(t, err)
			require.Equal(t, "答案", out.Result)
			require.Equal(t, 2, model.calls(), "损坏的条目按未命中处理")
			require.EqualValues(t, 1, c.deletes.Load(), "删除损坏的条目")

			_, err = agent.AsRunnable().Invoke(context.Background(), "问题")
			require.NoError(t, err)
			require.Equal(t, 2, model.calls(), "重新写入后命中")
		})
	}
}

// memoryAgent 构造带长期记忆与模型缓存的 Agent；log 为空时用新的会话日志，使历史与之前的执行相同。
func memoryAgent(t *testing.T, m *memory.Memory, mc ModelCache, model *cacheModel, ts ...interfaces.Tool) *ConfigurableAgent[*core.AgentState] {
	t.Helper()
	agent, err := NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(session.NewMemoryStore()).WithLongTermMemory(m).
		WithTools(append([]interfaces.Tool{plainTool()}, ts...)...).WithModelCache(mc).Build()
	require.NoError(t, err)
	return agent
}

// Forget、Rewound、Purge 之后相同的请求不命中，并且对应标签的条目已被物理删除。
func TestModelCacheRunMemoryErase(t *testing.T) {
	cases := []struct {
		name string
		op   func(ctx context.Context, m *memory.Memory) error
	}{
		{"Forget", func(ctx context.Context, m *memory.Memory) error { return m.Forget(ctx, "r1") }},
		{"Rewound", func(ctx context.Context, m *memory.Memory) error { return m.Rewound(ctx, "s", 0) }},
		{"Purge", func(ctx context.Context, m *memory.Memory) error { return m.Purge(ctx) }},
	}
	for _, tc := range cases {
		for _, erase := range []bool{true, false} {
			t.Run(tc.name+map[bool]string{true: "/物理删除", false: "/仅版本失效"}[erase], func(t *testing.T) {
				ctx := memory.WithNamespace(context.Background(), "t1", "alice")
				c := newCountingCache()
				mc := ModelCache{Cache: c, TTL: time.Minute, TagKey: testTagKey}
				cfg := memory.Config{}
				if erase {
					cfg.OnErase = mc.Eraser()
				}
				m := newModelCacheMemory(t, cfg)
				_, err := m.Remember(ctx, memory.Record{ID: "r1", Content: "用户养了一只猫", Importance: 0.1})
				require.NoError(t, err)

				model := newCacheModel("dep", textTurn("答案"), textTurn("答案"))
				_, err = memoryAgent(t, m, mc, model).AsRunnable().Invoke(ctx, "问题")
				require.NoError(t, err)
				require.EqualValues(t, 1, c.size())

				require.NoError(t, tc.op(ctx, m))
				if erase {
					require.Zero(t, c.size(), "对应标签的条目已被物理删除")
				} else {
					require.EqualValues(t, 1, c.size())
				}
				_, err = memoryAgent(t, m, mc, model).AsRunnable().Invoke(ctx, "问题")
				require.NoError(t, err)
				require.Equal(t, 2, model.calls(), "删除类操作之后不命中")
				require.Equal(t, model.request(0).Messages, model.request(1).Messages, "请求相同，不命中来自记忆版本")
			})
		}
	}
}

// gatedTool 与 inner 定义相同；open 为 false 时不执行，直接返回 result，使两次执行的请求只差记忆版本。
type gatedTool struct {
	interfaces.Tool
	open   bool
	result string
}

func (g gatedTool) Invoke(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	if g.open {
		return g.Tool.Invoke(ctx, in)
	}
	return &interfaces.ToolOutput{Success: true, Result: g.result}, nil
}

// 同一轮中先命中，执行 memory_forget 之后的下一步不命中：版本在每次调用前重新读取。
func TestModelCacheRunForgetWithinTurn(t *testing.T) {
	for _, forget := range []bool{false, true} {
		t.Run(map[bool]string{false: "未遗忘时下一步命中", true: "遗忘后下一步不命中"}[forget], func(t *testing.T) {
			ctx := memory.WithNamespace(context.Background(), "t1", "alice")
			m, _ := newTestMemory(t, ctx)
			_, err := m.Remember(ctx, memory.Record{ID: "r1", Content: "用户养了一只猫", Importance: 0.1})
			require.NoError(t, err)
			var forgetTool interfaces.Tool
			for _, tool := range m.Tools() {
				if tool.Name() == constants.ToolMemoryForget {
					forgetTool = tool
				}
			}
			c := newCountingCache()
			mc := ModelCache{Cache: c, TTL: time.Minute, TagKey: testTagKey}
			model := newCacheModel("dep", toolTurn("c1", constants.ToolMemoryForget, `{"id":"r1"}`), textTurn("好的"), textTurn("好的"))
			_, err = memoryAgent(t, m, mc, model, gatedTool{Tool: forgetTool, result: `{"forgotten":"r1"}`}).AsRunnable().Invoke(ctx, "清理")
			require.NoError(t, err)
			require.Equal(t, 2, model.calls())

			out, err := memoryAgent(t, m, mc, model, gatedTool{Tool: forgetTool, open: forget, result: `{"forgotten":"r1"}`}).AsRunnable().Invoke(ctx, "清理")
			require.NoError(t, err)
			require.Equal(t, "好的", out.Result)
			if !forget {
				require.Equal(t, 2, model.calls())
				require.Equal(t, int64(2), out.Metadata["model_cache_hits"])
				return
			}
			require.Equal(t, 3, model.calls(), "第一步命中，遗忘之后的下一步不命中")
			require.Equal(t, int64(1), out.Metadata["model_cache_hits"])
			require.Equal(t, model.request(1).Messages, model.request(2).Messages, "请求只差记忆版本")
		})
	}
}

// 进行中的调用跨过一次 Forget 返回：写入前复核版本不一致，放弃写入。
func TestModelCacheRunVersionRecheck(t *testing.T) {
	ctx := memory.WithNamespace(context.Background(), "t1", "alice")
	m, _ := newTestMemory(t, ctx)
	_, err := m.Remember(ctx, memory.Record{ID: "r1", Content: "无关的旧事", Importance: 0.1})
	require.NoError(t, err)
	c := newCountingCache()
	model := newCacheModel("dep", textTurn("答案"))
	model.block = func(context.Context) { require.NoError(t, m.Forget(ctx, "r1")) }
	sink := &cacheSink{}
	_, err = memoryAgent(t, m, ModelCache{Cache: c, TTL: time.Minute, TagKey: testTagKey}, model).AsRunnable().Invoke(observe.WithObserver(ctx, sink), "问题")
	require.NoError(t, err)
	require.Zero(t, c.sets.Load(), "调用期间发生删除，放弃写入")
	_, ok := sink.find("model_cache_write_skipped")
	require.True(t, ok)
}

// 两个 Builder 共用同一个 ModelCache：身份与请求相同时命中，客户端身份不同时不命中。
func TestModelCacheSharedAcrossAgents(t *testing.T) {
	for _, tc := range []struct {
		name   string
		second string
		calls  int
	}{
		{"身份相同命中", "dep", 1},
		{"客户端 CacheIdentity 不同", "dep-2", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mc := ModelCache{Cache: newCountingCache(), TTL: time.Hour, Scope: scopeOf("u1"), TagKey: testTagKey}
			calls := 0
			for _, id := range []string{"dep", tc.second} {
				model := newCacheModel(id, textTurn("答案"))
				agent, err := NewSimpleBuilder(model).WithSessionID("s").WithTools(plainTool()).WithModelCache(mc).Build()
				require.NoError(t, err)
				out, err := agent.AsRunnable().Invoke(context.Background(), "问题")
				require.NoError(t, err)
				require.Equal(t, "答案", out.Result)
				calls += model.calls()
			}
			require.Equal(t, tc.calls, calls)
		})
	}
}

// 后端出错不影响执行：查找失败按未命中调用模型，写入失败只记录。
func TestModelCacheBackendErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		get    error
		set    error
		event  string
		hasSet bool
	}{
		{name: "Get 返回一般错误", get: errors.New("后端不可用"), event: "model_cache_read_error"},
		{name: "SetTagged 返回错误", set: errors.New("后端不可用"), event: "model_cache_write_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCountingCache()
			c.getErr, c.setErr = tc.get, tc.set
			model := newCacheModel("dep", textTurn("答案"))
			agent, err := NewSimpleBuilder(model).WithSessionID("s").WithTools(plainTool()).
				WithModelCache(ModelCache{Cache: c, TTL: time.Hour, Scope: scopeOf("u1"), TagKey: testTagKey}).Build()
			require.NoError(t, err)
			sink := &cacheSink{}
			out, err := agent.AsRunnable().Invoke(observe.WithObserver(context.Background(), sink), "问题")
			require.NoError(t, err)
			require.Equal(t, "答案", out.Result)
			require.Equal(t, 1, model.calls())
			e, ok := sink.find(tc.event)
			require.True(t, ok, "缺少 %s 事件", tc.event)
			require.Equal(t, "execution", e.ErrorKind)
		})
	}
}

// 命中、未命中、写入失败、写入放弃各发出一条可区分的观测事件，model 事件带 Cached 标记。
func TestModelCacheObserve(t *testing.T) {
	ctx := memory.WithNamespace(context.Background(), "t1", "alice")
	newAgent := func(t *testing.T, c *countingCache, model *cacheModel) *ConfigurableAgent[*core.AgentState] {
		agent, err := NewSimpleBuilder(model).WithSessionID("s").WithTools(plainTool()).
			WithModelCache(ModelCache{Cache: c, TTL: time.Hour, Scope: scopeOf("u1"), TagKey: testTagKey}).Build()
		require.NoError(t, err)
		return agent
	}
	cases := []struct {
		name   string
		run    func(t *testing.T, sink *cacheSink)
		event  string
		cached bool
	}{
		{name: "未命中", event: "model_cache_miss", run: func(t *testing.T, sink *cacheSink) {
			_, err := newAgent(t, newCountingCache(), newCacheModel("dep", textTurn("答"))).AsRunnable().Invoke(observe.WithObserver(ctx, sink), "问题")
			require.NoError(t, err)
		}},
		{name: "命中", event: "model_cache_hit", cached: true, run: func(t *testing.T, sink *cacheSink) {
			c := newCountingCache()
			_, err := newAgent(t, c, newCacheModel("dep", textTurn("答"))).AsRunnable().Invoke(ctx, "问题")
			require.NoError(t, err)
			_, err = newAgent(t, c, newCacheModel("dep")).AsRunnable().Invoke(observe.WithObserver(ctx, sink), "问题")
			require.NoError(t, err)
		}},
		{name: "写入失败", event: "model_cache_write_error", run: func(t *testing.T, sink *cacheSink) {
			c := newCountingCache()
			c.setErr = errors.New("后端不可用")
			_, err := newAgent(t, c, newCacheModel("dep", textTurn("答"))).AsRunnable().Invoke(observe.WithObserver(ctx, sink), "问题")
			require.NoError(t, err)
		}},
		{name: "写入因版本复核放弃", event: "model_cache_write_skipped", run: func(t *testing.T, sink *cacheSink) {
			m, _ := newTestMemory(t, ctx)
			_, err := m.Remember(ctx, memory.Record{ID: "r1", Content: "无关的旧事", Importance: 0.1})
			require.NoError(t, err)
			model := newCacheModel("dep", textTurn("答"))
			model.block = func(context.Context) { require.NoError(t, m.Forget(ctx, "r1")) }
			_, err = memoryAgent(t, m, ModelCache{Cache: newCountingCache(), TTL: time.Minute, TagKey: testTagKey}, model).AsRunnable().Invoke(observe.WithObserver(ctx, sink), "问题")
			require.NoError(t, err)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &cacheSink{}
			tc.run(t, sink)
			_, ok := sink.find(tc.event)
			require.True(t, ok, "缺少 %s 事件", tc.event)
			end, ok := sink.find("model_end")
			require.True(t, ok)
			require.Equal(t, tc.cached, end.Cached, "model 事件的 Cached 标记")
		})
	}
}

// 命中的工具调用回合与之前一轮（中止）有相同的工具调用 ID：会话日志按 ID 索引的工具元数据只取本轮的结果，不会串用。
func TestModelCacheToolCallIDs(t *testing.T) {
	log := session.NewMemoryStore()
	c := newCountingCache()
	runs := 0
	lookup := tools.NewBaseTool("lookup", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		runs++
		return &interfaces.ToolOutput{Success: true, Result: "found"}, nil
	})
	model := newCacheModel("dep")
	model.reply = func(n int) ([]*llm.StreamChunk, error) {
		switch n {
		case 0:
			return toolTurn("c1", "lookup", "{}"), nil
		case 1:
			return nil, errors.New("模型不可用")
		default:
			return textTurn("完成"), nil
		}
	}
	agent, err := NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(log).WithTools(lookup).
		WithModelCache(ModelCache{Cache: c, TTL: time.Hour, Scope: scopeOf("u1"), TagKey: testTagKey}).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "查一下")
	require.Error(t, err, "第一轮在第二步失败并中止")
	out, err := agent.AsRunnable().Invoke(context.Background(), "查一下")
	require.NoError(t, err)
	require.Equal(t, "完成", out.Result)
	require.Equal(t, 3, model.calls(), "第二轮第一步命中")
	require.Equal(t, int64(1), out.Metadata["model_cache_hits"])
	require.Equal(t, 2, runs)

	events := sessionEvents(t, log, "s")
	var results []session.MessageData
	for _, ev := range events {
		if ev.Type == constants.SessionEventToolResult {
			var d session.MessageData
			require.NoError(t, json.Unmarshal(ev.Data, &d))
			results = append(results, d)
		}
	}
	require.Len(t, results, 2, "两轮各记一条 c1 的工具结果")
	for _, d := range results {
		require.Equal(t, "c1", d.Message.ToolCallID)
		require.Equal(t, &session.ToolMeta{Name: "lookup", Success: true}, d.Tool)
	}
	history, err := session.DeriveMessages(events)
	require.NoError(t, err)
	var ids []string
	for _, msg := range history {
		if msg.Role == "tool" {
			ids = append(ids, msg.ToolCallID)
		}
	}
	require.Equal(t, []string{"c1"}, ids, "中止轮次的消息不进入历史，c1 只出现一次")
}

// RequestError 钩子替换客户端后，新的调用按新客户端的身份计算键。
func TestModelCacheReplannedClient(t *testing.T) {
	c := newCountingCache()
	mc := ModelCache{Cache: c, TTL: time.Hour, Scope: scopeOf("u1"), TagKey: testTagKey}
	down := errors.New("主模型不可用")
	primary := newCacheModel("primary")
	primary.reply = func(n int) ([]*llm.StreamChunk, error) {
		if n == 0 {
			return nil, down
		}
		return textTurn("主模型的答案"), nil
	}
	backup := newCacheModel("backup", textTurn("备用模型的答案"))
	switchClient := func(_ context.Context, f *RequestFailure) (RequestErrorDecision, error) {
		return RequestErrorDecision{Action: RequestRetry, Client: backup}, nil
	}
	agent, err := NewSimpleBuilder(primary).WithSessionID("s").WithTools(plainTool()).WithRequestError(1, switchClient).WithModelCache(mc).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "问题")
	require.NoError(t, err)
	require.Equal(t, "备用模型的答案", out.Result)
	require.Equal(t, 1, backup.calls())

	// 主模型恢复后不会取到备用模型的条目。
	out, err = agent.AsRunnable().Invoke(context.Background(), "问题")
	require.NoError(t, err)
	require.Equal(t, "主模型的答案", out.Result)
	require.Equal(t, 2, primary.calls())

	again, err := NewSimpleBuilder(backup).WithSessionID("s").WithTools(plainTool()).WithModelCache(mc).Build()
	require.NoError(t, err)
	out, err = again.AsRunnable().Invoke(context.Background(), "问题")
	require.NoError(t, err)
	require.Equal(t, "备用模型的答案", out.Result)
	require.Equal(t, 1, backup.calls(), "备用模型的条目按备用模型的身份命中")
}

// DeleteSession 删除该会话的条目，其他会话不受影响。
func TestModelCacheRunDeleteSession(t *testing.T) {
	c := newCountingCache()
	mc := ModelCache{Cache: c, TTL: time.Hour, Scope: scopeOf("u1"), TagKey: testTagKey}
	for _, id := range []string{"s1", "s2"} {
		agent, err := NewSimpleBuilder(newCacheModel("dep", textTurn("答案"))).WithSessionID(id).WithModelCache(mc).Build()
		require.NoError(t, err)
		_, err = agent.AsRunnable().Invoke(context.Background(), "问题")
		require.NoError(t, err)
	}
	require.EqualValues(t, 2, c.size())
	require.NoError(t, mc.DeleteSession(context.Background(), "s1"))
	require.EqualValues(t, 1, c.size())
	model := newCacheModel("dep", textTurn("答案"))
	agent, err := NewSimpleBuilder(model).WithSessionID("s2").WithModelCache(mc).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "问题")
	require.NoError(t, err)
	require.Zero(t, model.calls(), "其他会话的条目仍命中")
}
