package main

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

// 示例离线运行：每个场景都应写出完成标记。不能只依赖 ctx 超时，图会等节点关闭流，不响应取消的节点会让调用挂住。
func TestMultiAgentPatternsDemo(t *testing.T) {
	out := &lockedWriter{}
	done := make(chan error, 1)
	go func() { done <- run(context.Background(), out) }()
	select {
	case err := <-done:
		require.NoError(t, err, out.String())
	case <-time.After(30 * time.Second):
		t.Fatalf("示例未在 30 秒内结束：\n%s", out.String())
	}
	for _, marker := range []string{"S1 完成", "S2 完成", "S3 完成", "S5 完成", "S7 完成", "S9 完成", "组合流程 完成"} {
		require.Contains(t, out.String(), marker+"\n")
	}
}
