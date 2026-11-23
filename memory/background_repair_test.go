package memory

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func TestBackgroundStoreTimeoutLeavesWindowUsable(t *testing.T) {
	st := newHookStore(t)
	b := maintenanceConfig(t, st, "store-deadline", 10)
	model := &scriptedModel{reply: func(string, string) (string, error) { return "{}", nil }}
	now := time.Now()
	m := workerMemory(t, Config{Durable: st, Background: b, Model: model, Now: func() time.Time { return now }, ReflectionThreshold: .5})
	ctx := WithNamespace(context.Background(), testNS...)
	_, err := m.Remember(ctx, Record{ID: "r", Kind: KindEpisode, Content: "经历", Importance: 1})
	require.NoError(t, err)
	st.h.before = func(op, scope, id string) error {
		if op == "load" && strings.HasPrefix(id, "goagent/budget/") {
			return context.DeadlineExceeded
		}
		return nil
	}
	_, err = m.ProcessPending(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	_, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	w := s.Derivations["reflection"]
	require.Empty(t, w.BlockedBudgetRootKey)
	require.Equal(t, 1, w.Attempts)
	require.Empty(t, model.calls)
	st.h.before = nil
	now = w.RetryAt
	_, err = m.ProcessPending(ctx)
	require.NoError(t, err)
	require.Len(t, model.calls, 1)
}

type lateCompletion struct{}

func (lateCompletion) Provider() constants.Provider { return "fake" }
func (lateCompletion) CompleteStream(ctx context.Context, _ *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	<-ctx.Done()
	ch := make(chan *llm.StreamChunk, 2)
	ch <- &llm.StreamChunk{Delta: "迟到片段"}
	ch <- &llm.StreamChunk{Done: true, Content: `{"result":"迟到"}`, Usage: &llm.Usage{TotalTokens: 8, PromptTokens: 6, CompletionTokens: 2}}
	close(ch)
	return ch, nil
}

type lateVector struct{}

func (lateVector) Embed(ctx context.Context, _ string) ([]float32, error) {
	<-ctx.Done()
	return []float32{1, 0}, nil
}
func TestMaintenanceDeadlineDiscardsLateSuccess(t *testing.T) {
	for _, kind := range []string{"chat", "embedding"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				st := newHookStore(t)
				b := maintenanceConfig(t, st, "initial", 4)
				c := b.Budget.Configuration()
				c.RootKey = "expires"
				c.DeadlineUnixMilli = time.Now().Add(time.Second).UnixMilli()
				var err error
				b.Budget, err = budget.NewDurable(c, st)
				require.NoError(t, err)
				idx := NewMemoryIndex()
				m := newMemory(t, Config{Durable: st, Background: b, Index: idx, Model: lateCompletion{}, Embedder: lateVector{}})
				ctx := m.maintenanceContext(context.Background(), testNS, initialScope)
				if kind == "chat" {
					var out struct{ Result string }
					err = m.chatJSON(ctx, "sys", "user", &out)
					require.Empty(t, out.Result)
				} else {
					_, err = m.mutate(ctx, testNS, initialScope, func(x *tx) error {
						x.Records.Put("k", Record{ID: "k", Kind: KindFact, Namespace: testNS, Content: "文本", Incarnation: 1, Revision: 1, ContentRev: 1})
						return nil
					})
					require.NoError(t, err)
					err = m.Reindex(ctx, false)
					ids, e := idx.IDs(ctx, recordSpace.namespace(testNS, initialScope))
					require.NoError(t, e)
					require.Empty(t, ids)
				}
				require.ErrorIs(t, err, errBackgroundBudget)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				snap, e := b.Budget.Snapshot(ctx)
				require.NoError(t, e)
				require.Equal(t, 1, snap.ReservedRequests)
				if kind == "chat" {
					require.Equal(t, 8, snap.KnownTokens.TotalTokens)
				} else {
					require.Equal(t, 1, snap.UnknownUsageEntries)
				}
			})
		})
	}
}
func TestMaintenanceParentCancellationKeepsWindowUsable(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	call, stop := context.WithCancel(parent)
	defer stop()
	cancel()
	<-call.Done()
	err := windowDeadline(parent, call, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, errBackgroundBudget)
}
func indexQueued(t *testing.T, m *Memory) bool {
	t.Helper()
	entries, err := m.queued(context.Background())
	require.NoError(t, err)
	return slices.ContainsFunc(entries, func(e queueEntry) bool { return e.Reason == queueIndex && slices.Equal(e.Namespace, testNS) })
}
func TestReflectionUpsertFailureRepairedWithoutJobs(t *testing.T) {
	st := newHookStore(t)
	b := maintenanceConfig(t, st, "reflection-write", 10)
	base := NewMemoryIndex()
	var fail atomic.Bool
	idx := hookIndex{Index: base, before: func(op string, _ []string, id string) error {
		if op == "upsert" && fail.Load() {
			return errors.New("索引写入失败")
		}
		return nil
	}}
	m := workerMemory(t, Config{Durable: st, Background: b, Model: &scriptedModel{reply: func(string, string) (string, error) { return `{"insights":[{"content":"一条反思"}]}`, nil }}, Index: idx, Embedder: bigramEmbedder{}, ReflectionThreshold: .5})
	ctx := WithNamespace(context.Background(), testNS...)
	_, err := m.Remember(ctx, Record{ID: "e", Kind: KindEpisode, Content: "经历", Importance: 1})
	require.NoError(t, err)
	fail.Store(true)
	_, err = m.ProcessPending(ctx)
	require.Error(t, err)
	_, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.True(t, s.Records["e"].Reflected)
	require.Len(t, s.Records, 2)
	require.True(t, indexQueued(t, m))
	fail.Store(false)
	m = newMemory(t, m.cfg)
	require.NoError(t, m.Maintain(ctx))
	require.False(t, indexQueued(t, m))
	ids, err := base.IDs(ctx, recordSpace.namespace(testNS, initialScope))
	require.NoError(t, err)
	require.Len(t, ids, 2)
	for _, r := range s.Records {
		k := recordKey(r)
		k.Model = m.modelTag
		require.Contains(t, ids, k.String())
	}
}
func TestPruneDuringReindexPreservesNewRepair(t *testing.T) {
	st := newHookStore(t)
	b := maintenanceConfig(t, st, "prune-race", 20)
	base := NewMemoryIndex()
	entered, release := make(chan struct{}), make(chan struct{})
	emb := hookEmbedder{EmbeddingClient: bigramEmbedder{}, before: func(text string) {
		if text == "旧名" {
			close(entered)
			<-release
		}
	}}
	idx := hookIndex{Index: base, before: func(op string, _ []string, raw string) error {
		k, _ := parseIndexKey(raw)
		if op == "upsert" && k.ID == "entity" && k.ContentRev == 2 {
			return errors.New("新向量暂时写入失败")
		}
		return nil
	}}
	m := newMemory(t, Config{Durable: st, Background: b, Index: idx, Embedder: emb, Retention: Retention{TTL: map[Kind]time.Duration{KindEpisode: time.Hour}}})
	ctx := WithNamespace(context.Background(), testNS...)
	old, err := m.Remember(ctx, Record{ID: "old", Kind: KindEpisode, Content: "旧经历", CreatedAt: m.now().Add(-2 * time.Hour)})
	require.NoError(t, err)
	keep, err := m.Remember(ctx, Record{ID: "keep", Kind: KindFact, Content: "有效事实"})
	require.NoError(t, err)
	_, err = m.mutate(ctx, testNS, initialScope, func(x *tx) error {
		x.Entities.Put("entity", Entity{ID: "entity", Namespace: testNS, Name: "旧名", Live: true, Incarnation: x.incarnation(), Revision: 1, ContentRev: 1, Names: []EntityName{
			{Name: "旧名", Live: true, Origins: []Origin{{Record: refOf(old)}}}, {Name: "新名", Live: true, Origins: []Origin{{Record: refOf(keep)}}},
		}})
		return nil
	})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- m.Reindex(ctx, false) }()
	<-entered
	pruneErr := m.Prune(ctx)
	close(release)
	require.NoError(t, <-done)
	require.ErrorContains(t, pruneErr, "新向量暂时写入失败")
	_, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.Equal(t, "新名", s.Entities["entity"].Name)
	require.True(t, indexQueued(t, m))
	cfg := m.cfg
	cfg.Index = base
	m = newMemory(t, cfg)
	require.NoError(t, m.Maintain(ctx))
	require.False(t, indexQueued(t, m))
	keys, err := base.IDs(ctx, entitySpace.namespace(testNS, initialScope))
	require.NoError(t, err)
	k := entityKey(s.Entities["entity"])
	k.Model = m.modelTag
	require.Equal(t, []string{k.String()}, keys)
}

func TestBackgroundDeadlineRetainsUsageAfterBufferedDelta(t *testing.T) {
	stream := make(chan *llm.StreamChunk, 2)
	stream <- &llm.StreamChunk{Delta: "忽略正文"}
	stream <- &llm.StreamChunk{Done: true, Usage: &llm.Usage{TotalTokens: 8, PromptTokens: 6, CompletionTokens: 2}}
	// 流保持打开，没有后续发送者；返回时须保留终止块中的用量。
	got := bufferedUsage(stream, usage.Evidence{})
	require.Equal(t, usage.BasicFields, got.KnownFields)
	require.Equal(t, 8, got.Tokens.TotalTokens)
	require.Empty(t, stream)
}
