// Package storetest 是 durable.Store 实现的一致性测试，覆盖 durable.Store 与 durable.Scoped 文档中的契约。
// 后端实现在自己的测试中调用 Run，用同一后端上的两个独立实例模拟两个进程。
package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
)

// Options 选择要运行的可选用例。
type Options struct {
	// Scoped 为真时两个实例都须实现 durable.Scoped，并运行作用域用例。
	Scoped bool
}

// blockedWait 是判定 Lock 处于等待状态所用的时长。
const blockedWait = 150 * time.Millisecond

// Run 对 newPair 返回的两个实例运行全部用例；每个用例调用一次 newPair，两个实例须共享同一后端且初始为空。
func Run(t *testing.T, newPair func(t *testing.T) (a, b durable.Store), opts Options) {
	cases := []struct {
		name string
		run  func(*testing.T, durable.Store, durable.Store)
	}{
		{"互斥", testMutualExclusion},
		{"不可重入", testNotReentrant},
		{"不同ID互不阻塞", testIndependentIDs},
		{"同实例同时持有多个ID", testNestedLocks},
		{"跨实例读写", testRoundTrip},
		{"不存在返回ErrNotExist", testNotExist},
		{"取消后仍可写入", testSaveAfterCancel},
		{"写入原子", testAtomicSave},
		{"审批跨实例续跑", testRunnerResume},
		{"操作跨实例回放", testOperationReplay},
	}
	if opts.Scoped {
		cases = append(cases, []struct {
			name string
			run  func(*testing.T, durable.Store, durable.Store)
		}{
			{"作用域隔离", testScopeIsolation},
			{"作用域与根同名ID互不阻塞", testScopeNestedLocks},
			{"作用域删除", testScopeRemoval},
			{"持锁期间删除作用域", testScopeRemovalWhileLocked},
			{"墓碑回收", testPruneRemoved},
		}...)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b := newPair(t)
			require.NotNil(t, a)
			require.NotNil(t, b)
			c.run(t, a, b)
		})
	}
}

func lock(t *testing.T, s durable.Store, id string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := s.Lock(ctx, id)
	require.NoError(t, err)
	return func() { require.NoError(t, unlock()) }
}

func requireBlocked(t *testing.T, s durable.Store, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), blockedWait)
	defer cancel()
	start := time.Now()
	unlock, err := s.Lock(ctx, id)
	if err == nil {
		_ = unlock()
	}
	require.ErrorIs(t, err, durable.ErrLockUnavailable)
	require.ErrorIs(t, err, context.DeadlineExceeded, "须保留 ctx 错误")
	require.GreaterOrEqual(t, time.Since(start), blockedWait*8/10, "Lock 应等待到 ctx 结束")
}

func save(t *testing.T, s durable.Store, id, version string) {
	t.Helper()
	unlock := lock(t, s, id)
	defer unlock()
	require.NoError(t, s.Save(context.Background(), id, &durable.Snapshot{Version: version, RunID: id, Data: json.RawMessage(`{"v":"` + version + `"}`)}))
}

func requireVersion(t *testing.T, s durable.Store, id, version string) {
	t.Helper()
	snap, err := s.Load(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, snap)
	require.Equal(t, version, snap.Version)
	require.Equal(t, id, snap.RunID)
	require.JSONEq(t, `{"v":"`+version+`"}`, string(snap.Data))
}

func testMutualExclusion(t *testing.T, a, b durable.Store) {
	unlock := lock(t, a, "run")
	requireBlocked(t, b, "run")

	acquired := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		u, err := b.Lock(ctx, "run")
		if err == nil {
			err = u()
		}
		acquired <- err
	}()
	time.Sleep(50 * time.Millisecond)
	select {
	case err := <-acquired:
		t.Fatalf("a 持锁期间 b 不应获得锁：%v", err)
	default:
	}
	unlock()
	require.NoError(t, <-acquired, "释放后等待中的 Lock 应获得锁")
}

func testNotReentrant(t *testing.T, a, _ durable.Store) {
	unlock := lock(t, a, "run")
	defer unlock()
	requireBlocked(t, a, "run")
}

func testIndependentIDs(t *testing.T, a, b durable.Store) {
	unlock := lock(t, a, "run-1")
	defer unlock()
	lock(t, b, "run-2")()
}

// testNestedLocks 对应图、操作、预算在同一进程内的嵌套持锁。
func testNestedLocks(t *testing.T, a, b durable.Store) {
	unlock1 := lock(t, a, "run-1")
	defer unlock1()
	unlock2 := lock(t, a, "run-2")
	defer unlock2()
	lock(t, b, "run-3")()
}

func testRoundTrip(t *testing.T, a, b durable.Store) {
	save(t, a, "run", "v1")
	requireVersion(t, b, "run", "v1")
	save(t, b, "run", "v2")
	requireVersion(t, a, "run", "v2")

	full := &durable.Snapshot{Version: "v3", Plan: "plan", RunID: "run", Next: 2, Data: json.RawMessage(`{"k":[1,2]}`), Waiting: true, Approved: true, Key: "key"}
	unlock := lock(t, a, "run")
	require.NoError(t, a.Save(context.Background(), "run", full))
	unlock()
	got, err := b.Load(context.Background(), "run")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.JSONEq(t, string(full.Data), string(got.Data))
	want, loaded := *full, *got
	want.Data, loaded.Data = nil, nil
	require.Equal(t, want, loaded, "Snapshot 的全部字段须原样往返")
}

func testNotExist(t *testing.T, a, _ durable.Store) {
	_, err := a.Load(context.Background(), "missing")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func testSaveAfterCancel(t *testing.T, a, b durable.Store) {
	ctx, cancel := context.WithCancel(context.Background())
	unlock, err := a.Lock(ctx, "run")
	require.NoError(t, err)
	cancel()
	snap := &durable.Snapshot{Version: "v1", RunID: "run", Data: json.RawMessage(`{"v":"v1"}`)}
	require.NoError(t, a.Save(context.WithoutCancel(ctx), "run", snap))
	require.NoError(t, unlock())
	requireVersion(t, b, "run", "v1")
}

// testAtomicSave 在 a 上持续写入不同大小的快照，同时从 b 读取：每次读到的都必须是某个完整快照。
func testAtomicSave(t *testing.T, a, b durable.Store) {
	const writes = 50
	unlock := lock(t, a, "run")
	done := make(chan struct{})
	var wg sync.WaitGroup
	var readErr error
	stop := sync.OnceFunc(func() {
		close(done)
		wg.Wait()
		unlock()
	})
	defer stop()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			snap, err := b.Load(context.Background(), "run")
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err == nil {
				err = checkPadded(snap)
			}
			if err != nil {
				readErr = err
				return
			}
		}
	}()
	for i := 0; i < writes; i++ {
		data, _ := json.Marshal(map[string]any{"n": i, "pad": strings.Repeat("x", (i%5)*4096)})
		require.NoError(t, a.Save(context.Background(), "run", &durable.Snapshot{Version: fmt.Sprint(i), RunID: "run", Data: data}))
	}
	stop()
	require.NoError(t, readErr)
	snap, err := b.Load(context.Background(), "run")
	require.NoError(t, err)
	require.Equal(t, fmt.Sprint(writes-1), snap.Version)
}

func checkPadded(snap *durable.Snapshot) error {
	var data struct {
		N   int    `json:"n"`
		Pad string `json:"pad"`
	}
	if snap.RunID != "run" || json.Unmarshal(snap.Data, &data) != nil || snap.Version != fmt.Sprint(data.N) || len(data.Pad) != (data.N%5)*4096 {
		return fmt.Errorf("读到不完整的快照：version=%q run=%q", snap.Version, snap.RunID)
	}
	return nil
}

// testRunnerResume 在 a 上执行到审批等待，在 b 上用审批键续跑：已完成的步骤不重复执行。
func testRunnerResume(t *testing.T, a, b durable.Store) {
	ctx := context.Background()
	var mu sync.Mutex
	calls := map[string]int{}
	steps := []durable.Step{
		{Name: "charge", Run: func(_ context.Context, in durable.StepInput) (json.RawMessage, error) {
			mu.Lock()
			calls["charge"]++
			mu.Unlock()
			return in.Data, nil
		}},
		{Name: "ship", RequireApproval: true, Run: func(_ context.Context, in durable.StepInput) (json.RawMessage, error) {
			mu.Lock()
			calls["ship"]++
			mu.Unlock()
			return json.RawMessage(`"shipped"`), nil
		}},
	}
	first := &durable.Runner{Store: a, Version: "v1", Steps: steps}
	snap, err := first.Run(ctx, "order", json.RawMessage(`{"amount":1}`), "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.True(t, snap.Waiting)

	second := &durable.Runner{Store: b, Version: "v1", Steps: steps}
	snap, err = second.Run(ctx, "order", nil, snap.Key)
	require.NoError(t, err)
	require.Equal(t, 2, snap.Next)
	require.JSONEq(t, `"shipped"`, string(snap.Data))
	require.Equal(t, map[string]int{"charge": 1, "ship": 1}, calls)
}

// testOperationReplay 在 a 上完成的持久操作，在 b 上以同一执行键再次调用时回放，不再执行。
func testOperationReplay(t *testing.T, a, b durable.Store) {
	calls := 0
	run := func(context.Context) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"ok":true}`), nil
	}
	ctxA := durable.WithJournal(durable.WithExecutionKey(context.Background(), "root"), a)
	record, err := durable.Do(ctxA, "effect", json.RawMessage(`{}`), durable.OperationPolicy{}, run)
	require.NoError(t, err)
	require.False(t, record.Replayed)

	ctxB := durable.WithJournal(durable.WithExecutionKey(context.Background(), "root"), b)
	record, err = durable.Do(ctxB, "effect", json.RawMessage(`{}`), durable.OperationPolicy{}, run)
	require.NoError(t, err)
	require.True(t, record.Replayed)
	require.JSONEq(t, `{"ok":true}`, string(record.Value))
	require.Equal(t, 1, calls)
}

func scoped(t *testing.T, s durable.Store) durable.Scoped {
	t.Helper()
	sc, ok := s.(durable.Scoped)
	require.True(t, ok, "Options.Scoped 要求实例实现 durable.Scoped")
	return sc
}

func testScopeIsolation(t *testing.T, a, b durable.Store) {
	save(t, a, "run", "root")
	save(t, scoped(t, a).Scope("s1"), "run", "s1")
	requireVersion(t, b, "run", "root")
	requireVersion(t, scoped(t, b).Scope("s1"), "run", "s1")
	_, err := scoped(t, b).Scope("s2").Load(context.Background(), "run")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func testScopeNestedLocks(t *testing.T, a, _ durable.Store) {
	unlock := lock(t, a, "run")
	defer unlock()
	lock(t, scoped(t, a).Scope("s"), "run")()
}

func requireRemoved(t *testing.T, s durable.Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.Load(ctx, "run")
	require.ErrorIs(t, err, durable.ErrScopeRemoved)
	unlock, err := s.Lock(ctx, "run")
	if err == nil {
		_ = unlock()
	}
	require.ErrorIs(t, err, durable.ErrScopeRemoved)
}

func testScopeRemoval(t *testing.T, a, b durable.Store) {
	ctx := context.Background()
	save(t, a, "run", "root")
	save(t, scoped(t, a).Scope("gone"), "run", "gone")
	save(t, scoped(t, a).Scope("kept"), "run", "kept")

	require.NoError(t, scoped(t, b).RemoveScope(ctx, "gone"))
	requireRemoved(t, scoped(t, a).Scope("gone"))
	requireRemoved(t, scoped(t, b).Scope("gone"))
	require.NoError(t, scoped(t, a).RemoveScope(ctx, "gone"), "重复删除应成功")
	requireRemoved(t, scoped(t, a).Scope("gone"))

	requireVersion(t, b, "run", "root")
	requireVersion(t, scoped(t, b).Scope("kept"), "run", "kept")

	require.NoError(t, scoped(t, a).RemoveScope(ctx, "never-used"))
	requireRemoved(t, scoped(t, b).Scope("never-used"))
}

// testScopeRemovalWhileLocked 接受两种实现：RemoveScope 等待持锁者释放（FileStore），
// 或立即返回并拒绝持锁者之后的写入。
func testScopeRemovalWhileLocked(t *testing.T, a, b durable.Store) {
	scope, remover := scoped(t, a).Scope("s"), scoped(t, b)
	unlock := lock(t, scope, "run")
	removed := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		removed <- remover.RemoveScope(ctx, "s")
	}()
	snap := &durable.Snapshot{Version: "v1", RunID: "run", Data: json.RawMessage(`{}`)}
	select {
	case err := <-removed:
		require.NoError(t, err)
		require.ErrorIs(t, scope.Save(context.Background(), "run", snap), durable.ErrScopeRemoved, "删除返回后持锁者的写入须被拒绝")
		unlock()
	case <-time.After(blockedWait):
		require.NoError(t, scope.Save(context.Background(), "run", snap), "删除仍在等待时持锁者照常写入")
		unlock()
		require.NoError(t, <-removed)
	}
	requireRemoved(t, scoped(t, a).Scope("s"))
}

// pruneMargin 容忍后端写墓碑所用时钟与本机时钟的偏差。
const pruneMargin = 300 * time.Millisecond

func testPruneRemoved(t *testing.T, a, b durable.Store) {
	ctx := context.Background()
	save(t, scoped(t, a).Scope("gone"), "run", "gone")
	require.NoError(t, scoped(t, a).RemoveScope(ctx, "gone"))

	require.NoError(t, scoped(t, b).PruneRemoved(ctx, time.Hour))
	requireRemoved(t, scoped(t, a).Scope("gone"))

	time.Sleep(2 * pruneMargin)
	require.NoError(t, scoped(t, b).RemoveScope(ctx, "gone"), "重复删除不刷新墓碑时间")
	require.NoError(t, scoped(t, b).PruneRemoved(ctx, pruneMargin))
	_, err := scoped(t, a).Scope("gone").Load(ctx, "run")
	require.ErrorIs(t, err, os.ErrNotExist, "墓碑回收后同名作用域视为不存在")
}
