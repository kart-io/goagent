package main

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/memory"
)

// lockedWriter 让示例写入与测试读取共用一把锁，超时分支读取输出时不触发 -race。
type lockedWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *lockedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// 示例离线运行：每个场景都应写出完成标记。Worker 或 Agent 挂住时由超时兜底，并打印已有输出便于定位。
func TestMemoryDemo(t *testing.T) {
	out := &lockedWriter{}
	done := make(chan error, 1)
	go func() { done <- run(context.Background(), out) }()
	select {
	case err := <-done:
		require.NoError(t, err, out.String())
	case <-time.After(60 * time.Second):
		t.Fatalf("示例未在 60 秒内结束：\n%s", out.String())
	}
	for _, marker := range []string{"A 隔离与删除 完成", "B 抽取管线 完成", "C 缓存一致性 完成", "D 预算与保留 完成"} {
		require.Contains(t, out.String(), marker+"\n")
	}
}

// 条目 17：并发写入只断言不变量，不断言谁赢。起跑线用 channel 对齐，结果与调度顺序无关。
func TestMemoryConcurrentWrites(t *testing.T) {
	const writers = 8
	cases := []struct {
		name string
		// write 是第 i 个写者的操作；setup 返回的值在所有写者间共享。
		setup func(t *testing.T, ctx context.Context, m *memory.Memory) int64
		write func(ctx context.Context, m *memory.Memory, i int, rev int64) error
		after func(t *testing.T, ctx context.Context, m *memory.Memory)
	}{
		{
			name:  "同一 ID 并发 Remember 只有一个成功",
			setup: func(*testing.T, context.Context, *memory.Memory) int64 { return 0 },
			write: func(ctx context.Context, m *memory.Memory, i int, _ int64) error {
				_, err := m.Remember(ctx, memory.Record{ID: "same", Kind: memory.KindFact, Content: fmt.Sprintf("写者 %d 的版本", i)})
				return err
			},
			after: func(t *testing.T, ctx context.Context, m *memory.Memory) {
				all, err := m.Recall(ctx, memory.Query{})
				require.NoError(t, err)
				require.Len(t, all, 1)
			},
		},
		{
			name: "同一 Revision 并发 Update 只有一个成功",
			setup: func(t *testing.T, ctx context.Context, m *memory.Memory) int64 {
				r, err := m.Remember(ctx, memory.Record{ID: "shared", Kind: memory.KindFact, Content: "初始版本"})
				require.NoError(t, err)
				return r.Revision
			},
			write: func(ctx context.Context, m *memory.Memory, i int, rev int64) error {
				content := fmt.Sprintf("写者 %d 的修改", i)
				_, err := m.Update(ctx, "shared", rev, memory.Patch{Content: &content})
				return err
			},
			after: func(t *testing.T, ctx context.Context, m *memory.Memory) {
				r, err := m.Get(ctx, "shared")
				require.NoError(t, err)
				require.Equal(t, int64(2), r.Revision, "只应发生一次修改")
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, err := newEnv(t.TempDir(), "concurrent", memory.Config{})
			require.NoError(t, err)
			ctx := memory.WithNamespace(context.Background(), alice...)
			rev := c.setup(t, ctx, e.mem)
			start := make(chan struct{})
			errs := make([]error, writers)
			var wg sync.WaitGroup
			for i := range writers {
				wg.Go(func() {
					<-start
					errs[i] = c.write(ctx, e.mem, i, rev)
				})
			}
			close(start)
			wg.Wait()
			ok := 0
			for _, err := range errs {
				if err == nil {
					ok++
					continue
				}
				require.True(t, agentErrors.IsCode(err, agentErrors.CodeConflict), "失败的写者应得到 CodeConflict：%v", err)
			}
			require.Equal(t, 1, ok)
			c.after(t, ctx, e.mem)
		})
	}
}
