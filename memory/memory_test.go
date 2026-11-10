package memory

import (
	"context"
	"errors"
	"hash/fnv"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	storemem "github.com/kart-io/goagent/store/memory"
)

// bigramEmbedder 把字符对哈希到固定维度，文本重合越多向量越接近。
type bigramEmbedder struct{ fail error }

func (e bigramEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if e.fail != nil {
		return nil, e.fail
	}
	v := make([]float32, 64)
	for g := range bigrams(text) {
		h := fnv.New32a()
		h.Write([]byte(g))
		v[h.Sum32()%64]++
	}
	v[63] += 0.01 // 避免空文本得到零向量
	return v, nil
}

// scriptedModel 按系统提示词区分抽取、整理、教训与反思请求。
type scriptedModel struct {
	reply func(system, user string) (string, error)
	calls []string
}

func (m *scriptedModel) Provider() constants.Provider { return constants.Provider("fake") }
func (m *scriptedModel) CompleteStream(_ context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	system, user := req.Messages[0].Content, req.Messages[1].Content
	m.calls = append(m.calls, system)
	text, err := m.reply(system, user)
	if err != nil {
		return nil, err
	}
	ch := make(chan *llm.StreamChunk, 2)
	ch <- &llm.StreamChunk{Delta: text, Content: text}
	ch <- &llm.StreamChunk{Done: true, Content: text, FinishReason: "stop"}
	close(ch)
	return ch, nil
}

func newMemory(t *testing.T, cfg Config) *Memory {
	t.Helper()
	if cfg.Store == nil {
		cfg.Store = storemem.New()
	}
	m, err := New(cfg)
	require.NoError(t, err)
	return m
}

func vectorMemory(t *testing.T) *Memory {
	return newMemory(t, Config{Index: NewMemoryIndex(), Embedder: bigramEmbedder{}, EmbedModel: "bigram"})
}

func contents(rs []Scored) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Content
	}
	return out
}

func TestRecallIsolatesNamespacesAndRanks(t *testing.T) {
	for name, m := range map[string]*Memory{"仅文本": newMemory(t, Config{}), "向量": vectorMemory(t)} {
		t.Run(name, func(t *testing.T) {
			alice, bob := WithNamespace(context.Background(), "t1", "alice"), WithNamespace(context.Background(), "t1", "bob")
			for _, c := range []struct {
				ctx     context.Context
				content string
				kind    Kind
			}{
				{alice, "用户喜欢喝美式咖啡", KindFact},
				{alice, "用户在上海工作", KindFact},
				{alice, "上次帮用户订了咖啡豆", KindEpisode},
				{bob, "用户喜欢喝绿茶", KindFact},
			} {
				_, err := m.Remember(c.ctx, Record{Kind: c.kind, Content: c.content, Importance: 0.5})
				require.NoError(t, err)
			}
			got, err := m.Recall(alice, Query{Text: "喜欢喝什么咖啡"})
			require.NoError(t, err)
			require.NotEmpty(t, got)
			require.Equal(t, "用户喜欢喝美式咖啡", got[0].Content)
			require.NotContains(t, contents(got), "用户喜欢喝绿茶", "不能召回其他命名空间")

			facts, err := m.Recall(alice, Query{Text: "咖啡", Kinds: []Kind{KindEpisode}})
			require.NoError(t, err)
			require.Equal(t, []string{"上次帮用户订了咖啡豆"}, contents(facts))

			require.NoError(t, m.Forget(alice, got[0].ID))
			got, err = m.Recall(alice, Query{Text: "喜欢喝什么咖啡"})
			require.NoError(t, err)
			require.NotContains(t, contents(got), "用户喜欢喝美式咖啡", "失效记录不再召回")
		})
	}
}

func TestMemoryRequiresNamespace(t *testing.T) {
	m := newMemory(t, Config{})
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"写入": func() error { _, err := m.Remember(ctx, Record{Content: "x", Importance: 0.1}); return err },
		"召回": func() error { _, err := m.Recall(ctx, Query{Text: "x"}); return err },
		"空段": func() error { _, err := m.Recall(WithNamespace(ctx, "t", " "), Query{}); return err },
	} {
		t.Run(name, func(t *testing.T) { require.True(t, agentErrors.IsCode(call(), agentErrors.CodeInvalidInput)) })
	}
}

func TestNewValidatesConfig(t *testing.T) {
	for name, cfg := range map[string]Config{
		"缺少存储":   {},
		"索引缺少嵌入": {Store: storemem.New(), Index: NewMemoryIndex()},
		"嵌入缺少索引": {Store: storemem.New(), Embedder: bigramEmbedder{}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := New(cfg)
			require.Error(t, err)
		})
	}
}

func TestMemoryIndex(t *testing.T) {
	ctx := context.Background()
	ns := []string{"t"}
	tests := []struct {
		name string
		run  func(*MemoryIndex) error
	}{
		{"零向量", func(x *MemoryIndex) error { return x.Upsert(ctx, ns, "a", []float32{0, 0}) }},
		{"非有限值", func(x *MemoryIndex) error { return x.Upsert(ctx, ns, "a", []float32{float32(math.NaN()), 1}) }},
		{"维度不一致", func(x *MemoryIndex) error {
			if err := x.Upsert(ctx, ns, "a", []float32{1, 0}); err != nil {
				return err
			}
			return x.Upsert(ctx, ns, "b", []float32{1, 0, 0})
		}},
		{"没有命名空间", func(x *MemoryIndex) error { return x.Upsert(ctx, nil, "a", []float32{1}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { require.Error(t, tt.run(NewMemoryIndex())) })
	}

	x := NewMemoryIndex()
	big := []float32{3e38, 3e38}
	require.NoError(t, x.Upsert(ctx, ns, "big", big))
	big[0] = -1 // 修改输入不影响索引
	require.NoError(t, x.Upsert(ctx, ns, "other", []float32{1, -1}))
	hits, err := x.Search(ctx, ns, []float32{1, 1}, 5)
	require.NoError(t, err)
	require.Equal(t, "big", hits[0].ID)
	require.InDelta(t, 1.0, hits[0].Score, 1e-9, "大幅值向量的自相似度为 1")
	hits, err = x.Search(ctx, ns, []float32{1, 1}, -1)
	require.NoError(t, err)
	require.Empty(t, hits)
	hits, err = x.Search(ctx, []string{"other"}, []float32{1, 1}, 5)
	require.NoError(t, err)
	require.Empty(t, hits)
}

func TestRecallBlockPinnedFirstWithinBudget(t *testing.T) {
	m := newMemory(t, Config{RecallRunes: 120})
	ctx := WithNamespace(context.Background(), "u")
	_, err := m.Remember(ctx, Record{Content: "用户叫小明", Pinned: true, Importance: 1})
	require.NoError(t, err)
	_, err = m.Remember(ctx, Record{Content: "用户喜欢咖啡", Importance: 0.5})
	require.NoError(t, err)
	_, err = m.Remember(ctx, Record{Content: "用户喜欢咖啡" + strings.Repeat("长", 80), Importance: 0.5})
	require.NoError(t, err)
	block, err := m.RecallBlock(ctx, "咖啡")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(block, RecallPrefix))
	require.Less(t, strings.Index(block, "小明"), strings.Index(block, "咖啡"), "核心记忆在前")
	require.NotContains(t, block, "长长", "超出预算的记录不放入")

	empty, err := newMemory(t, Config{}).RecallBlock(ctx, "咖啡")
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestPruneRetention(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := newMemory(t, Config{Retention: Retention{TTL: map[Kind]time.Duration{KindEpisode: 24 * time.Hour}, MaxRecords: 2, InvalidRetention: time.Hour}})
	now := start
	m.now = func() time.Time { return now }
	ctx := WithNamespace(context.Background(), "u")
	for _, r := range []Record{
		{ID: "episode", Kind: KindEpisode, Content: "做过的事", Importance: 0.9},
		{ID: "low", Content: "不重要", Importance: 0.1},
		{ID: "mid", Content: "一般", Importance: 0.5},
		{ID: "high", Content: "重要", Importance: 0.9},
		{ID: "pinned", Content: "核心", Importance: 0, Pinned: true},
	} {
		_, err := m.Remember(ctx, r)
		require.NoError(t, err)
	}
	now = start.Add(48 * time.Hour)
	require.NoError(t, m.Prune(ctx))
	valid := map[string]bool{}
	for _, id := range []string{"episode", "low", "mid", "high", "pinned"} {
		r, err := m.Get(ctx, id)
		require.NoError(t, err)
		valid[id] = r.Valid()
	}
	require.Equal(t, map[string]bool{"episode": false, "low": false, "mid": true, "high": true, "pinned": true}, valid,
		"过期的情景失效；超出容量时淘汰得分最低的未钉住记录")

	now = now.Add(2 * time.Hour)
	require.NoError(t, m.Prune(ctx))
	_, err := m.Get(ctx, "low")
	require.True(t, agentErrors.IsCode(err, agentErrors.CodeStoreNotFound), "失效超过保留期后删除")
}

func TestPurgeAndReindex(t *testing.T) {
	x := NewMemoryIndex()
	m := newMemory(t, Config{Index: x, Embedder: bigramEmbedder{}, EmbedModel: "v1"})
	ctx := WithNamespace(context.Background(), "u")
	r, err := m.Remember(ctx, Record{Content: "用户喜欢咖啡", Importance: 0.5})
	require.NoError(t, err)

	m2 := newMemory(t, Config{Store: m.cfg.Store, Index: NewMemoryIndex(), Embedder: bigramEmbedder{}, EmbedModel: "v2"})
	require.NoError(t, m2.Reindex(ctx, false))
	got, err := m2.Get(ctx, r.ID)
	require.NoError(t, err)
	require.Equal(t, "v2", got.EmbedModel)
	hits, err := m2.cfg.Index.Search(ctx, []string{"u"}, mustEmbed(t, "咖啡"), 1)
	require.NoError(t, err)
	require.Len(t, hits, 1, "重建后可向量检索")

	require.NoError(t, m.Purge(ctx))
	all, err := m.records.List(ctx, []string{"u"})
	require.NoError(t, err)
	require.Empty(t, all)
	hits, err = x.Search(ctx, []string{"u"}, mustEmbed(t, "咖啡"), 1)
	require.NoError(t, err)
	require.Empty(t, hits)
}

// 嵌入失败时不写入正文，避免留下无法向量检索的记录。
func TestRememberFailsWhenEmbeddingFails(t *testing.T) {
	m := newMemory(t, Config{Index: NewMemoryIndex(), Embedder: bigramEmbedder{fail: errors.New("嵌入服务不可用")}})
	ctx := WithNamespace(context.Background(), "u")
	_, err := m.Remember(ctx, Record{ID: "x", Content: "内容", Importance: 0.5})
	require.ErrorContains(t, err, "嵌入服务不可用")
	_, err = m.Get(ctx, "x")
	require.True(t, agentErrors.IsCode(err, agentErrors.CodeStoreNotFound))
}

func mustEmbed(t *testing.T, text string) []float32 {
	v, err := bigramEmbedder{}.Embed(context.Background(), text)
	require.NoError(t, err)
	return v
}

// appendTurn 写入一轮对话，返回 turn/start 与 turn/end 的序号。
func appendTurn(t *testing.T, s session.Store, id string, aborted bool, msgs ...llm.Message) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	rec, _, err := session.OpenRecorder(ctx, s, id)
	require.NoError(t, err)
	start, err := session.NewEvent(constants.SessionEventTurnStart, session.TurnStart{Input: msgs[0].Content})
	require.NoError(t, err)
	_, err = rec.Append(ctx, start)
	require.NoError(t, err)
	from := rec.Seq()
	derived, err := session.DeriveMessages(rec.Events())
	require.NoError(t, err)
	require.NoError(t, rec.Sync(ctx, append(derived, msgs...), nil))
	end, err := session.NewEvent(constants.SessionEventTurnEnd, session.TurnEnd{Aborted: aborted})
	require.NoError(t, err)
	if aborted {
		end.Surface = &session.Surface{Start: from, End: rec.Seq()}
	}
	_, err = rec.Append(ctx, end)
	require.NoError(t, err)
	return from, rec.Seq()
}

func TestWorkerExtractsAndSupersedes(t *testing.T) {
	sessions := session.NewMemoryStore()
	model := &scriptedModel{reply: func(system, user string) (string, error) {
		switch {
		case strings.HasPrefix(system, extractPrompt[:10]) && strings.Contains(user, "上海"):
			return "```json\n{\"memories\":[{\"kind\":\"fact\",\"content\":\"用户住在上海\",\"importance\":0.8,\"keywords\":[\"住址\"]}]}\n```", nil
		case strings.HasPrefix(system, extractPrompt[:10]):
			return `{"memories":[{"kind":"fact","content":"用户住在北京","importance":0.8,"keywords":["住址"]}]}`, nil
		case system == reconcilePrompt:
			id := strings.TrimSpace(strings.SplitN(strings.SplitN(user, "id=", 2)[1], "：", 2)[0])
			return `{"op":"invalidate","target":"` + id + `"}`, nil
		}
		return "", errors.New("意外的请求")
	}}
	m := newMemory(t, Config{Model: model, Sessions: sessions})
	ctx := WithNamespace(context.Background(), "u")

	from, to := appendTurn(t, sessions, "s", false, llm.UserMessage("我住在上海"), llm.Message{Role: "assistant", Content: "好的"})
	job := Job{Key: "s/1", Namespace: []string{"u"}, Session: "s", From: from, To: to}
	require.NoError(t, m.Enqueue(ctx, job))
	n, err := m.ProcessPending(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	old, err := m.Recall(ctx, Query{Text: "住在哪里"})
	require.NoError(t, err)
	require.Equal(t, []string{"用户住在上海"}, contents(old))
	require.Equal(t, Source{Session: "s", From: from, To: to, Job: "s/1"}, old[0].Source)

	require.NoError(t, m.Enqueue(ctx, job), "重复登记不产生新任务")
	n, err = m.ProcessPending(ctx)
	require.NoError(t, err)
	require.Zero(t, n)

	from, to = appendTurn(t, sessions, "s", false, llm.UserMessage("我搬到北京了"), llm.Message{Role: "assistant", Content: "记下了"})
	require.NoError(t, m.Enqueue(ctx, Job{Key: "s/2", Namespace: []string{"u"}, Session: "s", From: from, To: to}))
	_, err = m.ProcessPending(ctx)
	require.NoError(t, err)
	now, err := m.Recall(ctx, Query{Text: "住在哪里"})
	require.NoError(t, err)
	require.Equal(t, []string{"用户住在北京"}, contents(now))
	superseded, err := m.Get(ctx, old[0].ID)
	require.NoError(t, err)
	require.False(t, superseded.Valid())
	require.Equal(t, now[0].ID, superseded.SupersededBy, "旧事实失效并指向新事实")

	done, ok, err := m.Job(ctx, "s/2")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, JobDone, done.Status)
}

func TestWorkerSkipsRewoundAndLearnsFromAborted(t *testing.T) {
	sessions := session.NewMemoryStore()
	model := &scriptedModel{reply: func(system, user string) (string, error) {
		if system == failurePrompt {
			require.Contains(t, user, "退款")
			return `{"lesson":"退款前先确认订单状态","importance":0.7}`, nil
		}
		return "", errors.New("被退回的轮次不应请求模型")
	}}
	m := newMemory(t, Config{Model: model, Sessions: sessions})
	ctx := WithNamespace(context.Background(), "u")

	from, to := appendTurn(t, sessions, "s", false, llm.UserMessage("以后都用英文回答"), llm.Message{Role: "assistant", Content: "OK"})
	_, err := session.Rewind(context.Background(), sessions, "s", from-1, to)
	require.NoError(t, err)
	require.NoError(t, m.Enqueue(ctx, Job{Key: "s/rewound", Namespace: []string{"u"}, Session: "s", From: from, To: to}))

	from, to = appendTurn(t, sessions, "s", true, llm.UserMessage("帮我退款"), llm.Message{Role: "assistant", Content: "调用失败"})
	require.NoError(t, m.Enqueue(ctx, Job{Key: "s/aborted", Namespace: []string{"u"}, Session: "s", From: from, To: to, Aborted: true}))
	_, err = m.ProcessPending(ctx)
	require.NoError(t, err)
	got, err := m.Recall(ctx, Query{Text: "退款"})
	require.NoError(t, err)
	require.Equal(t, []string{"退款前先确认订单状态"}, contents(got))
	require.Equal(t, KindReflection, got[0].Kind)
	all, err := m.records.List(ctx, []string{"u"})
	require.NoError(t, err)
	require.Len(t, all, 1, "被退回的轮次不留下记忆")
}

func TestWorkerRetriesThenFails(t *testing.T) {
	sessions := session.NewMemoryStore()
	m := newMemory(t, Config{Model: &scriptedModel{reply: func(string, string) (string, error) { return "不是 JSON", nil }}, Sessions: sessions, JobAttempts: 2})
	ctx := WithNamespace(context.Background(), "u")
	from, to := appendTurn(t, sessions, "s", false, llm.UserMessage("你好"), llm.Message{Role: "assistant", Content: "你好"})
	require.NoError(t, m.Enqueue(ctx, Job{Key: "k", Namespace: []string{"u"}, Session: "s", From: from, To: to}))
	for _, want := range []JobStatus{JobPending, JobFailed} {
		_, err := m.ProcessPending(ctx)
		require.NoError(t, err)
		j, _, err := m.Job(ctx, "k")
		require.NoError(t, err)
		require.Equal(t, want, j.Status)
		require.Contains(t, j.Error, "JSON")
	}
}

func TestReflectionAfterThreshold(t *testing.T) {
	sessions := session.NewMemoryStore()
	turn := 0
	model := &scriptedModel{reply: func(system, user string) (string, error) {
		switch {
		case strings.HasPrefix(system, extractPrompt[:10]):
			turn++
			return `{"memories":[{"kind":"episode","content":"第` + string(rune('0'+turn)) + `次加班到深夜","importance":0.6}]}`, nil
		case system == reconcilePrompt:
			return `{"op":"add"}`, nil
		case system == reflectPrompt:
			return `{"insights":[{"content":"用户经常加班","importance":0.8}]}`, nil
		}
		return "", errors.New("意外的请求")
	}}
	m := newMemory(t, Config{Model: model, Sessions: sessions, ReflectionThreshold: 1.0})
	ctx := WithNamespace(context.Background(), "u")
	for i := range 2 {
		from, to := appendTurn(t, sessions, "s", false, llm.UserMessage("今天又加班"), llm.Message{Role: "assistant", Content: "辛苦了"})
		require.NoError(t, m.Enqueue(ctx, Job{Key: "s/" + string(rune('a'+i)), Namespace: []string{"u"}, Session: "s", From: from, To: to}))
		_, err := m.ProcessPending(ctx)
		require.NoError(t, err)
	}
	got, err := m.Recall(ctx, Query{Kinds: []Kind{KindReflection}})
	require.NoError(t, err)
	require.Equal(t, []string{"用户经常加班"}, contents(got))
	episodes, err := m.Recall(ctx, Query{Kinds: []Kind{KindEpisode}})
	require.NoError(t, err)
	for _, e := range episodes {
		require.True(t, e.Reflected)
	}
}

func TestRewoundInvalidatesLaterRecords(t *testing.T) {
	m := newMemory(t, Config{})
	ctx := WithNamespace(context.Background(), "u")
	for _, r := range []Record{
		{ID: "early", Content: "早", Importance: 0.5, Source: Source{Session: "s", From: 2}},
		{ID: "late", Content: "晚", Importance: 0.5, Source: Source{Session: "s", From: 9}},
		{ID: "other", Content: "其他会话", Importance: 0.5, Source: Source{Session: "x", From: 9}},
	} {
		_, err := m.Remember(ctx, r)
		require.NoError(t, err)
	}
	require.NoError(t, m.Rewound(ctx, "s", 5))
	for id, valid := range map[string]bool{"early": true, "late": false, "other": true} {
		r, err := m.Get(ctx, id)
		require.NoError(t, err)
		require.Equal(t, valid, r.Valid(), id)
	}
}
