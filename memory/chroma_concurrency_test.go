package memory

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// blockingChroma 让写入停在 gate 上，统计同时在途的写入数。
type blockingChroma struct {
	inFlight, peak atomic.Int32
	gate           chan struct{}
}

func (c *blockingChroma) enter() {
	n := c.inFlight.Add(1)
	for p := c.peak.Load(); n > p && !c.peak.CompareAndSwap(p, n); p = c.peak.Load() {
	}
	<-c.gate
	c.inFlight.Add(-1)
}
func (c *blockingChroma) CreateCollection(context.Context, string, map[string]interface{}) error {
	return nil
}
func (c *blockingChroma) AddDocuments(context.Context, string, []string, [][]float32, []map[string]interface{}) error {
	c.enter()
	return nil
}
func (c *blockingChroma) Query(context.Context, string, [][]float32, int) (*ChromaQueryResult, error) {
	return &ChromaQueryResult{}, nil
}
func (c *blockingChroma) Delete(context.Context, string, []string) error { c.enter(); return nil }
func (c *blockingChroma) DeleteCollection(context.Context, string) error { return nil }

// 写入与删除的网络请求可以并发，不被本地锁串行。
func TestChromaVectorStoreWritesRunConcurrently(t *testing.T) {
	client := &blockingChroma{gate: make(chan struct{})}
	s := NewChromaVectorStore(client, "c", 2)
	done := make(chan struct{}, 2)
	go func() { _ = s.Store(context.Background(), "a", []float32{1, 0}); done <- struct{}{} }()
	go func() { _ = s.Delete(context.Background(), "b"); done <- struct{}{} }()
	require.Eventually(t, func() bool { return client.peak.Load() == 2 }, time.Second, time.Millisecond, "第二个请求被本地锁挡住")
	close(client.gate)
	<-done
	<-done
}

// blockingSearch 让向量检索停在 gate 上，模拟远端向量库。
type blockingSearch struct {
	VectorStore
	reached, gate chan struct{}
}

func (b *blockingSearch) Search(ctx context.Context, embedding []float32, limit int) ([]*SearchResult, error) {
	close(b.reached)
	<-b.gate
	return b.VectorStore.Search(ctx, embedding, limit)
}

// 远端向量检索进行中时，写入记忆不被读锁挡住。
func TestVectorSearchDoesNotHoldLockDuringRemoteSearch(t *testing.T) {
	ctx := context.Background()
	vs := &blockingSearch{VectorStore: AsVectorStore(NewInMemoryVectorStore(3)), reached: make(chan struct{}), gate: make(chan struct{})}
	hm := NewHierarchicalMemoryWithContext(ctx, vs)
	t.Cleanup(func() { _ = hm.Shutdown(ctx) })
	searched := make(chan struct{})
	go func() { _, _ = hm.VectorSearch(ctx, []float32{1, 0, 0}, 5, 0); close(searched) }()
	<-vs.reached
	stored := make(chan error, 1)
	go func() { stored <- hm.Store(ctx, "k", "v", StoreOptions{}) }()
	select {
	case err := <-stored:
		require.NoError(t, err)
	case <-time.After(time.Second):
		close(vs.gate)
		t.Fatal("写入被进行中的向量检索挡住")
	}
	close(vs.gate)
	<-searched
}

// blockingAdd 在 armed 后让向量写入停在 gate 上，模拟远端向量库的慢写入。
type blockingAdd struct {
	VectorStore
	armed         atomic.Bool
	reached, gate chan struct{}
}

func (b *blockingAdd) Add(ctx context.Context, id string, embedding []float32, metadata map[string]interface{}) error {
	if b.armed.CompareAndSwap(true, false) {
		close(b.reached)
		<-b.gate
	}
	return b.VectorStore.Add(ctx, id, embedding, metadata)
}

// 写入记忆等待远端向量库时，只读操作不被挡住；写入完成后条目与向量索引一致。
func TestStoreTypedDoesNotBlockReadersDuringVectorWrite(t *testing.T) {
	ctx := context.Background()
	vs := &blockingAdd{VectorStore: AsVectorStore(NewInMemoryVectorStore(3)), reached: make(chan struct{}), gate: make(chan struct{})}
	hm := NewHierarchicalMemoryWithContext(ctx, vs, WithEmbedder(&keywordEmbedder{}))
	t.Cleanup(func() { _ = hm.Shutdown(ctx) })
	require.NoError(t, hm.Store(ctx, "old", "数据库旧记忆", StoreOptions{}))

	vs.armed.Store(true)
	stored := make(chan error, 1)
	go func() { stored <- hm.StoreTyped(ctx, "new", "数据库新记忆", MemoryTypeLongTerm, StoreOptions{}) }()
	<-vs.reached
	read := make(chan struct{})
	go func() {
		_, _ = hm.Get(ctx, "old")
		_, _ = hm.Search(ctx, "数据库", 5)
		_ = hm.GetStats()
		_, _ = hm.VectorSearch(ctx, []float32{1, 0, 0}, 5, 0)
		close(read)
	}()
	select {
	case <-read:
	case <-time.After(time.Second):
		close(vs.gate)
		t.Fatal("只读操作被进行中的向量写入挡住")
	}
	close(vs.gate)
	require.NoError(t, <-stored)
	entries, err := hm.VectorSearch(ctx, []float32{1, 0, 0}, 5, 0)
	require.NoError(t, err)
	require.Len(t, entries, 2)
}

// 拆分写锁与读锁后，各类读写操作并发执行不出现数据竞争或死锁，结束时向量索引只指向仍存在的条目。
func TestHierarchicalMemoryConcurrentOperations(t *testing.T) {
	ctx := context.Background()
	vs := NewInMemoryVectorStore(3)
	hm := NewHierarchicalMemoryWithContext(ctx, AsVectorStore(vs), WithEmbedder(&keywordEmbedder{}), WithShortTermCapacity(4))
	t.Cleanup(func() { _ = hm.Shutdown(ctx) })
	var wg sync.WaitGroup
	for w := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 40 {
				key := fmt.Sprintf("k%d", (w+i)%8)
				switch i % 7 {
				case 0:
					_ = hm.Store(ctx, key, "数据库短期", StoreOptions{})
				case 1:
					_ = hm.StoreTyped(ctx, key, "数据库长期", MemoryTypeLongTerm, StoreOptions{})
				case 2:
					_, _ = hm.Get(ctx, key)
				case 3:
					_, _ = hm.VectorSearch(ctx, []float32{1, 0, 0}, 5, 0)
				case 4:
					_ = hm.Associate(ctx, key, fmt.Sprintf("k%d", (w+i+1)%8), 0.5)
				case 5:
					_ = hm.Consolidate(ctx)
				case 6:
					_ = hm.Forget(ctx, 0.01)
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("并发操作未在期限内结束，可能死锁")
	}
	results, err := AsVectorStore(vs).Search(ctx, []float32{1, 0, 0}, 100)
	require.NoError(t, err)
	for _, r := range results {
		_, err := hm.Get(ctx, r.ID)
		require.NoError(t, err, "向量 %s 没有对应条目", r.ID)
	}
}
