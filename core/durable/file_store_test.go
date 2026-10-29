package durable

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/testing/panictest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func saveScope(ctx context.Context, store *FileStore, name, id string) error {
	return store.Scope(name).Save(ctx, id, &Snapshot{RunID: id, Version: "v1"})
}

// backdate 把作用域墓碑的写入时间改到 age 之前。
func backdate(t *testing.T, store *FileStore, name string, age time.Duration) {
	t.Helper()
	at := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(store.scope(name).gate, at, at))
}

// 真实子进程在 RemoveScope 写完墓碑、删除目录之前退出（持有排他锁时死亡）：墓碑已生效，
// 重启后的实例写入一直被拒绝，再次 RemoveScope 能拿到锁（flock 随进程死亡释放）并清理干净。
func TestRemoveScopeCrashAfterTombstoneProcess(t *testing.T) {
	if dir := os.Getenv("GOAGENT_SCOPE_CRASH_DIR"); dir != "" {
		store, err := NewFileStore(dir)
		require.NoError(t, err)
		afterTombstone = func() { os.Exit(3) }
		_ = store.RemoveScope(context.Background(), "run")
		t.Fatal("子进程应在墓碑写入后退出")
	}
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	require.NoError(t, err)
	require.NoError(t, saveScope(ctx, store, "run", "a"))

	cmd := exec.Command(os.Args[0], "-test.run=^TestRemoveScopeCrashAfterTombstoneProcess$")
	cmd.Env = append(os.Environ(), "GOAGENT_SCOPE_CRASH_DIR="+dir)
	output, err := panictest.CombinedOutput(cmd, panictest.ChildTimeout(t, time.Minute))
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, string(output))
	require.Equal(t, 3, exit.ExitCode(), string(output))

	scope := store.scope("run")
	gate, err := os.ReadFile(scope.gate)
	require.NoError(t, err)
	require.Equal(t, scopeTombstone, gate, "墓碑已落盘")
	_, err = os.Stat(filepath.Join(scope.files.dir, digest("a")+".json"))
	require.NoError(t, err, "目录尚未删除，模拟崩溃点正确")

	restarted, err := NewFileStore(dir)
	require.NoError(t, err)
	require.ErrorIs(t, saveScope(ctx, restarted, "run", "late"), ErrScopeRemoved)
	_, err = restarted.Scope("run").Lock(ctx, "late")
	require.ErrorIs(t, err, ErrScopeRemoved)

	lockCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	require.NoError(t, restarted.RemoveScope(lockCtx, "run"), "死亡进程的排他锁已释放")
	_, err = os.Stat(scope.files.dir)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.ErrorIs(t, saveScope(ctx, restarted, "run", "late"), ErrScopeRemoved)
	entries, err := os.ReadDir(filepath.Join(dir, scopesDir))
	require.NoError(t, err)
	require.Len(t, entries, 1, "只剩墓碑")
}

// PruneRemoved 只回收过期墓碑（连同崩溃残留目录），未过期墓碑仍拒绝写入，未删除的作用域不受影响。
func TestPruneRemoved(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.PruneRemoved(ctx, time.Hour), "没有作用域时为空操作")

	for _, name := range []string{"old", "fresh", "crashed", "live"} {
		require.NoError(t, saveScope(ctx, store, name, "a"))
	}
	require.NoError(t, store.RemoveScope(ctx, "old"))
	require.NoError(t, store.RemoveScope(ctx, "fresh"))
	require.NoError(t, writeTombstone(store.scope("crashed").gate), "模拟墓碑写入后崩溃，目录残留")
	backdate(t, store, "old", 2*time.Hour)
	backdate(t, store, "crashed", 2*time.Hour)
	backdate(t, store, "live", 2*time.Hour)

	require.ErrorContains(t, store.PruneRemoved(ctx, -time.Second), "不能为负")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, store.PruneRemoved(cancelled, time.Hour), context.Canceled)

	require.NoError(t, store.PruneRemoved(ctx, time.Hour))
	for _, name := range []string{"old", "crashed"} {
		_, err = os.Stat(store.scope(name).gate)
		require.ErrorIs(t, err, os.ErrNotExist, name)
		_, err = os.Stat(store.scope(name).files.dir)
		require.ErrorIs(t, err, os.ErrNotExist, name)
	}
	require.ErrorIs(t, saveScope(ctx, store, "fresh", "late"), ErrScopeRemoved, "未过期墓碑仍拒绝")
	_, err = store.Scope("live").Load(ctx, "a")
	require.NoError(t, err, "未删除的作用域不受影响")
	require.NoError(t, saveScope(ctx, store, "live", "b"))
	require.NoError(t, saveScope(ctx, store, "old", "late"), "墓碑回收后迟到写入不再被拒绝（保证边界）")
}

// 回收与写入并发（多个实例模拟多进程）：未过期墓碑始终拒绝；过期作用域的写入要么被拒绝，
// 要么发生在回收之后并且保留下来——回收方不会删掉锁在已删除旧闸门上之后新写入的数据。
func TestPruneRemovedConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	stores := make([]*FileStore, 3)
	for i := range stores {
		var err error
		stores[i], err = NewFileStore(dir)
		require.NoError(t, err)
	}
	const scopes = 8
	for i := range scopes {
		for _, name := range []string{fmt.Sprintf("old-%d", i), fmt.Sprintf("fresh-%d", i)} {
			require.NoError(t, stores[0].RemoveScope(ctx, name))
		}
		backdate(t, stores[0], fmt.Sprintf("old-%d", i), 2*time.Hour)
	}
	var mu sync.Mutex
	written := map[[2]string]bool{}
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Go(func() {
			store := stores[w%len(stores)]
			for round := range 20 {
				i := round % scopes
				assert.ErrorIs(t, saveScope(ctx, store, fmt.Sprintf("fresh-%d", i), "x"), ErrScopeRemoved)
				name, id := fmt.Sprintf("old-%d", i), fmt.Sprintf("w%d-%d", w, round)
				err := saveScope(ctx, store, name, id)
				if err == nil {
					mu.Lock()
					written[[2]string{name, id}] = true
					mu.Unlock()
				} else {
					assert.ErrorIs(t, err, ErrScopeRemoved)
				}
			}
		})
	}
	for p := range 3 {
		wg.Go(func() {
			for range 10 {
				assert.NoError(t, stores[p].PruneRemoved(ctx, time.Hour))
			}
		})
	}
	wg.Wait()
	require.NoError(t, stores[0].PruneRemoved(ctx, time.Hour))
	for key := range written {
		_, err := stores[0].Scope(key[0]).Load(ctx, key[1])
		require.NoError(t, err, "回收之后写入的数据不被删除：%s", key)
	}
	for i := range scopes {
		gate, err := os.ReadFile(stores[0].scope(fmt.Sprintf("fresh-%d", i)).gate)
		require.NoError(t, err)
		require.Equal(t, scopeTombstone, gate, "未过期墓碑保留")
		if gate, err = os.ReadFile(stores[0].scope(fmt.Sprintf("old-%d", i)).gate); err == nil {
			require.Empty(t, gate, "过期墓碑已回收，残留的只能是之后写入重建的空闸门")
		} else {
			require.ErrorIs(t, err, os.ErrNotExist)
		}
	}
}

// RemoveScope 在墓碑落盘后、删除目录前中断（测试钩子模拟崩溃）：残留数据仍在磁盘上，
// 但无锁 Load 先看到墓碑，返回 ErrScopeRemoved 且不产生任何文件；墓碑回收后返回 os.ErrNotExist。
func TestLoadAfterTombstoneBeforeRemoval(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	require.NoError(t, err)
	require.NoError(t, saveScope(ctx, store, "run", "a"))

	crash := errors.New("模拟崩溃")
	afterTombstone = func() { panic(crash) }
	t.Cleanup(func() { afterTombstone = nil })
	require.PanicsWithValue(t, crash, func() { _ = store.RemoveScope(ctx, "run") })
	afterTombstone = nil

	scope := store.scope("run")
	_, err = os.Stat(filepath.Join(scope.files.dir, digest("a")+".json"))
	require.NoError(t, err, "目录尚未删除，模拟崩溃点正确")
	before := listTree(t, dir)
	for _, id := range []string{"a", "missing"} {
		_, err = store.Scope("run").Load(ctx, id)
		require.ErrorIs(t, err, ErrScopeRemoved, id)
	}
	require.Equal(t, before, listTree(t, dir), "读取不产生任何文件")

	backdate(t, store, "run", 2*time.Hour)
	require.NoError(t, store.PruneRemoved(ctx, time.Hour))
	_, err = store.Scope("run").Load(ctx, "a")
	require.ErrorIs(t, err, os.ErrNotExist, "墓碑回收后连同残留一起删除")
}

func listTree(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	require.NoError(t, filepath.Walk(dir, func(path string, _ os.FileInfo, err error) error {
		paths = append(paths, path)
		return err
	}))
	return paths
}
