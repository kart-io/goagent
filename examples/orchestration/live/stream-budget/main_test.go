package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/internal/livemodel/livemodeltest"
	"github.com/kart-io/goagent/examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

// chunkedModel 按给定片段逐块流式输出，模拟逐 token 的真实提供方。
type chunkedModel struct {
	demomodel.Model
	parts []string
}

func (m *chunkedModel) CompleteStream(ctx context.Context, _ *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	m.Calls.Add(1)
	ch := make(chan *llm.StreamChunk, len(m.parts)+1)
	content := ""
	for _, p := range m.parts {
		content += p
		ch <- &llm.StreamChunk{Delta: p, Content: content}
	}
	ch <- &llm.StreamChunk{Content: content, Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: len(m.parts)}}
	close(ch)
	return ch, nil
}
func (m *chunkedModel) CompleteToolsStream(ctx context.Context, r *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	return m.CompleteStream(ctx, r)
}
func (m *chunkedModel) ChatStream(ctx context.Context, ms []llm.Message) (<-chan *llm.StreamChunk, error) {
	return m.CompleteStream(ctx, &llm.CompletionRequest{Messages: ms})
}

func fakeClient(respond string, delay time.Duration) *livemodel.Client {
	var calls atomic.Int64
	return livemodel.Wrap(&demomodel.Model{Calls: &calls, Respond: func(*llm.CompletionRequest) llm.StreamChunk {
		time.Sleep(delay)
		return llm.StreamChunk{Content: respond, Delta: respond}
	}}, livemodel.Config{Provider: "fake", Model: "fake"})
}

func TestSummarizeOffline(t *testing.T) {
	t.Run("多段流式：预览拼接等于终态", func(t *testing.T) {
		var calls atomic.Int64
		parts := []string{"结论：缓存", "改直连。\r\n影响：", "9 分钟。\n改进：", "压测门禁。  "}
		client := livemodel.Wrap(&chunkedModel{Model: demomodel.Model{Calls: &calls}, parts: parts}, livemodel.Config{Provider: "fake"})
		var seen []string
		out, err := Summarize(context.Background(), client, func(d string) { seen = append(seen, d) })
		require.NoError(t, err)
		require.Equal(t, out.Previews, seen)
		require.Len(t, out.Previews, len(parts))
		require.Equal(t, strings.TrimSpace(strings.Join(out.Previews, "")), out.Final)
		require.NotContains(t, out.Final, "\r", "Map 去掉回车")
		require.Equal(t, int64(1), out.NewModelCalls)
		require.Equal(t, 1, out.Budget.ReservedRequests)
		require.LessOrEqual(t, out.FirstPreview, out.Elapsed)
	})
	t.Run("一次性输出：只有一段预览", func(t *testing.T) {
		out, err := Summarize(context.Background(), fakeClient("结论：根因\n影响：小\n改进：门禁", 0), nil)
		require.NoError(t, err)
		require.Len(t, out.Previews, 1)
		require.Equal(t, "结论：根因\n影响：小\n改进：门禁", out.Final)
	})
	t.Run("空输出不提交终态", func(t *testing.T) {
		out, err := Summarize(context.Background(), fakeClient("   ", 0), nil)
		require.Error(t, err)
		require.Empty(t, out.Final)
	})
	t.Run("调用方取消", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		out, err := Summarize(ctx, fakeClient("结论：x", 0), nil)
		require.Error(t, err)
		require.Empty(t, out.Final)
		require.Zero(t, out.NewModelCalls, "已取消时不发请求")
	})
}

func TestContendOffline(t *testing.T) {
	for _, tc := range []struct{ children, limit int }{
		{3, 2}, {5, 1}, {4, 4}, {2, 5}, {10, 3},
	} {
		t.Run(fmt.Sprintf("%d个子Agent共享%d次", tc.children, tc.limit), func(t *testing.T) {
			client := fakeClient("回答", 20*time.Millisecond)
			out, err := Contend(context.Background(), client, tc.children, tc.limit)
			require.NoError(t, err)
			want := min(tc.children, tc.limit)
			require.Equal(t, want, out.Succeeded)
			require.Equal(t, tc.children-want, out.Rejected)
			require.Len(t, out.Errors, tc.children-want)
			for _, e := range out.Errors {
				require.ErrorIs(t, e, budget.ErrBudgetExceeded, "失败只能来自预算拒绝")
			}
			require.Equal(t, int64(want), out.NewModelCalls, "被拒绝的请求不得发出")
			require.Equal(t, want, out.Budget.ReservedRequests)
		})
	}
}

func TestLiveSummarize(t *testing.T) {
	client := livemodeltest.Require(t)
	out, err := Summarize(context.Background(), client, nil)
	t.Logf("%s/%s：预览 %d 段，首段 %v，总 %v\n%s", client.Live.Provider, client.Live.Model, len(out.Previews), out.FirstPreview, out.Elapsed, out.Final)
	require.NoError(t, err)
	lines := strings.Split(out.Final, "\n")
	var kept []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			kept = append(kept, strings.TrimSpace(l))
		}
	}
	require.Len(t, kept, 3, out.Final)
	for i, prefix := range []string{"结论", "影响", "改进"} {
		require.True(t, strings.HasPrefix(strings.Trim(kept[i], "*"), prefix), kept[i])
	}
	require.Contains(t, out.Final, "9")
	require.Contains(t, out.Final, "3200")
	require.Equal(t, strings.TrimSpace(strings.Join(out.Previews, "")), out.Final, "终态只是预览的一次性提交")
	require.Equal(t, int64(1), out.NewModelCalls)
	require.Equal(t, 1, out.Budget.ReservedRequests)
	require.Positive(t, out.Budget.KnownTokens.TotalTokens)
	if client.StreamMode() == core.StreamModeTokens {
		require.Greater(t, len(out.Previews), 1, "逐 token 提供方应分多段到达")
		require.Less(t, out.FirstPreview, out.Elapsed, "首段预览早于完成")
	}
}

func TestLiveContend(t *testing.T) {
	client := livemodeltest.Require(t)
	out, err := Contend(context.Background(), client, 3, 2)
	t.Logf("%s/%s：%+v", client.Live.Provider, client.Live.Model, out)
	require.NoError(t, err)
	require.Equal(t, 2, out.Succeeded)
	require.Equal(t, 1, out.Rejected)
	for _, e := range out.Errors {
		require.True(t, errors.Is(e, budget.ErrBudgetExceeded), e)
	}
	require.Equal(t, int64(2), out.NewModelCalls)
	require.Equal(t, 2, out.Budget.ReservedRequests)
	require.Positive(t, out.Budget.KnownTokens.TotalTokens)
	for _, a := range out.Answers {
		require.NotEmpty(t, strings.TrimSpace(a))
	}
}
