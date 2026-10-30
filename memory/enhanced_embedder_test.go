package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// keywordEmbedder 按关键词返回固定的 3 维向量，使相似度结果可预期。
type keywordEmbedder struct {
	mu     sync.Mutex
	texts  []string
	err    error
	onCall func()
}

func (e *keywordEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	if e.onCall != nil {
		e.onCall()
	}
	e.mu.Lock()
	e.texts = append(e.texts, text)
	e.mu.Unlock()
	if e.err != nil {
		return nil, e.err
	}
	switch {
	case strings.Contains(text, "数据库"):
		return []float64{1, 0, 0}, nil
	case strings.Contains(text, "天气"):
		return []float64{0, 1, 0}, nil
	default:
		return []float64{0, 0, 1}, nil
	}
}

func (e *keywordEmbedder) calls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.texts...)
}

type stringerContent struct{ topic string }

func (s stringerContent) String() string { return "关于" + s.topic + "的笔记" }

func newEmbedderMemory(t *testing.T, embedder TextEmbedder, opts ...MemoryOption) (*HierarchicalMemory, *InMemoryVectorStore) {
	t.Helper()
	ctx := context.Background()
	vs := NewInMemoryVectorStore(3)
	if embedder != nil {
		opts = append(opts, WithEmbedder(embedder))
	}
	hm := NewHierarchicalMemoryWithContext(ctx, AsVectorStore(vs), opts...)
	t.Cleanup(func() { _ = hm.Shutdown(ctx) })
	return hm, vs
}

func TestStoreTypedWithEmbedder_LongTermSearchable(t *testing.T) {
	ctx := context.Background()
	embedder := &keywordEmbedder{}
	hm, _ := newEmbedderMemory(t, embedder)

	require.NoError(t, hm.StoreTyped(ctx, "plan-1", "数据库优化方案", MemoryTypeSemantic, StoreOptions{}))
	require.NoError(t, hm.StoreTyped(ctx, "note-1", "明天天气晴", MemoryTypeLongTerm, StoreOptions{}))

	entries, err := hm.VectorSearch(ctx, []float32{1, 0, 0}, 1, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "plan-1", entries[0].ID)
	assert.InDelta(t, 1.0, entries[0].Metadata["similarity_score"], 1e-6)
	assert.Equal(t, []float32{1, 0, 0}, entries[0].Embedding)
	assert.Equal(t, []string{"数据库优化方案", "明天天气晴"}, embedder.calls())
}

func TestStoreWithEmbedder_ShortTermSearchable(t *testing.T) {
	ctx := context.Background()
	hm, vs := newEmbedderMemory(t, &keywordEmbedder{})

	require.NoError(t, hm.Store(ctx, "chat-1", "用户问数据库索引", StoreOptions{}))
	assert.Equal(t, 1, vs.Size())

	entries, err := hm.VectorSearch(ctx, []float32{1, 0, 0}, 5, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "chat-1", entries[0].ID)
	assert.Equal(t, MemoryTypeShortTerm, entries[0].Type)
}

func TestStoreWithEmbedder_ShortTermEvictionDropsVector(t *testing.T) {
	ctx := context.Background()
	hm, vs := newEmbedderMemory(t, &keywordEmbedder{}, WithShortTermCapacity(1))

	require.NoError(t, hm.Store(ctx, "old", "数据库连接池", StoreOptions{}))
	require.NoError(t, hm.Store(ctx, "new", "今天天气", StoreOptions{}))

	assert.Equal(t, 1, vs.Size(), "被淘汰的短期条目不应在向量存储留下孤儿")
	entries, err := hm.VectorSearch(ctx, []float32{0, 1, 0}, 1, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "new", entries[0].ID)
}

func TestStoreWithEmbedder_EvictionKeepsVectorOwnedByLongTerm(t *testing.T) {
	ctx := context.Background()
	hm, vs := newEmbedderMemory(t, &keywordEmbedder{}, WithShortTermCapacity(1))

	require.NoError(t, hm.StoreTyped(ctx, "shared", "数据库备份", MemoryTypeLongTerm, StoreOptions{}))
	require.NoError(t, hm.Store(ctx, "shared", "数据库备份", StoreOptions{}))
	require.NoError(t, hm.Store(ctx, "other", "今天天气", StoreOptions{}))

	assert.Equal(t, 2, vs.Size())
	entries, err := hm.VectorSearch(ctx, []float32{1, 0, 0}, 1, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "shared", entries[0].ID)
}

func TestStoreWithEmbedder_ConsolidateKeepsVector(t *testing.T) {
	ctx := context.Background()
	hm, vs := newEmbedderMemory(t, &keywordEmbedder{})

	// 足够长的内容使重要性超过巩固阈值
	require.NoError(t, hm.Store(ctx, "big", strings.Repeat("数据库", 100), StoreOptions{}))
	require.NoError(t, hm.Consolidate(ctx))

	stats := hm.GetStats()
	assert.Equal(t, 0, stats.ShortTermCount)
	assert.Equal(t, 1, stats.LongTermCount)
	assert.Equal(t, 1, vs.Size())

	entries, err := hm.VectorSearch(ctx, []float32{1, 0, 0}, 1, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "big", entries[0].ID)
	assert.Equal(t, MemoryTypeLongTerm, entries[0].Type)
}

func TestStoreTypedWithEmbedder_EmbedErrorStoresNothing(t *testing.T) {
	ctx := context.Background()
	cause := errors.New("embedding service unavailable")
	hm, vs := newEmbedderMemory(t, &keywordEmbedder{err: cause})

	for _, memType := range []MemoryType{MemoryTypeShortTerm, MemoryTypeLongTerm} {
		err := hm.StoreTyped(ctx, "k-"+string(memType), "数据库", memType, StoreOptions{})
		require.Error(t, err)
		assert.ErrorIs(t, err, cause)
		var agentErr *agentErrors.AgentError
		require.ErrorAs(t, err, &agentErr)
		assert.Equal(t, agentErrors.CodeRetrievalEmbedding, agentErr.Code)
	}

	stats := hm.GetStats()
	assert.Equal(t, 0, stats.TotalEntries)
	assert.Equal(t, 0, vs.Size())
}

func TestStoreTypedWithEmbedder_NonStringContent(t *testing.T) {
	ctx := context.Background()
	embedder := &keywordEmbedder{}
	hm, _ := newEmbedderMemory(t, embedder)

	require.NoError(t, hm.StoreTyped(ctx, "bytes", []byte("数据库日志"), MemoryTypeLongTerm, StoreOptions{}))
	require.NoError(t, hm.StoreTyped(ctx, "stringer", stringerContent{topic: "天气"}, MemoryTypeLongTerm, StoreOptions{}))
	require.NoError(t, hm.StoreTyped(ctx, "map", map[string]interface{}{"topic": "数据库"}, MemoryTypeLongTerm, StoreOptions{}))
	require.NoError(t, hm.StoreTyped(ctx, "nil", nil, MemoryTypeLongTerm, StoreOptions{}))
	require.NoError(t, hm.StoreTyped(ctx, "empty", "", MemoryTypeLongTerm, StoreOptions{}))
	require.NoError(t, hm.StoreTyped(ctx, "chan", make(chan int), MemoryTypeLongTerm, StoreOptions{}))

	assert.Equal(t, []string{"数据库日志", "关于天气的笔记", `{"topic":"数据库"}`}, embedder.calls())

	for _, id := range []string{"nil", "empty", "chan"} {
		entry, err := hm.longTerm.Get(ctx, id)
		require.NoError(t, err, "无法提取文本的条目仍应写入")
		assert.Empty(t, entry.Embedding)
	}

	entries, err := hm.VectorSearch(ctx, []float32{0, 1, 0}, 1, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "stringer", entries[0].ID)
}

func TestStoreTypedWithEmbedder_EmbedsOutsideLock(t *testing.T) {
	ctx := context.Background()
	embedder := &keywordEmbedder{}
	hm, _ := newEmbedderMemory(t, embedder)

	// Embed 期间读取统计需要 m.mu 读锁，若 StoreTyped 已持有写锁会死锁
	embedder.onCall = func() { _ = hm.GetStats() }

	done := make(chan error, 1)
	go func() { done <- hm.StoreTyped(ctx, "k", "数据库", MemoryTypeLongTerm, StoreOptions{}) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Embed 在持有 m.mu 时被调用")
	}
}

func TestStoreTypedWithoutEmbedder_Unchanged(t *testing.T) {
	ctx := context.Background()
	hm, vs := newEmbedderMemory(t, nil)

	require.NoError(t, hm.Store(ctx, "s", "数据库", StoreOptions{}))
	require.NoError(t, hm.StoreTyped(ctx, "l", "数据库", MemoryTypeLongTerm, StoreOptions{}))

	assert.Equal(t, 0, vs.Size())
	entry, err := hm.longTerm.Get(ctx, "l")
	require.NoError(t, err)
	assert.Empty(t, entry.Embedding)

	entries, err := hm.VectorSearch(ctx, []float32{1, 0, 0}, 5, 0)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// provider 客户端经 llm.EmbeddingClient 暴露 Embed，须能直接作为 TextEmbedder 传入。
var _ TextEmbedder = llm.EmbeddingClient(nil)

type ptrStringer struct{ topic string }

func (p *ptrStringer) String() string { return p.topic }

func TestStoreTypedWithEmbedder_NilPointerStringerSkipped(t *testing.T) {
	ctx := context.Background()
	embedder := &keywordEmbedder{}
	hm, _ := newEmbedderMemory(t, embedder)

	require.NotPanics(t, func() {
		require.NoError(t, hm.StoreTyped(ctx, "p", (*ptrStringer)(nil), MemoryTypeLongTerm, StoreOptions{}))
	})
	assert.Empty(t, embedder.calls())
}

func TestWithShortTermCapacity_Applied(t *testing.T) {
	ctx := context.Background()
	hm, _ := newEmbedderMemory(t, nil, WithShortTermCapacity(2))

	for _, k := range []string{"a", "b", "c"} {
		require.NoError(t, hm.Store(ctx, k, k, StoreOptions{}))
	}
	assert.Equal(t, 2, hm.shortTerm.Size())
}

func TestVectorSearch_ConcurrentDoesNotMutateSharedEntries(t *testing.T) {
	ctx := context.Background()
	hm, _ := newEmbedderMemory(t, &keywordEmbedder{})
	require.NoError(t, hm.Store(ctx, "s", "数据库短期", StoreOptions{}))
	require.NoError(t, hm.StoreTyped(ctx, "l", "数据库长期", MemoryTypeLongTerm, StoreOptions{}))

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				entries, err := hm.VectorSearch(ctx, []float32{1, 0, 0}, 5, 0)
				assert.NoError(t, err)
				assert.Len(t, entries, 2)
			}
		}()
	}
	wg.Wait()

	for _, id := range []string{"s", "l"} {
		var entry *MemoryEntry
		if e, err := hm.shortTerm.Get(ctx, id); err == nil {
			entry = e
		} else {
			entry, err = hm.longTerm.Get(ctx, id)
			require.NoError(t, err)
		}
		assert.NotContains(t, entry.Metadata, "similarity_score", "VectorSearch 不应修改存储中的条目")
	}
}

func TestVectorSearch_ThresholdFiltersLowSimilarity(t *testing.T) {
	ctx := context.Background()
	hm, _ := newEmbedderMemory(t, &keywordEmbedder{})
	require.NoError(t, hm.StoreTyped(ctx, "db", "数据库", MemoryTypeLongTerm, StoreOptions{}))
	require.NoError(t, hm.StoreTyped(ctx, "weather", "天气", MemoryTypeLongTerm, StoreOptions{}))

	entries, err := hm.VectorSearch(ctx, []float32{1, 0, 0}, 5, 0.5)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "db", entries[0].ID)

	// 等于阈值的结果保留：正交向量相似度为 0
	entries, err = hm.VectorSearch(ctx, []float32{1, 0, 0}, 5, 0)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestEviction_RestoresLongTermVector(t *testing.T) {
	ctx := context.Background()
	hm, vs := newEmbedderMemory(t, &keywordEmbedder{}, WithShortTermCapacity(1))

	require.NoError(t, hm.StoreTyped(ctx, "x", "数据库", MemoryTypeLongTerm, StoreOptions{}))
	require.NoError(t, hm.Store(ctx, "x", "天气", StoreOptions{}))
	require.NoError(t, hm.Store(ctx, "z", "其他", StoreOptions{}))

	assert.Equal(t, 2, vs.Size())
	entries, err := hm.VectorSearch(ctx, []float32{1, 0, 0}, 5, 0.5)
	require.NoError(t, err)
	require.Len(t, entries, 1, "短期 x 被淘汰后应恢复长期 x 的向量")
	assert.Equal(t, "x", entries[0].ID)
	assert.Equal(t, "数据库", entries[0].Content)
}

func TestForget_PromotedEntryKeepsLongTermVector(t *testing.T) {
	ctx := context.Background()
	hm, _ := newEmbedderMemory(t, &keywordEmbedder{})
	require.NoError(t, hm.StoreTyped(ctx, "x", "数据库", MemoryTypeLongTerm, StoreOptions{}))
	for i := 0; i < 11; i++ {
		_, err := hm.Get(ctx, "x")
		require.NoError(t, err)
	}
	_, err := hm.shortTerm.Get(ctx, "x")
	require.NoError(t, err, "前置条件：x 已晋升到短期记忆")

	require.NoError(t, hm.Forget(ctx, 0.5))
	_, err = hm.shortTerm.Get(ctx, "x")
	require.Error(t, err, "前置条件：短期 x 已被遗忘")
	_, err = hm.longTerm.Get(ctx, "x")
	require.NoError(t, err, "前置条件：长期 x 仍保留")

	entries, err := hm.VectorSearch(ctx, []float32{1, 0, 0}, 5, 0.5)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "x", entries[0].ID)
}

func TestGetPromotion_EvictionDropsVector(t *testing.T) {
	ctx := context.Background()
	hm, vs := newEmbedderMemory(t, &keywordEmbedder{}, WithShortTermCapacity(1))
	require.NoError(t, hm.Store(ctx, "y", "天气", StoreOptions{}))
	require.NoError(t, hm.StoreTyped(ctx, "x", "数据库", MemoryTypeLongTerm, StoreOptions{}))
	for i := 0; i < 11; i++ {
		_, err := hm.Get(ctx, "x")
		require.NoError(t, err)
	}
	_, err := hm.shortTerm.Get(ctx, "y")
	require.Error(t, err, "前置条件：y 因晋升被淘汰")

	assert.Equal(t, 1, vs.Size(), "被晋升挤出的 y 不应留下孤儿向量")
}

func TestStoreTypedWithEmbedder_UnembeddableOverwriteDropsVector(t *testing.T) {
	ctx := context.Background()
	hm, vs := newEmbedderMemory(t, &keywordEmbedder{})

	require.NoError(t, hm.StoreTyped(ctx, "l", "数据库", MemoryTypeLongTerm, StoreOptions{}))
	require.NoError(t, hm.StoreTyped(ctx, "l", nil, MemoryTypeLongTerm, StoreOptions{}))
	require.NoError(t, hm.Store(ctx, "s", "数据库", StoreOptions{}))
	require.NoError(t, hm.Store(ctx, "s", "", StoreOptions{}))

	assert.Equal(t, 0, vs.Size())
	entries, err := hm.VectorSearch(ctx, []float32{1, 0, 0}, 5, 0)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestStoreTypedWithEmbedder_UnembeddableShortOverLongKeepsLongVector(t *testing.T) {
	ctx := context.Background()
	hm, vs := newEmbedderMemory(t, &keywordEmbedder{})

	require.NoError(t, hm.StoreTyped(ctx, "x", "数据库", MemoryTypeLongTerm, StoreOptions{}))
	require.NoError(t, hm.Store(ctx, "x", nil, StoreOptions{}))

	assert.Equal(t, 1, vs.Size())
	entries, err := hm.VectorSearch(ctx, []float32{1, 0, 0}, 5, 0.5)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "数据库", entries[0].Content, "向量属于长期条目，应返回长期条目")
}

func TestStoreTypedWithEmbedder_VectorAddFailureStoresNothing(t *testing.T) {
	ctx := context.Background()
	// 3 维向量写入 4 维存储必然失败
	vs := NewInMemoryVectorStore(4)
	hm := NewHierarchicalMemoryWithContext(ctx, AsVectorStore(vs), WithEmbedder(&keywordEmbedder{}))
	t.Cleanup(func() { _ = hm.Shutdown(ctx) })

	require.Error(t, hm.StoreTyped(ctx, "l", "数据库", MemoryTypeLongTerm, StoreOptions{}))
	require.Error(t, hm.Store(ctx, "s", "数据库", StoreOptions{}))
	assert.Equal(t, 0, hm.GetStats().TotalEntries)
}
