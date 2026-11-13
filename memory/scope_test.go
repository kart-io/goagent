package memory

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// g0DigestFixture 是 a4f09197 对 TestDigestByProtocol 同一输入计算的摘要。
const g0DigestFixture = "d91093a757539d23f9bdc4ef78757367d42f9b1b826ffca349c49ecc98c22b33"

// requireNoRetiring 断言不变量 8：清理完成后 retiring 为空。
func requireNoRetiring(t *testing.T, m *Memory) {
	t.Helper()
	var ret retiringState
	_, _, err := loadJSON(context.Background(), m.cfg.Durable, retiringID, &ret)
	require.NoError(t, err)
	require.Empty(t, ret.Scopes, "retiring 已清空")
}

func retiringScopes(t *testing.T, m *Memory) map[string]retiringEntry {
	t.Helper()
	var ret retiringState
	_, _, err := loadJSON(context.Background(), m.cfg.Durable, retiringID, &ret)
	require.NoError(t, err)
	return ret.Scopes
}

// Purge 在提交前、提交后清理前与清理中途崩溃：提交前崩溃什么都不变；提交后旧数据立即不可读，
// 旧作用域留在 retiring 中，下一轮 Worker 补完物理删除（设计 4.3）。
func TestPurgeFaults(t *testing.T) {
	errCrash := errors.New("模拟崩溃")
	cases := []struct {
		name      string
		fail      func(op, scope, id string, committed bool) bool
		committed bool
	}{
		{"提交前", func(op, _, _ string, _ bool) bool { return op == "saveall" }, false},
		{"提交后清理前", func(op, scope, id string, committed bool) bool {
			return committed && op == "load" && scope == "" && id == retiringID
		}, true},
		{"清理中途（索引已删、作用域未删）", func(op, _, _ string, _ bool) bool { return op == "removescope" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newHookStore(t)
			x := NewMemoryIndex()
			m := durableMemory(t, st, Config{Index: x, Embedder: bigramEmbedder{}, EmbedModel: "v1", RollbackWindow: 1 << 40,
				Model: &scriptedModel{}, Sessions: session.NewMemoryStore()})
			ctx := WithNamespace(context.Background(), testNS...)
			_, err := m.Remember(ctx, Record{ID: "a", Content: "机密：项目代号青鸟", Importance: 0.5})
			require.NoError(t, err)

			var committed, armed atomic.Bool
			armed.Store(true)
			st.h.before = func(op, scope, id string) error {
				if op == "saveall" && !tc.fail(op, scope, id, false) {
					defer committed.Store(true)
				}
				if armed.Load() && tc.fail(op, scope, id, committed.Load()) {
					return errCrash
				}
				return nil
			}
			require.ErrorIs(t, m.Purge(ctx), errCrash)
			armed.Store(false)

			meta, err := m.loadMeta(ctx, testNS)
			require.NoError(t, err)
			_, s, err := m.read(ctx, testNS)
			require.NoError(t, err)
			if !tc.committed {
				require.Zero(t, meta.Purges, "提交前崩溃不切换作用域")
				require.Contains(t, s.Records, "a")
				require.Empty(t, retiringScopes(t, m))
				log, err := m.Deletions(ctx)
				require.NoError(t, err)
				require.Empty(t, log.Purges, "删除日志与 meta 同一次提交")
				return
			}
			require.EqualValues(t, 1, meta.Purges)
			require.Empty(t, s.Records, "提交后旧数据立即不可读")
			require.Contains(t, retiringScopes(t, m), scopeName(testNS, initialScope))

			m.now = func() time.Time { return time.Now().Add(m.cfg.RetireGrace) }
			_, err = m.ProcessPending(ctx)
			require.NoError(t, err, "下一轮 Worker 补完清理")
			requireNoRetiring(t, m)
			_, err = loadHead(ctx, m.scoped(testNS, initialScope))
			require.ErrorIs(t, err, agentErrors.ErrScopeRemoved)
			require.Empty(t, indexKeys(x, testNS))
			checkIndex(t, m, x, testNS)
		})
	}
}

// 跨越 Purge 的抽取任务：登记时作用域已不是当前的直接丢弃；登记后才 Purge 的在处理时丢弃，不写入新作用域。
func TestJobsAcrossPurgeDropped(t *testing.T) {
	sessions := session.NewMemoryStore()
	model := &scriptedModel{reply: func(string, string) (string, error) {
		return "", errors.New("跨越 Purge 的任务不应调用模型")
	}}
	m := newMemory(t, Config{Model: model, Sessions: sessions})
	ctx := WithNamespace(context.Background(), "u")
	turn, err := m.Turn(ctx)
	require.NoError(t, err)
	from, to := appendTurn(t, sessions, "s", false, llm.UserMessage("我住在上海"), llm.Message{Role: "assistant", Content: "好的"})
	job := func(key, scope string) Job {
		return Job{Key: key, Namespace: []string{"u"}, Session: "s", Scope: scope, TurnSeq: turn.Seq, From: from, To: to}
	}
	require.NoError(t, m.Enqueue(ctx, job("before", turn.Scope)))
	require.NoError(t, m.Purge(ctx))

	for _, tc := range []struct{ key, scope string }{{"old", turn.Scope}, {"g0", ""}} {
		require.NoError(t, m.Enqueue(ctx, job(tc.key, tc.scope)), "回放会再次登记，丢弃时不报错")
		_, ok, err := m.Job(ctx, tc.key)
		require.NoError(t, err)
		require.False(t, ok, "%s 未登记", tc.key)
	}
	require.EqualValues(t, 2, m.droppedJobs.Load())

	n, err := m.ProcessPending(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	j, _, err := m.Job(ctx, "before")
	require.NoError(t, err)
	require.Equal(t, JobDropped, j.Status)
	require.EqualValues(t, 3, m.droppedJobs.Load())
	require.Empty(t, model.calls)
	_, s, err := m.read(ctx, []string{"u"})
	require.NoError(t, err)
	require.Empty(t, s.Records, "新作用域没有收到旧轮次的抽取结果")
}

// memory_save 写入本轮绑定的作用域：轮次开始后发生的 Purge 使保存成为工具错误；G0 绑定只在从未清空时可用。
func TestMemorySaveScope(t *testing.T) {
	cases := []struct {
		name    string
		bind    func(t *testing.T, m *Memory, ctx context.Context) (context.Context, bool)
		purge   bool // 在取得绑定之后 Purge
		wantErr bool
	}{
		{"没有绑定", func(*testing.T, *Memory, context.Context) (context.Context, bool) { return nil, false }, true, false},
		{"绑定当前作用域", bindTurn, false, false},
		{"绑定已清空的作用域", bindTurn, true, true},
		{"G0 绑定且从未清空", bindG0, false, false},
		{"G0 绑定且清空过", bindG0, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMemory(t, Config{})
			ctx := WithNamespace(context.Background(), testNS...)
			bound, ok := tc.bind(t, m, ctx)
			if tc.purge {
				require.NoError(t, m.Purge(ctx))
			}
			if ok {
				ctx = bound
			}
			_, err := saveTool(m).Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"content": "用户喜欢喝茶"}})
			_, s, rerr := m.read(ctx, testNS)
			require.NoError(t, rerr)
			if tc.wantErr {
				require.True(t, agentErrors.IsCode(err, agentErrors.CodeConflict), "作用域改变转为工具错误：%v", err)
				require.Empty(t, s.Records, "不写入新作用域")
				return
			}
			require.NoError(t, err)
			require.Len(t, s.Records, 1, "写入当前作用域")
		})
	}
}

func bindTurn(t *testing.T, m *Memory, ctx context.Context) (context.Context, bool) {
	turn, err := m.Turn(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, turn.Scope)
	return WithTurn(ctx, turn), true
}

func bindG0(_ *testing.T, _ *Memory, ctx context.Context) (context.Context, bool) {
	return WithTurn(ctx, TurnMemory{Write: testNS, Read: [][]string{testNS}}), true
}

func saveTool(m *Memory) interfaces.Tool {
	for _, tool := range m.Tools() {
		if tool.Name() == constants.ToolMemorySave {
			return tool
		}
	}
	panic("没有 memory_save")
}

// Hydrate 不还原版本已变化的引用：正文被修改、同 ID 被重建、命名空间被清空。
func TestHydrateDropsChangedRefs(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, m *Memory, ctx context.Context)
	}{
		{"修改正文", func(t *testing.T, m *Memory, ctx context.Context) {
			content := "用户改喝绿茶"
			_, err := m.Update(ctx, "a", 1, Patch{Content: &content})
			require.NoError(t, err)
		}},
		{"同 ID 重建", func(t *testing.T, m *Memory, ctx context.Context) {
			_, err := m.mutate(ctx, testNS, "", func(t *tx) error {
				r, _ := t.Records.Get("a")
				t.Records.Delete("a")
				createRecord(t, r)
				return nil
			})
			require.NoError(t, err)
		}},
		{"清空后重写同一内容", func(t *testing.T, m *Memory, ctx context.Context) {
			require.NoError(t, m.Purge(ctx))
			_, err := m.Remember(ctx, Record{ID: "a", Content: "用户喜欢喝红茶", Importance: 0.5})
			require.NoError(t, err)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMemory(t, Config{})
			ctx := WithNamespace(context.Background(), testNS...)
			_, err := m.Remember(ctx, Record{ID: "a", Content: "用户喜欢喝红茶", Importance: 0.5})
			require.NoError(t, err)
			snap, err := m.Snapshot(ctx, "红茶")
			require.NoError(t, err)
			require.Len(t, snap.Refs, 1)
			_, adopted, err := m.Hydrate(ctx, snap, snap.Refs)
			require.NoError(t, err)
			require.Equal(t, snap.Refs, adopted, "未变化时还原")

			tc.change(t, m, ctx)
			got, adopted, err := m.Hydrate(ctx, snap, snap.Refs)
			require.NoError(t, err)
			require.Empty(t, adopted)
			require.Empty(t, got.Records)
		})
	}
}

// memory-recall/1 快照的摘要只按 G0 的三字段引用编码：作用域与版本字段不影响它，与 G0 逐字节相同。
func TestDigestByProtocol(t *testing.T) {
	turn := TurnMemory{Write: testNS, Read: [][]string{testNS}}
	refs := []Ref{{Kind: RefRecord, Namespace: testNS, ID: "a"}}
	v1 := Snapshot{Protocol: recallProtocolV1, Identity: "id", Turn: turn}
	g0, err := v1.Digest(refs)
	require.NoError(t, err)
	require.Equal(t, g0DigestFixture, g0, "与 a4f09197 计算的摘要相同")

	versioned := []Ref{{Kind: RefRecord, Namespace: testNS, ID: "a", Scope: "0", Incarnation: 3, ContentRev: 2}}
	got, err := v1.Digest(versioned)
	require.NoError(t, err)
	require.Equal(t, g0, got, "/1 不编码版本字段")

	bound := v1
	bound.Turn.Scope, bound.Turn.Seq = "0", 7
	got, err = bound.Digest(refs)
	require.NoError(t, err)
	require.Equal(t, g0, got, "绑定的作用域与序号不进入摘要")

	v2 := Snapshot{Protocol: RecallProtocol, Identity: "id", Turn: turn}
	a, err := v2.Digest(refs)
	require.NoError(t, err)
	b, err := v2.Digest(versioned)
	require.NoError(t, err)
	require.NotEqual(t, a, b, "/2 编码版本字段")
	require.NotEqual(t, g0, a, "协议进入摘要")
}

// Purge 与 Remember、Worker 并发：Purge 返回后旧作用域已删除，没有写入落在它上面；所有成功的写入都在当前作用域。
func TestPurgeConcurrentWithWrites(t *testing.T) {
	sessions := session.NewMemoryStore()
	model := &scriptedModel{reply: func(system, user string) (string, error) {
		if system == reconcilePrompt {
			return `{"op":"add"}`, nil
		}
		return `{"memories":[{"kind":"fact","content":"用户住在北京","importance":0.8}]}`, nil
	}}
	x := NewMemoryIndex()
	m := newMemory(t, Config{Index: x, Embedder: bigramEmbedder{}, EmbedModel: "v1", Model: model, Sessions: sessions})
	ctx := WithNamespace(context.Background(), testNS...)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var writeErrs []error
	record := func(err error) {
		if err != nil && !errors.Is(err, errScopeChanged) {
			mu.Lock()
			writeErrs = append(writeErrs, err)
			mu.Unlock()
		}
	}
	for w := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 20 {
				_, err := m.Remember(ctx, Record{Content: "并发写入", Importance: 0.5})
				record(err)
				turn, err := m.Turn(ctx)
				record(err)
				if err != nil {
					continue
				}
				sid := "s" + string(rune('a'+w))
				from, to := appendTurn(t, sessions, sid, false, llm.UserMessage("我住在北京"))
				record(m.Enqueue(ctx, Job{Key: sid + "/" + string(rune('a'+i)), Namespace: testNS, Session: sid,
					Scope: turn.Scope, TurnSeq: turn.Seq, From: from, To: to}))
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 10 {
			_, err := m.ProcessPending(ctx)
			record(err)
		}
	}()
	for range 3 {
		require.NoError(t, m.Purge(ctx))
		meta, err := m.loadMeta(ctx, testNS)
		require.NoError(t, err)
		_, err = loadHead(ctx, m.scoped(testNS, initialScope))
		require.ErrorIs(t, err, agentErrors.ErrScopeRemoved)
		require.NotEqual(t, initialScope, meta.Current)
	}
	wg.Wait()
	require.Empty(t, writeErrs)

	m.now = func() time.Time { return time.Now().Add(m.cfg.RetireGrace) }
	_, err := m.ProcessPending(ctx)
	require.NoError(t, err)
	requireNoRetiring(t, m)
	checkIndex(t, m, x, testNS)
	meta, err := m.loadMeta(ctx, testNS)
	require.NoError(t, err)
	require.EqualValues(t, 3, meta.Purges)
	scope, _, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.Equal(t, meta.Current, scope)
}
