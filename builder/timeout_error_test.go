package builder

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

// 运行超时须以超时错误报告。曾有两处随机丢失：终态事件与已结束的执行 ctx 竞争；提供方在 ctx 结束后不发终态关闭流，
// readToolTurn 当成连接中断。调用方只看到 io.ErrUnexpectedEOF。
func TestRunTimeoutIsReportedAsDeadline(t *testing.T) {
	for i := range 50 {
		hang := step{run: func(ctx context.Context, _ func(*llm.StreamChunk) bool) { <-ctx.Done() }}
		agent, err := NewSimpleBuilder(newScriptModel(hang)).WithTools(plainTool()).WithTimeout(5 * time.Millisecond).Build()
		require.NoError(t, err)
		_, err = agent.AsRunnable().Invoke(context.Background(), "q")
		require.ErrorIs(t, err, context.DeadlineExceeded, "run %d", i)
		require.NotErrorIs(t, err, io.ErrUnexpectedEOF, "run %d", i)
	}
}
