package contextpolicy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock 是可注入内存版的时钟。
type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func putOne(s ResultStore, run, logical string) error {
	data := []byte(run + "/" + logical)
	return s.PutIfAbsent(WithResultScope(context.Background(), run), NewRef(run, "p", logical, data), data)
}

// 内存版：只回收释放时间早于阈值的墓碑，未过期的仍拒绝写入；全部过期后墓碑归零；重复 Release 不刷新释放时间。
func TestMemoryStorePruneReleased(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{at: time.Unix(1_000_000, 0)}
	s := NewMemoryStore()
	mem := s.(*resultStore)
	mem.now = clock.now

	require.NoError(t, putOne(s, "old", "k"))
	require.NoError(t, s.Release(ctx, "old"))
	clock.advance(30 * time.Minute)
	require.NoError(t, s.Release(ctx, "fresh"))
	require.NoError(t, s.Release(ctx, "old"), "重复释放")
	clock.advance(45 * time.Minute)

	require.ErrorContains(t, s.PruneReleased(ctx, -time.Second), "不能为负")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, s.PruneReleased(cancelled, time.Hour), context.Canceled)

	require.NoError(t, s.PruneReleased(ctx, time.Hour))
	require.Len(t, mem.released, 1)
	require.ErrorIs(t, putOne(s, "fresh", "late"), ErrReleased, "未过期墓碑仍拒绝")
	require.NoError(t, putOne(s, "old", "late"), "墓碑回收后迟到写入不再被拒绝（保证边界）")

	require.NoError(t, s.Release(ctx, "old"))
	clock.advance(2 * time.Hour)
	require.NoError(t, s.PruneReleased(ctx, time.Hour))
	require.Empty(t, mem.released, "墓碑归零")
	require.Empty(t, mem.memory)
}

// 内存版回收与写入、释放并发（-race 检查）：未过期运行始终拒绝写入。
func TestMemoryStorePruneConcurrent(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{at: time.Unix(1_000_000, 0)}
	s := NewMemoryStore()
	mem := s.(*resultStore)
	mem.now = clock.now
	for i := range 16 {
		require.NoError(t, s.Release(ctx, fmt.Sprintf("old-%d", i)))
	}
	clock.advance(2 * time.Hour)
	for i := range 16 {
		require.NoError(t, s.Release(ctx, fmt.Sprintf("fresh-%d", i)))
	}
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Go(func() {
			for i := range 16 {
				assert.ErrorIs(t, putOne(s, fmt.Sprintf("fresh-%d", i), fmt.Sprint(w)), ErrReleased)
				if err := putOne(s, fmt.Sprintf("old-%d", i), fmt.Sprint(w)); err != nil {
					assert.ErrorIs(t, err, ErrReleased)
				}
			}
		})
		wg.Go(func() { assert.NoError(t, s.PruneReleased(ctx, time.Hour)) })
	}
	wg.Wait()
	require.Len(t, mem.released, 16, "只剩未过期墓碑")
}

// 持久版：按墓碑 mtime 回收，未过期的仍拒绝写入；与两个实例（模拟两进程）的写入并发无竞争；
// 底层不支持作用域时报错。
func TestDurableStorePruneReleased(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, other := newFileResultStore(t, dir), newFileResultStore(t, dir)
	for i := range 8 {
		run := fmt.Sprintf("old-%d", i)
		require.NoError(t, putOne(s, run, "k"))
		require.NoError(t, s.Release(ctx, run))
	}
	gates, err := filepath.Glob(filepath.Join(dir, "scopes", "*.gate"))
	require.NoError(t, err)
	require.Len(t, gates, 8)
	past := time.Now().Add(-2 * time.Hour)
	for _, gate := range gates {
		require.NoError(t, os.Chtimes(gate, past, past))
	}
	require.NoError(t, s.Release(ctx, "fresh"))
	requireOnlyTombstones(t, dir, 9)

	var wg sync.WaitGroup
	for w, store := range []ResultStore{s, other} {
		wg.Go(func() {
			for round := range 8 {
				assert.ErrorIs(t, putOne(store, "fresh", fmt.Sprint(w, round)), ErrReleased)
			}
		})
		wg.Go(func() { assert.NoError(t, store.PruneReleased(ctx, time.Hour)) })
	}
	wg.Wait()
	requireOnlyTombstones(t, dir, 1)
	require.ErrorIs(t, putOne(other, "fresh", "late"), ErrReleased, "未过期墓碑仍拒绝")
	require.NoError(t, putOne(other, "old-0", "late"), "墓碑回收后迟到写入不再被拒绝（保证边界）")

	file, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	plain, err := NewDurableResultStore(struct{ durable.Store }{file})
	require.NoError(t, err)
	require.ErrorContains(t, plain.PruneReleased(ctx, time.Hour), "不支持释放")
}
