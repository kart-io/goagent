package store

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// gatedBackend 让指定 key 的第一次 Get 读到值后停在 gate 上，模拟慢后端或与写入交错的冷读。
type gatedBackend struct {
	*InMemoryLangGraphStore
	key     string
	gated   atomic.Bool
	reached chan struct{}
	gate    chan struct{}
	release func()
}

func (b *gatedBackend) Get(ctx context.Context, ns []string, key string) (*StoreValue, error) {
	v, err := b.InMemoryLangGraphStore.Get(ctx, ns, key)
	if key == b.key {
		if b.gated.CompareAndSwap(false, true) {
			close(b.reached)
			<-b.gate
		}
	}
	return v, err
}

// newGated 返回的后端在测试结束时放开 gate，失败路径也不会让后续 Close 卡住。
func newGated(t *testing.T, key string) *gatedBackend {
	gate := make(chan struct{})
	b := &gatedBackend{InMemoryLangGraphStore: NewInMemoryLangGraphStore(), key: key, reached: make(chan struct{}), gate: gate, release: sync.OnceFunc(func() { close(gate) })}
	t.Cleanup(b.release)
	return b
}

// 一个 key 的冷读等待后端时，其他 key 的缓存命中不被阻塞。
func TestStoreWithCacheHitNotBlockedByColdMiss(t *testing.T) {
	ctx := context.Background()
	b := newGated(t, "cold")
	ns := []string{"n"}
	require.NoError(t, b.Put(ctx, ns, "hot", "v"))
	require.NoError(t, b.Put(ctx, ns, "cold", "v"))
	c := NewStoreWithCache(b, time.Minute)
	defer c.Close()
	defer b.release()
	_, err := c.Get(ctx, ns, "hot")
	require.NoError(t, err)

	go func() { _, _ = c.Get(ctx, ns, "cold") }()
	<-b.reached
	done := make(chan struct{})
	go func() { _, _ = c.Get(ctx, ns, "hot"); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("缓存命中被另一个 key 的后端读取阻塞")
	}
	b.release()
}

// 冷读拿到旧值后、写入完成前不能把旧值发布进缓存，之后的读取必须看到新值。
func TestStoreWithCacheColdReadDoesNotPublishStaleValue(t *testing.T) {
	ctx := context.Background()
	b := newGated(t, "k")
	ns := []string{"n"}
	require.NoError(t, b.Put(ctx, ns, "k", "old"))
	c := NewStoreWithCache(b, time.Minute)
	defer c.Close()
	defer b.release()

	read := make(chan *StoreValue, 1)
	go func() { v, _ := c.Get(ctx, ns, "k"); read <- v }()
	<-b.reached
	written := make(chan error, 1)
	go func() { written <- c.Put(ctx, ns, "k", "new") }()
	select {
	case err := <-written:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("写入被冷读阻塞")
	}
	b.release()
	require.Equal(t, "old", (<-read).Value)

	v, err := c.Get(ctx, ns, "k")
	require.NoError(t, err)
	require.Equal(t, "new", v.Value)
}
