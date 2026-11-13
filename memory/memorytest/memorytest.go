// Package memorytest 是长期记忆在某个 durable.Store 后端上的一致性测试：只经公开 API，
// 用同一后端上的两个独立实例模拟两个进程，检查记录的读写、冲突、遗忘、清空与删除日志在实例之间一致。
// 后端实现在自己的测试中调用 Run。
package memorytest

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/memory"
)

// Run 对 newPair 返回的两个实例运行全部用例；每个用例调用一次 newPair，两个实例须共享同一后端、初始为空且实现 durable.Scoped。
func Run(t *testing.T, newPair func(t *testing.T) (a, b durable.Store)) {
	cases := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, a, b *memory.Memory)
	}{
		{"跨实例读写", testRoundTrip},
		{"重复ID冲突", testDuplicateID},
		{"修订号冲突", testRevisionConflict},
		{"并发写入不丢失", testConcurrentWrites},
		{"交替写入", testInterleavedUpdates},
		{"遗忘跨实例生效", testForget},
		{"清空跨实例生效", testPurge},
		{"清空与进行中的写入", testPurgeInflight},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sa, sb := newPair(t)
			ctx := memory.WithNamespace(context.Background(), "t1", "alice")
			c.run(t, ctx, newMemory(t, sa), newMemory(t, sb))
		})
	}
}

func newMemory(t *testing.T, st durable.Store) *memory.Memory {
	t.Helper()
	m, err := memory.New(memory.Config{Durable: st, RollbackWindow: time.Hour})
	require.NoError(t, err)
	return m
}

func record(id, content string) memory.Record {
	return memory.Record{ID: id, Content: content, Keywords: []string{"咖啡"}, Importance: 0.5}
}

func testRoundTrip(t *testing.T, ctx context.Context, a, b *memory.Memory) {
	_, err := a.Remember(ctx, record("r1", "用户喜欢手冲咖啡"))
	require.NoError(t, err)
	got, err := b.Get(ctx, "r1")
	require.NoError(t, err)
	require.Equal(t, "用户喜欢手冲咖啡", got.Content)

	content := "用户改喝拿铁咖啡"
	updated, err := b.Update(ctx, "r1", got.Revision, memory.Patch{Content: &content})
	require.NoError(t, err)
	require.Equal(t, got.Revision+1, updated.Revision)
	require.Equal(t, got.ContentRev+1, updated.ContentRev)

	got, err = a.Get(ctx, "r1")
	require.NoError(t, err)
	require.Equal(t, content, got.Content, "另一实例的提交对本实例立即可见")
	hits, err := a.Recall(ctx, memory.Query{Text: "拿铁"})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, "r1", hits[0].ID)
}

func testDuplicateID(t *testing.T, ctx context.Context, a, b *memory.Memory) {
	_, err := a.Remember(ctx, record("r1", "第一条"))
	require.NoError(t, err)
	_, err = b.Remember(ctx, record("r1", "另一实例的同 ID 记录"))
	require.True(t, agentErrors.IsCode(err, agentErrors.CodeConflict), "得到 %v", err)
	got, err := a.Get(ctx, "r1")
	require.NoError(t, err)
	require.Equal(t, "第一条", got.Content)
}

func testRevisionConflict(t *testing.T, ctx context.Context, a, b *memory.Memory) {
	r, err := a.Remember(ctx, record("r1", "原文"))
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, m := range []*memory.Memory{a, b} {
		wg.Go(func() {
			content := fmt.Sprintf("实例 %d 的修改", i)
			_, errs[i] = m.Update(ctx, "r1", r.Revision, memory.Patch{Content: &content})
		})
	}
	wg.Wait()
	conflicts := 0
	for _, err := range errs {
		if err != nil {
			require.True(t, agentErrors.IsCode(err, agentErrors.CodeConflict), "得到 %v", err)
			conflicts++
		}
	}
	require.Equal(t, 1, conflicts, "同一修订号只有一个修改成功")
	got, err := b.Get(ctx, "r1")
	require.NoError(t, err)
	require.Equal(t, r.Revision+1, got.Revision)
}

func testConcurrentWrites(t *testing.T, ctx context.Context, a, b *memory.Memory) {
	const n = 10
	var wg sync.WaitGroup
	for i, m := range []*memory.Memory{a, b} {
		wg.Go(func() {
			for j := range n {
				_, err := m.Remember(ctx, record(fmt.Sprintf("m%d-%02d", i, j), fmt.Sprintf("实例 %d 的第 %d 条", i, j)))
				require.NoError(t, err)
			}
		})
	}
	wg.Wait()
	for _, m := range []*memory.Memory{a, b} {
		for i := range 2 {
			for j := range n {
				_, err := m.Get(ctx, fmt.Sprintf("m%d-%02d", i, j))
				require.NoError(t, err)
			}
		}
	}
}

// 两个实例轮流在对方的提交之上修改，覆盖增量提交与（达到阈值时的）压缩。
func testInterleavedUpdates(t *testing.T, ctx context.Context, a, b *memory.Memory) {
	r, err := a.Remember(ctx, record("r1", "第 0 版"))
	require.NoError(t, err)
	ms := []*memory.Memory{a, b}
	const n = 60
	for i := 1; i <= n; i++ {
		content := fmt.Sprintf("第 %d 版：%s", i, "用户提到咖啡与深圳的天气")
		r, err = ms[i%2].Update(ctx, "r1", r.Revision, memory.Patch{Content: &content})
		require.NoError(t, err)
	}
	for _, m := range ms {
		got, err := m.Get(ctx, "r1")
		require.NoError(t, err)
		require.Equal(t, int64(n+1), got.Revision)
		require.Equal(t, r.Content, got.Content)
	}
}

func testForget(t *testing.T, ctx context.Context, a, b *memory.Memory) {
	r, err := a.Remember(ctx, record("r1", "用户的银行卡号是 1234"))
	require.NoError(t, err)
	require.NoError(t, b.Forget(ctx, "r1"))

	got, err := a.Get(ctx, "r1")
	require.NoError(t, err)
	require.Equal(t, memory.ReasonForgotten, got.InvalidReason)
	require.Empty(t, got.Content)
	hits, err := a.Recall(ctx, memory.Query{Text: "银行卡"})
	require.NoError(t, err)
	require.Empty(t, hits)
	_, err = a.Update(ctx, "r1", r.Revision, memory.Patch{Content: &r.Content})
	require.True(t, agentErrors.IsCode(err, agentErrors.CodeConflict), "遗忘是终态，得到 %v", err)

	log, err := a.Deletions(ctx)
	require.NoError(t, err)
	require.Len(t, log.Forgotten, 1)
	require.Equal(t, "r1", log.Forgotten[0].ID)
}

func testPurge(t *testing.T, ctx context.Context, a, b *memory.Memory) {
	for i := range 3 {
		_, err := a.Remember(ctx, record(fmt.Sprintf("r%d", i), "机密：项目代号青鸟"))
		require.NoError(t, err)
	}
	_, err := b.Get(ctx, "r0") // 让 b 缓存清空前的 state
	require.NoError(t, err)
	require.NoError(t, a.Purge(ctx))

	_, err = b.Get(ctx, "r0")
	require.True(t, agentErrors.IsCode(err, agentErrors.CodeStoreNotFound), "得到 %v", err)
	hits, err := b.Recall(ctx, memory.Query{Text: "青鸟"})
	require.NoError(t, err)
	require.Empty(t, hits)
	log, err := b.Deletions(ctx)
	require.NoError(t, err)
	require.Len(t, log.Purges, 1)

	_, err = b.Remember(ctx, record("r0", "清空后重新写入"))
	require.NoError(t, err, "清空后可以复用 ID")
}

// testPurgeInflight：b 持续写入时 a 两次清空。进行中的写入要么失败（CodeConflict），要么落在某个作用域；
// 在两次清空都返回之后开始的写入都在当前作用域，a 能读到。
func testPurgeInflight(t *testing.T, ctx context.Context, a, b *memory.Memory) {
	var purges atomic.Int32
	var stop atomic.Bool
	var after []string
	errs := make(chan error, 1)
	go func() {
		defer close(errs)
		for i := 0; !stop.Load() || len(after) < 5; i++ {
			started := purges.Load()
			r, err := b.Remember(ctx, record(fmt.Sprintf("w%d", i), "进行中的写入"))
			switch {
			case err != nil && !agentErrors.IsCode(err, agentErrors.CodeConflict):
				errs <- err
				return
			case err == nil && started == 2:
				after = append(after, r.ID)
			}
		}
	}()
	for range 2 {
		require.NoError(t, a.Purge(ctx))
		purges.Add(1)
	}
	stop.Store(true)
	require.NoError(t, <-errs)
	for _, id := range after {
		_, err := a.Get(ctx, id)
		require.NoError(t, err, "清空返回后开始的写入 %s 在当前作用域", id)
	}
	log, err := b.Deletions(ctx)
	require.NoError(t, err)
	require.Len(t, log.Purges, 2)
}
