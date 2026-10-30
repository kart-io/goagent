package contextpolicy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// diskFiles 返回目录下全部普通文件（相对路径）与子目录数量。
func diskFiles(t *testing.T, dir string) (files []string, dirs int) {
	t.Helper()
	require.NoError(t, filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if info.IsDir() {
			if rel != "." {
				dirs++
			}
			return nil
		}
		files = append(files, rel)
		return nil
	}))
	return files, dirs
}

// requireOnlyTombstones 断言磁盘上只剩 scopes/ 目录与 want 个墓碑：没有证据、锁、清单或临时文件。
func requireOnlyTombstones(t *testing.T, dir string, want int) {
	t.Helper()
	files, dirs := diskFiles(t, dir)
	require.Len(t, files, want)
	for _, f := range files {
		require.Equal(t, "scopes", filepath.Dir(f), f)
		require.Equal(t, ".gate", filepath.Ext(f), f)
		b, err := os.ReadFile(filepath.Join(dir, f))
		require.NoError(t, err)
		require.Equal(t, "removed\n", string(b), "只剩已删除作用域的墓碑")
	}
	if want > 0 {
		require.Equal(t, 1, dirs, "作用域目录全部删除")
	}
}

func newFileResultStore(t *testing.T, dir string) ResultStore {
	t.Helper()
	file, err := durable.NewFileStore(dir)
	require.NoError(t, err)
	s, err := NewDurableResultStore(file)
	require.NoError(t, err)
	return s
}

// 跨执行共享的 ResultStore 按运行键 Release 后不再保留该运行的证据：内存条目回到零，
// 磁盘上没有证据、锁文件或清单，每个运行只留一个 8 字节墓碑。
func TestResultStoreBoundedGrowth(t *testing.T) {
	const runs = 1000
	payload := make([]byte, 1024)
	cycle := func(s ResultStore) {
		for i := range runs {
			run := fmt.Sprintf("run-%d", i)
			ctx := WithResultScope(context.Background(), run)
			for j := range 2 {
				data := append([]byte{byte(j)}, payload...)
				ref := NewRef(run, "p", fmt.Sprint(j), data)
				require.NoError(t, s.PutIfAbsent(ctx, ref, data))
				_, err := s.Get(ctx, ref)
				require.NoError(t, err)
			}
			require.NoError(t, s.Release(ctx, run))
		}
		require.NoError(t, s.Release(context.Background(), "never-written"), "释放不存在的运行不报错")
	}
	mem := NewMemoryStore()
	cycle(mem)
	require.Empty(t, mem.(*resultStore).memory, "内存条目不随执行次数增长")
	require.Len(t, mem.(*resultStore).released, runs+1, "内存墓碑每运行一个运行键")

	dir := t.TempDir()
	ds := newFileResultStore(t, dir)
	cycle(ds)
	requireOnlyTombstones(t, dir, runs+1)

	for _, s := range []ResultStore{mem, ds} {
		ctx := WithResultScope(context.Background(), "run-0")
		_, err := s.Get(ctx, NewRef("run-0", "p", "0", append([]byte{0}, payload...)))
		require.ErrorIs(t, err, ErrReleased, "释放后读不到证据")
	}
	requireOnlyTombstones(t, dir, runs+1)

	// 底层存储不支持作用域时明确报错，而不是静默保留。
	file, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	plain, err := NewDurableResultStore(struct{ durable.Store }{file})
	require.NoError(t, err)
	require.ErrorContains(t, plain.Release(context.Background(), "run-0"), "不支持释放")
}

// 读取不存在或已释放的引用不加锁，也不在磁盘上产生任何文件。
func TestResultStoreGetMissingLeavesNoFiles(t *testing.T) {
	dir := t.TempDir()
	s := newFileResultStore(t, dir)
	ctx := WithResultScope(context.Background(), "run")
	_, err := s.Get(ctx, NewRef("run", "p", "missing", []byte("x")))
	require.ErrorIs(t, err, os.ErrNotExist)
	files, dirs := diskFiles(t, dir)
	require.Empty(t, files)
	require.Zero(t, dirs)

	ref := NewRef("run", "p", "k", []byte("x"))
	require.NoError(t, s.PutIfAbsent(ctx, ref, []byte("x")))
	require.NoError(t, s.Release(ctx, "run"))
	_, err = s.Get(ctx, ref)
	require.ErrorIs(t, err, ErrReleased)
	require.ErrorIs(t, err, durable.ErrScopeRemoved)
	requireOnlyTombstones(t, dir, 1)
}

// Release 之后到达的写入被拒绝且不留数据；另一个 FileStore 实例（模拟另一进程）同样看到墓碑。
func TestResultStoreLatePutAfterRelease(t *testing.T) {
	data := []byte("evidence")
	ref := NewRef("run", "p", "k", data)
	late := NewRef("run", "p", "late", data)
	ctx := WithResultScope(context.Background(), "run")

	mem := NewMemoryStore()
	require.NoError(t, mem.PutIfAbsent(ctx, ref, data))
	require.NoError(t, mem.Release(ctx, "run"))
	require.ErrorIs(t, mem.PutIfAbsent(ctx, late, data), ErrReleased)
	require.ErrorIs(t, mem.PutIfAbsent(ctx, ref, data), ErrReleased)
	_, err := mem.Get(ctx, late)
	require.ErrorIs(t, err, ErrReleased, "内存版释放后读取与持久版一致")
	require.Empty(t, mem.(*resultStore).memory)

	dir := t.TempDir()
	writer, releaser := newFileResultStore(t, dir), newFileResultStore(t, dir)
	require.NoError(t, writer.PutIfAbsent(ctx, ref, data))
	require.NoError(t, releaser.Release(ctx, "run"))
	err = writer.PutIfAbsent(ctx, late, data)
	require.ErrorIs(t, err, ErrReleased)
	require.ErrorIs(t, err, durable.ErrScopeRemoved)
	require.ErrorIs(t, newFileResultStore(t, dir).PutIfAbsent(ctx, ref, data), ErrReleased, "新实例（重启后）同样拒绝")
	require.NoError(t, releaser.Release(ctx, "run"), "重复释放幂等")
	requireOnlyTombstones(t, dir, 1)

	// 与 Release 并发的写入要么在释放前完成并随作用域删除，要么被拒绝，都不留数据。
	for i := range 20 {
		run := fmt.Sprintf("race-%d", i)
		ctx := WithResultScope(context.Background(), run)
		var wg sync.WaitGroup
		for j := range 8 {
			wg.Go(func() {
				d := []byte(fmt.Sprint(j))
				err := writer.PutIfAbsent(ctx, NewRef(run, "p", fmt.Sprint(j), d), d)
				if err != nil {
					assert.ErrorIs(t, err, ErrReleased)
				}
			})
		}
		wg.Go(func() { assert.NoError(t, releaser.Release(ctx, run)) })
		wg.Wait()
	}
	requireOnlyTombstones(t, dir, 21)
}

// 并发写同一引用：相同内容全部成功，内容冲突者报错，最终只存在一份且与引用一致。
func TestResultStoreConcurrentPutSameRef(t *testing.T) {
	for name, s := range map[string]ResultStore{"memory": NewMemoryStore(), "file": newFileResultStore(t, t.TempDir())} {
		t.Run(name, func(t *testing.T) {
			ctx := WithResultScope(context.Background(), "run")
			data := []byte("same evidence")
			ref := NewRef("run", "p", "k", data)
			other := NewRef("run", "p", "k", []byte("other evidence"))
			var wg sync.WaitGroup
			var mu sync.Mutex
			okSame, okOther := 0, 0
			for i := range 32 {
				wg.Go(func() {
					var err error
					if i%2 == 0 {
						err = s.PutIfAbsent(ctx, ref, data)
					} else {
						err = s.PutIfAbsent(ctx, other, []byte("other evidence"))
					}
					mu.Lock()
					defer mu.Unlock()
					if err == nil && i%2 == 0 {
						okSame++
					} else if err == nil {
						okOther++
					} else {
						assert.ErrorContains(t, err, "conflict")
					}
				})
			}
			wg.Wait()
			require.True(t, (okSame == 16 && okOther == 0) || (okSame == 0 && okOther == 16), "只有一种内容胜出：same=%d other=%d", okSame, okOther)
			winner, want := ref, data
			if okOther > 0 {
				winner, want = other, []byte("other evidence")
			}
			got, err := s.Get(ctx, winner)
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

// 同一 store 中不同运行互不影响；重新打开同一目录（模拟进程重启后用同一 RunKey 恢复）能读回证据。
func TestResultStoreRunsIsolated(t *testing.T) {
	dir := t.TempDir()
	for name, s := range map[string]ResultStore{"memory": NewMemoryStore(), "file": newFileResultStore(t, dir)} {
		t.Run(name, func(t *testing.T) {
			a, b := WithResultScope(context.Background(), "A"), WithResultScope(context.Background(), "B")
			refA, refB := NewRef("A", "p", "k", []byte("a")), NewRef("B", "p", "k", []byte("b"))
			require.NoError(t, s.PutIfAbsent(a, refA, []byte("a")))
			require.NoError(t, s.PutIfAbsent(b, refB, []byte("b")))
			require.NoError(t, s.Release(a, "A"))
			_, err := s.Get(a, refA)
			require.ErrorIs(t, err, ErrReleased)
			got, err := s.Get(b, refB)
			require.NoError(t, err)
			require.Equal(t, "b", string(got))
			more := NewRef("B", "p", "more", []byte("more"))
			require.NoError(t, s.PutIfAbsent(b, more, []byte("more")), "其他运行仍可写入")
		})
	}
	resumed := newFileResultStore(t, dir)
	got, err := resumed.Get(WithResultScope(context.Background(), "B"), NewRef("B", "p", "more", []byte("more")))
	require.NoError(t, err)
	require.Equal(t, "more", string(got))
}

// Release 落盘墓碑后、删除证据前崩溃：再次 Release 之前，Get 已返回 ErrReleased，且读取不产生文件。
// 墓碑按 FileStore 的布局直接写入闸门（scopes/<摘要>.gate），模拟崩溃点。
func TestResultStoreGetAfterTombstoneBeforeRemoval(t *testing.T) {
	dir := t.TempDir()
	s := newFileResultStore(t, dir)
	ctx := WithResultScope(context.Background(), "run")
	data := []byte("evidence")
	ref := NewRef("run", "p", "k", data)
	require.NoError(t, s.PutIfAbsent(ctx, ref, data))
	got, err := s.Get(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, data, got)

	gate := filepath.Join(dir, "scopes", digest([]byte("run"))+".gate")
	require.NoError(t, os.WriteFile(gate, []byte("removed\n"), 0600))
	before, beforeDirs := diskFiles(t, dir)
	for _, r := range []Ref{ref, NewRef("run", "p", "missing", data)} {
		_, err = s.Get(ctx, r)
		require.ErrorIs(t, err, ErrReleased)
		require.ErrorIs(t, err, durable.ErrScopeRemoved)
	}
	after, afterDirs := diskFiles(t, dir)
	require.Equal(t, before, after, "读取不产生文件")
	require.Equal(t, beforeDirs, afterDirs)
	require.Greater(t, len(after), 1, "证据仍残留在磁盘上")

	require.NoError(t, s.Release(ctx, "run"), "再次释放清理残留")
	requireOnlyTombstones(t, dir, 1)
}
