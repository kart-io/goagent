package builder

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
	"github.com/stretchr/testify/require"
)

type nativeEventModel struct {
	*eventModel
	turns    [][]*llm.StreamChunk
	requests []*llm.CompletionRequest
	toolSets [][]interfaces.Tool
}

func (m *nativeEventModel) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, tools []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	copy := *req
	copy.Messages = append([]llm.Message(nil), req.Messages...)
	m.requests = append(m.requests, &copy)
	m.toolSets = append(m.toolSets, append([]interfaces.Tool(nil), tools...))
	index := len(m.requests) - 1
	if index >= len(m.turns) {
		return nil, errors.New("发生了多余模型请求")
	}
	stream := make(chan *llm.StreamChunk)
	go func() {
		defer close(stream)
		for _, chunk := range m.turns[index] {
			select {
			case stream <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return stream, nil
}
func nativeCall(id, name, args string) llm.ToolCall {
	call := llm.ToolCall{ID: id, Type: "function"}
	call.Function.Name = name
	call.Function.Arguments = args
	return call
}
func nativeModel(turns ...[]*llm.StreamChunk) *nativeEventModel {
	return &nativeEventModel{eventModel: &eventModel{MockLLMClient: NewMockLLMClient()}, turns: turns}
}

type countingEventTool struct {
	*MockTool
	calls int
}

func (t *countingEventTool) Invoke(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	t.calls++
	return t.MockTool.Invoke(ctx, in)
}

func TestNativeToolLoopUsesTwoRequests(t *testing.T) {
	model := nativeModel([]*llm.StreamChunk{{Delta: "先查询资料"}, {Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("id-1", "lookup", "{}")}, Usage: &llm.Usage{TotalTokens: 2}}}, []*llm.StreamChunk{{Delta: "30"}, {Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: 3}}})
	lookup := &countingEventTool{MockTool: NewMockTool("lookup", "三十天")}
	r := documentRetrieverFunc(func(context.Context, string) ([]*interfaces.Document, error) {
		return []*interfaces.Document{{ID: "policy", PageContent: "参考原文"}}, nil
	})
	agent, err := NewSimpleBuilder(model).WithTools(lookup).WithSystemPrompt("唯一指令").WithRetriever(r).WithMaxIterations(2).Build()
	require.NoError(t, err)
	output, err := agent.AsRunnable().Invoke(context.Background(), "期限？")
	require.NoError(t, err)
	require.Equal(t, "30", output.Result)
	require.Equal(t, 1, lookup.calls)
	require.Len(t, model.requests, 2)
	require.Len(t, model.toolSets[0], 1)
	require.Empty(t, model.toolSets[1])
	require.Equal(t, true, output.Metadata["final_answer_only"])
	require.Zero(t, model.selections)
	require.Nil(t, model.request, "不得调用单独的答案接口")
	require.Equal(t, 5, output.TokenUsage.TotalTokens)
	require.Equal(t, 2, output.Metadata["model_calls"])
	require.Equal(t, true, output.Metadata["usage_complete"])
	require.Equal(t, 1, output.Metadata["retrieval_document_count"])
	first := model.requests[0]
	second := model.requests[1]
	require.Len(t, first.Messages, 2)
	require.Equal(t, "system", first.Messages[0].Role)
	require.Equal(t, 1, strings.Count(first.Messages[0].Content, "唯一指令"))
	require.NotContains(t, first.Messages[1].Content, "唯一指令")
	require.Equal(t, 1, strings.Count(first.Messages[0].Content, retrievalInstruction))
	require.NotContains(t, first.Messages[1].Content, retrievalInstruction)
	require.Contains(t, first.Messages[1].Content, "参考原文")
	require.Len(t, second.Messages, 4)
	require.Equal(t, "assistant", second.Messages[2].Role)
	require.Equal(t, "先查询资料", second.Messages[2].Content)
	require.Equal(t, "id-1", second.Messages[2].ToolCalls[0].ID)
	require.Equal(t, "tool", second.Messages[3].Role)
	require.Equal(t, "id-1", second.Messages[3].ToolCallID)
	require.Equal(t, "三十天", second.Messages[3].Content)
}

func TestNativeToolLoopAnswersWithoutToolInOneRequest(t *testing.T) {
	model := nativeModel([]*llm.StreamChunk{{Delta: "直接答案"}, {Done: true, FinishReason: "stop"}})
	tool := &countingEventTool{MockTool: NewMockTool("lookup", "资料")}
	agent, err := NewSimpleBuilder(model).WithTools(tool).WithMaxIterations(1).Build()
	require.NoError(t, err)
	output, err := agent.AsRunnable().Invoke(context.Background(), "不需要查询")
	require.NoError(t, err)
	require.Equal(t, "直接答案", output.Result)
	require.Len(t, model.requests, 1)
	require.Zero(t, tool.calls)
	require.Equal(t, false, output.Metadata["usage_complete"])
	require.Equal(t, false, output.Metadata["answer_usage_reported"])
}

func TestNativeToolLoopRejectsIncompleteCalls(t *testing.T) {
	for _, scenario := range []string{"预算", "截断", "参数", "重复ID", "空工具", "上游错误", "长度截断"} {
		t.Run(scenario, func(t *testing.T) {
			final := &llm.StreamChunk{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("call", "lookup", "{}")}}
			chunks := []*llm.StreamChunk{final}
			maxCalls := 2
			switch scenario {
			case "预算":
				maxCalls = 1
			case "截断":
				chunks = []*llm.StreamChunk{{Delta: "未完成"}}
			case "参数":
				final.ToolCalls[0].Function.Arguments = `{"q":`
			case "重复ID":
				final.ToolCalls = append(final.ToolCalls, final.ToolCalls[0])
			case "空工具":
				final.ToolCalls = nil
			case "上游错误":
				final.Error = io.ErrUnexpectedEOF
			case "长度截断":
				final.FinishReason = "length"
			}
			model := nativeModel(chunks)
			tool := &countingEventTool{MockTool: NewMockTool("lookup", "资料")}
			agent, err := NewSimpleBuilder(model).WithTools(tool).WithMaxIterations(maxCalls).Build()
			require.NoError(t, err)
			_, err = agent.AsRunnable().Invoke(context.Background(), "执行")
			require.Error(t, err)
			require.Zero(t, tool.calls)
			require.Len(t, model.requests, 1)
		})
	}
}

func TestPlainStreamDoesNotDuplicateSystemPrompt(t *testing.T) {
	model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "答案"}, {Done: true}}}
	agent, err := NewSimpleBuilder(model).WithSystemPrompt("唯一指令").Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "问题")
	require.NoError(t, err)
	require.Equal(t, []llm.Message{{Role: "system", Content: "唯一指令"}, {Role: "user", Content: "问题"}}, model.request.Messages)
}

func TestNativeLoopReservesOnlyLastRoundForAnswer(t *testing.T) {
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{nativeCall("a", "lookup", "{}")}}},
		[]*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{nativeCall("b", "lookup", "{}")}}},
		[]*llm.StreamChunk{{Delta: "完整答案"}, {Done: true}},
	)
	tool := &countingEventTool{MockTool: NewMockTool("lookup", "资料")}
	agent, err := NewSimpleBuilder(model).WithTools(tool).WithMaxIterations(3).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "需要两步查询")
	require.NoError(t, err)
	require.Equal(t, "完整答案", out.Result)
	require.Equal(t, 2, tool.calls)
	require.Len(t, model.requests, 3)
	require.Len(t, model.toolSets[0], 1)
	require.Len(t, model.toolSets[1], 1)
	require.Empty(t, model.toolSets[2])
	require.Equal(t, true, out.Metadata["final_answer_only"])
}

func TestNativeFinalRoundRejectsUnexpectedTools(t *testing.T) {
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{nativeCall("a", "lookup", "{}")}}},
		[]*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{nativeCall("b", "final", "{}")}}},
	)
	lookup := &countingEventTool{MockTool: NewMockTool("lookup", "资料")}
	final := &countingEventTool{MockTool: NewMockTool("final", "结果")}
	agent, err := NewSimpleBuilder(model).WithTools(lookup, final).WithMaxIterations(2).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "查询")
	require.ErrorContains(t, err, "未提供工具")
	require.Nil(t, out)
	require.Equal(t, 1, lookup.calls)
	require.Zero(t, final.calls)
	require.Len(t, model.requests, 2)
}

func TestNativeLastRoundAllowsConfiguredTerminalTool(t *testing.T) {
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{nativeCall("a", "lookup", "{}")}}},
		[]*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{nativeCall("b", "final", "{}")}}},
	)
	lookup := &countingEventTool{MockTool: NewMockTool("lookup", "资料")}
	final := &countingEventTool{MockTool: NewMockTool("final", "已完成")}
	agent, err := NewSimpleBuilder(model).WithTools(lookup, final).WithReturnDirectTools("final").WithMaxIterations(2).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "先查询再输出终态结果")
	require.NoError(t, err)
	require.Equal(t, "已完成", out.Result)
	require.Equal(t, 1, lookup.calls)
	require.Equal(t, 1, final.calls)
	require.Len(t, model.toolSets[0], 2)
	require.Len(t, model.toolSets[1], 1)
	require.Equal(t, "final", model.toolSets[1][0].Name())
	require.Equal(t, "tool", out.Metadata["answer_source"])
}

func TestCompactStreamPreservesHistoryAndOriginalQuestion(t *testing.T) {
	ctx := context.Background()
	mem := memory.NewInMemoryManager(nil)
	require.NoError(t, mem.AddConversation(ctx, &interfaces.Conversation{SessionID: "history", Role: "user", Content: "旧问题"}))
	require.NoError(t, mem.AddConversation(ctx, &interfaces.Conversation{SessionID: "history", Role: "assistant", Content: "旧答案"}))
	model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "新答案"}, {Done: true}}}
	r := documentRetrieverFunc(func(context.Context, string) ([]*interfaces.Document, error) {
		return []*interfaces.Document{{ID: "ref", PageContent: "检索证据"}}, nil
	})
	agent, err := NewSimpleBuilder(model).WithSystemPrompt("规则").WithMemory(mem).WithSessionID("history").WithRetriever(r).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(ctx, "新问题")
	require.NoError(t, err)
	messages := model.request.Messages
	require.Len(t, messages, 4)
	require.Equal(t, llm.Message{Role: "user", Content: "旧问题"}, messages[1])
	require.Equal(t, llm.Message{Role: "assistant", Content: "旧答案"}, messages[2])
	require.Contains(t, messages[3].Content, "检索证据")
	require.NotContains(t, messages[3].Content, "旧问题")
	history, err := mem.GetConversationHistory(ctx, "history", 10)
	require.NoError(t, err)
	require.Len(t, history, 4)
	require.Equal(t, "新问题", history[2].Content)
	require.Equal(t, "新答案", history[3].Content)
}
