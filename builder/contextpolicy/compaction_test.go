package contextpolicy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

// summaryModel 记录收到的摘要请求，按 reply 返回摘要或错误。
type summaryModel struct {
	reply    string
	err      error
	requests []*llm.CompletionRequest
}

func (m *summaryModel) Provider() constants.Provider { return constants.Provider("fake") }
func (m *summaryModel) CompleteStream(_ context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	m.requests = append(m.requests, req)
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan *llm.StreamChunk, 2)
	ch <- &llm.StreamChunk{Delta: m.reply, Content: m.reply}
	ch <- &llm.StreamChunk{Done: true, Content: m.reply, FinishReason: "stop"}
	close(ch)
	return ch, nil
}

// 每条消息按内容长度计 token，便于精确控制阈值。
func lengthEstimate(m llm.Message) int { return len([]rune(m.Content)) + len(m.ToolCalls)*10 }

func longConversation() []llm.Message {
	return []llm.Message{
		llm.SystemMessage("系统"),
		llm.UserMessage("任务原文：整理订单"),
		{Role: "assistant", Content: strings.Repeat("甲", 40)},
		toolRound("c1", "lookup"),
		{Role: "tool", ToolCallID: "c1", Content: strings.Repeat("乙", 40)},
		{Role: "assistant", Content: strings.Repeat("丙", 40)},
		llm.UserMessage("最近的追问"),
	}
}

func TestCompactionReduce(t *testing.T) {
	cases := []struct {
		name     string
		model    *summaryModel
		max      int
		annotate func([]llm.Message) string
		check    func(t *testing.T, in, out []llm.Message, model *summaryModel)
	}{
		{
			name: "未超过阈值不改写", model: &summaryModel{reply: "摘要"}, max: 1000,
			check: func(t *testing.T, in, out []llm.Message, model *summaryModel) {
				require.Equal(t, in, out)
				require.Empty(t, model.requests)
			},
		},
		{
			name: "超过阈值换成摘要并保留首条用户原话与本步输入", model: &summaryModel{reply: "已查询订单"}, max: 100,
			check: func(t *testing.T, in, out []llm.Message, model *summaryModel) {
				require.Equal(t, "system", out[0].Role)
				require.True(t, IsSummary(out[1]))
				require.Equal(t, "已查询订单", SummaryBody(out[1].Content))
				require.Equal(t, "任务原文：整理订单", out[2].Content)
				require.Equal(t, "最近的追问", out[len(out)-1].Content)
				require.Len(t, model.requests, 1)
				for i, m := range out {
					if m.Role == "tool" {
						require.NotEmpty(t, out[i-1].ToolCalls, "工具结果前必须是它的调用")
					}
				}
			},
		},
		{
			name: "摘要失败退化为省略说明", model: &summaryModel{err: errors.New("模型不可用")}, max: 100,
			check: func(t *testing.T, _, out []llm.Message, _ *summaryModel) {
				body := SummaryBody(out[1].Content)
				require.Contains(t, body, "模型不可用")
				require.Contains(t, body, "lookup 1 次")
				require.Contains(t, body, constants.ToolRecallHistory)
			},
		},
		{
			name: "附注附在摘要后且不进入正文", model: &summaryModel{reply: "摘要"}, max: 100,
			annotate: func([]llm.Message) string { return "待办：发货" },
			check: func(t *testing.T, _, out []llm.Message, _ *summaryModel) {
				require.Contains(t, out[1].Content, "待办：发货")
				require.Equal(t, "摘要", SummaryBody(out[1].Content))
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewCompaction(CompactionConfig{MaxTokens: tc.max, KeepTokens: 20, Model: tc.model, Estimate: lengthEstimate, Annotate: tc.annotate})
			require.NoError(t, err)
			in := longConversation()
			out, err := c.Reduce(context.Background(), in, 1)
			require.NoError(t, err)
			tc.check(t, in, out, tc.model)
		})
	}
}

// 没有摘要模型时只省略并说明；再次压缩时旧摘要原样交给摘要模型（递归摘要），不被截断。
func TestCompactionWithoutModelAndRecursiveSummary(t *testing.T) {
	plain, err := NewCompaction(CompactionConfig{MaxTokens: 100, KeepTokens: 20, Estimate: lengthEstimate})
	require.NoError(t, err)
	out, err := plain.Reduce(context.Background(), longConversation(), 1)
	require.NoError(t, err)
	require.Contains(t, SummaryBody(out[1].Content), "较早的")

	old := strings.Repeat("旧", maxRenderedRunes+10)
	model := &summaryModel{reply: "新摘要"}
	c, err := NewCompaction(CompactionConfig{MaxTokens: 100, KeepTokens: 20, Model: model, Estimate: lengthEstimate})
	require.NoError(t, err)
	in := append([]llm.Message{llm.SystemMessage("系统"), llm.UserMessage(SummaryPrefix + old + annotationMark + "过时的待办")}, longConversation()[2:]...)
	out, err = c.Reduce(context.Background(), in, 1)
	require.NoError(t, err)
	require.Equal(t, "新摘要", SummaryBody(out[1].Content))
	transcript := model.requests[0].Messages[1].Content
	require.Contains(t, transcript, old, "旧摘要不能被截断")
	require.NotContains(t, transcript, "过时的待办", "附注不交给摘要模型")
}

// 已超出上下文时只保留最近一次交互；没有可压缩的部分时报告未改变。
func TestCompactionRecover(t *testing.T) {
	c, err := NewCompaction(CompactionConfig{MaxTokens: 1000, KeepTokens: 500, Model: &summaryModel{reply: "摘要"}, Estimate: lengthEstimate})
	require.NoError(t, err)
	out, changed, err := c.Recover(context.Background(), longConversation())
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "最近的追问", out[len(out)-1].Content)
	require.Less(t, len(out), len(longConversation()))

	_, changed, err = c.Recover(context.Background(), []llm.Message{llm.SystemMessage("系统"), llm.UserMessage("只有一条")})
	require.NoError(t, err)
	require.False(t, changed)
}

// 取消时不退化为省略说明，而是返回错误，避免把取消误当作摘要失败写入历史。
func TestCompactionPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, err := NewCompaction(CompactionConfig{MaxTokens: 100, KeepTokens: 20, Model: &summaryModel{err: context.Canceled}, Estimate: lengthEstimate})
	require.NoError(t, err)
	_, err = c.Reduce(ctx, longConversation(), 1)
	require.ErrorIs(t, err, context.Canceled)
}

func TestNewCompactionValidatesAndIdentifies(t *testing.T) {
	for _, cfg := range []CompactionConfig{{MaxTokens: -1}, {MaxTokens: 100, KeepTokens: 100}, {MaxTokens: 100, KeepTokens: -1}} {
		_, err := NewCompaction(cfg)
		require.Error(t, err)
	}
	a, err := NewCompaction(CompactionConfig{MaxTokens: 100})
	require.NoError(t, err)
	b, err := NewCompaction(CompactionConfig{MaxTokens: 200})
	require.NoError(t, err)
	c, err := NewCompaction(CompactionConfig{MaxTokens: 100, Version: "v2"})
	require.NoError(t, err)
	require.NotEqual(t, a.Identity(), b.Identity())
	require.NotEqual(t, a.Identity(), c.Identity())
}
