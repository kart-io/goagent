package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 示例不依赖外部服务：完整运行一遍，每个审批都应被响应，不出现 not found 或长时间挂起。
func TestHumanInLoopDemo(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	stdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = stdout })

	output := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		output <- buf.String()
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		main()
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		_ = w.Close()
		t.Fatalf("示例未在 30 秒内结束，审批响应可能未送达：\n%s", <-output)
	}
	require.NoError(t, w.Close())
	out := <-output

	require.NotContains(t, out, "Error:", out)
	require.Contains(t, out, "→ Proceeding with database deletion", out)
	require.Equal(t, 4, strings.Count(out, "  Approved: true\n\n"), out)
	require.Contains(t, out, "region: us-west-2", out)
	require.Contains(t, out, "State successfully persisted:", out)
	require.Contains(t, out, "Total interrupts resolved: 2", out)
	require.Contains(t, out, "=== Demo Complete ===", out)
}
