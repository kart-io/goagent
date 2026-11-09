package memory

import (
	"context"
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
