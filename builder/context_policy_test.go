package builder

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
	"github.com/stretchr/testify/require"
)

// These tests catch lost evidence, broken tool-call linkage, unscoped reads,
// hidden summary requests, and accidental changes to the default execution path.
func TestContextPolicyOffloadPreservesProtocolAndPages(t *testing.T) {
	p := newToolContext(&ContextPolicy{MaxToolResultRunes: 3, KeepRecentToolRounds: 1, ReadPageRunes: 2})
	messages := []llm.Message{
		llm.SystemMessage("instructions"), llm.UserMessage("question"),
		{Role: "assistant", ReasoningContent: "reason", ToolCalls: []llm.ToolCall{nativeCall("a", "lookup", "{}")}},
		{Role: "tool", ToolCallID: "a", Content: "甲乙丙丁戊"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{nativeCall("b", "lookup", "{}")}},
		{Role: "tool", ToolCallID: "b", Content: "recent evidence"},
	}
	reduced, err := p.reduce(context.Background(), messages)
	require.NoError(t, err)
	require.Equal(t, messages[2], reduced[2])
	require.Equal(t, "a", reduced[3].ToolCallID)
	require.Equal(t, messages[5], reduced[5])
	require.Equal(t, "甲乙丙丁戊", messages[3].Content, "do not mutate previous requests")
	var reference struct {
		Key string `json:"context_result"`
	}
	require.NoError(t, json.Unmarshal([]byte(reduced[3].Content), &reference))
	require.NotEmpty(t, reference.Key)
	out, err := p.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]interface{}{"key": reference.Key, "offset": float64(2)}})
	require.NoError(t, err)
	var page struct {
		Text string `json:"text"`
		Next int    `json:"next_offset"`
		Done bool   `json:"done"`
	}
	require.NoError(t, json.Unmarshal([]byte(out.Result.(string)), &page))
	require.Equal(t, "丙丁", page.Text)
	require.Equal(t, 4, page.Next)
	require.False(t, page.Done)
	again, err := p.reduce(context.Background(), reduced)
	require.NoError(t, err)
	require.Equal(t, reduced, again)
	other := newToolContext(&ContextPolicy{MaxToolResultRunes: 3})
	foreign, err := other.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]interface{}{"key": reference.Key}})
	require.NoError(t, err)
	require.Contains(t, foreign.Result, "unknown context_result key", "references belong to one execution")
	require.NotContains(t, foreign.Result, "甲")
	// 非法 offset 与未知键一致：返回合法范围交还模型改正，不中止执行。
	for _, offset := range []interface{}{-1.0, 1.5, "2", 999.0} {
		bad, err := p.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]interface{}{"key": reference.Key, "offset": offset}})
		require.NoError(t, err)
		require.True(t, bad.Success)
		require.JSONEq(t, `{"error":"invalid offset","min_offset":0,"max_offset":5}`, bad.Result.(string))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Invoke(ctx, &interfaces.ToolInput{Args: map[string]interface{}{"key": reference.Key}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestContextPolicyRetainsWholeMultiToolRoundAndExclusions(t *testing.T) {
	p := newToolContext(&ContextPolicy{MaxToolResultRunes: 1, KeepRecentToolRounds: 1, ExcludeTools: []string{"secret"}})
	messages := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{nativeCall("a", "secret", "{}"), nativeCall("b", "lookup", "{}")}},
		{Role: "tool", ToolCallID: "a", Content: "excluded"}, {Role: "tool", ToolCallID: "b", Content: "old result"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{nativeCall("c", "lookup", "{}"), nativeCall("d", "lookup", "{}")}},
		{Role: "tool", ToolCallID: "c", Content: "latest one"}, {Role: "tool", ToolCallID: "d", Content: "latest two"},
	}
	reduced, err := p.reduce(context.Background(), messages)
	require.NoError(t, err)
	require.Equal(t, messages[1], reduced[1])
	require.NotEqual(t, messages[2].Content, reduced[2].Content)
	require.Equal(t, messages[3:], reduced[3:])
}

func TestContextPolicyNativeLoopAndDefault(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		model := nativeModel(
			[]*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{nativeCall("a", "lookup", "{}")}}},
			[]*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{nativeCall("b", "lookup", "{}")}}},
			[]*llm.StreamChunk{{Delta: "answer"}, {Done: true}},
		)
		b := NewSimpleBuilder(model).WithSystemPrompt("instructions").WithTools(NewMockTool("lookup", strings.Repeat("证据", 100))).WithMaxIterations(4)
		if enabled {
			b.WithContextPolicy(ContextPolicy{MaxToolResultRunes: 10, KeepRecentToolRounds: 1})
		}
		agent, err := b.Build()
		require.NoError(t, err)
		out, err := agent.AsRunnable().Invoke(context.Background(), "question")
		require.NoError(t, err)
		require.Equal(t, "answer", out.Result)
		require.Len(t, model.requests, 3)
		if enabled {
			require.Contains(t, model.requests[2].Messages[2].ToolCalls[0].ID, "a")
			require.Contains(t, model.requests[2].Messages[3].Content, "context_result")
			require.Equal(t, 1, out.Metadata["context_offloaded_results"])
			require.Len(t, model.toolSets[0], 2)
		} else {
			require.Equal(t, strings.Repeat("证据", 100), model.requests[2].Messages[3].Content)
		}
	}
}

func TestContextPolicySummaryBudgetAndUsage(t *testing.T) {
	summary := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "old fact"}, {Done: true, Usage: &llm.Usage{TotalTokens: 7}}}}
	p := newToolContext(&ContextPolicy{HistorySummary: &HistorySummaryConfig{Client: summary, TriggerRunes: 10, KeepRecentMessages: 2, MaxTokens: 20}})
	messages := []llm.Message{llm.SystemMessage("system"), llm.UserMessage(strings.Repeat("old", 20)), {Role: "assistant", Content: "old answer"}, llm.UserMessage("recent"), {Role: "assistant", Content: "recent answer"}, llm.UserMessage("current")}
	_, _, err := p.prepare(context.Background(), messages, 1)
	require.Error(t, err)
	require.Nil(t, summary.request, "do not spend the only request on a summary")
	reduced, turn, err := p.prepare(context.Background(), messages, 2)
	require.NoError(t, err)
	require.Equal(t, 7, turn.Usage.TotalTokens)
	require.Equal(t, messages[0], reduced[0])
	require.Equal(t, "user", reduced[1].Role, "summary cannot become system instructions")
	require.Contains(t, reduced[1].Content, "old fact")
	require.Equal(t, messages[3:], reduced[2:])
	require.NotContains(t, summary.request.Messages[1].Content, "current")

	model := nativeModel([]*llm.StreamChunk{{Delta: "answer"}, {Done: true, Usage: &llm.Usage{TotalTokens: 3}}})
	agent, err := NewSimpleBuilder(model).WithTools(NewMockTool("lookup", "data")).WithMaxIterations(2).WithContextPolicy(*p.policy).Build()
	require.NoError(t, err)
	response, err := agent.runToolEvents(context.Background(), &middleware.MiddlewareRequest{Input: "current"}, model, messages, map[string]interface{}{}, func(AgentEvent) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 10, response.TokenUsage.TotalTokens)
	require.Equal(t, 2, response.Metadata["model_calls"])
	require.Equal(t, 1, response.Metadata["summary_calls"])
	require.Equal(t, true, response.Metadata["usage_complete"])
	require.Empty(t, model.toolSets[0], "last request must be reserved for final answer")
}

func TestContextPolicyRejectsUnsupportedConfiguration(t *testing.T) {
	for _, policy := range []ContextPolicy{{MaxToolResultRunes: -1}, {ReadPageRunes: -1}, {HistorySummary: &HistorySummaryConfig{TriggerRunes: 1}}, {MaxToolResultRunes: 10}} {
		_, err := NewSimpleBuilder(NewMockLLMClient()).WithTools(NewMockTool("lookup", "x")).WithContextPolicy(policy).Build()
		require.Error(t, err)
	}
	_, err := NewSimpleBuilder(nativeModel()).WithTools(NewMockTool("read_context_result", "x")).WithContextPolicy(ContextPolicy{MaxToolResultRunes: 10}).Build()
	require.Error(t, err)
}

func TestContextPolicySummaryErrors(t *testing.T) {
	for _, chunks := range [][]*llm.StreamChunk{
		{{Done: true}}, {{Done: true, FinishReason: "length"}}, {{Error: errors.New("upstream")}}, {{Delta: "partial"}},
	} {
		summary := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: chunks}
		p := newToolContext(&ContextPolicy{HistorySummary: &HistorySummaryConfig{Client: summary, TriggerRunes: 1, KeepRecentMessages: 1, MaxTokens: 20}})
		_, _, err := p.prepare(context.Background(), []llm.Message{llm.UserMessage("old history"), llm.UserMessage("recent"), llm.UserMessage("current")}, 2)
		require.Error(t, err)
	}
}

type contextReadingModel struct{ *nativeEventModel }

func (m *contextReadingModel) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, tools []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	if len(m.requests) == 2 {
		var ref map[string]any
		if err := json.Unmarshal([]byte(req.Messages[3].Content), &ref); err != nil {
			return nil, err
		}
		args, _ := json.Marshal(map[string]any{"key": ref["context_result"], "offset": 2})
		m.turns[2] = []*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{nativeCall("read", contextReadTool, string(args))}}}
	}
	return m.nativeEventModel.CompleteToolsStream(ctx, req, tools)
}

func TestContextPolicyReadsOffloadedEvidenceThroughNativeLoop(t *testing.T) {
	model := &contextReadingModel{nativeModel(
		[]*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{nativeCall("a", "lookup", "{}")}}},
		[]*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{nativeCall("b", "lookup", "{}")}}}, nil,
		[]*llm.StreamChunk{{Delta: "丙丁"}, {Done: true}},
	)}
	agent, err := NewSimpleBuilder(model).WithSystemPrompt("instructions").WithTools(NewMockTool("lookup", "甲乙丙丁戊")).WithMaxIterations(4).WithContextPolicy(ContextPolicy{MaxToolResultRunes: 3, KeepRecentToolRounds: 1, ReadPageRunes: 2}).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "read evidence")
	require.NoError(t, err)
	require.Equal(t, "丙丁", out.Result)
	require.Len(t, model.requests, 4)
	last := model.requests[3].Messages
	require.Equal(t, "read", last[len(last)-1].ToolCallID)
	require.Contains(t, last[len(last)-1].Content, `"text":"丙丁"`)
	require.Empty(t, model.toolSets[3])
	// The final-answer round cannot call the reader. Restore referenced evidence
	// so that the answer never depends on now-unreachable context keys.
	require.Equal(t, "甲乙丙丁戊", last[3].Content)
}

func TestContextPolicySummaryPreservesMemoryAndUnknownUsage(t *testing.T) {
	ctx := context.Background()
	mem := memory.NewInMemoryManager(nil)
	old := strings.Repeat("旧证据", 100)
	for i, text := range []string{old, "old answer", "recent question", "recent answer"} {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		require.NoError(t, mem.AddConversation(ctx, &interfaces.Conversation{SessionID: "summary", Role: role, Content: text}))
	}
	summary := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "known fact"}, {Done: true}}}
	model := nativeModel([]*llm.StreamChunk{{Delta: "new answer"}, {Done: true, Usage: &llm.Usage{TotalTokens: 3}}})
	agent, err := NewSimpleBuilder(model).WithMemory(mem).WithSessionID("summary").WithTools(NewMockTool("lookup", "x")).WithMaxIterations(2).WithContextPolicy(ContextPolicy{HistorySummary: &HistorySummaryConfig{Client: summary, TriggerRunes: 100, KeepRecentMessages: 2, MaxTokens: 50}}).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(ctx, "current question")
	require.NoError(t, err)
	require.Equal(t, "new answer", out.Result)
	require.Equal(t, false, out.Metadata["usage_complete"])
	require.Equal(t, 2, out.Metadata["model_calls"])
	require.Equal(t, 3, out.TokenUsage.TotalTokens)
	history, err := mem.GetConversationHistory(ctx, "summary", 10)
	require.NoError(t, err)
	require.Len(t, history, 6)
	require.Equal(t, old, history[0].Content)
	require.Equal(t, "current question", history[4].Content)
	require.Equal(t, "current question", model.requests[0].Messages[len(model.requests[0].Messages)-1].Content)
}

func TestContextPolicySummaryCannotExpandHistory(t *testing.T) {
	summary := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "brief"}, {Done: true}}}
	p := newToolContext(&ContextPolicy{HistorySummary: &HistorySummaryConfig{Client: summary, TriggerRunes: 1, KeepRecentMessages: 1, MaxTokens: 20}})
	_, _, err := p.prepare(context.Background(), []llm.Message{llm.UserMessage("old text"), llm.UserMessage("recent"), llm.UserMessage("current")}, 2)
	require.Error(t, err, "the summary envelope also counts toward input size")
}

type sensitiveSummaryModel struct {
	llm.StreamClient
	APIKey string
}

func TestContextPolicySerializationDoesNotExposeSummaryClient(t *testing.T) {
	policy := ContextPolicy{HistorySummary: &HistorySummaryConfig{Client: &sensitiveSummaryModel{APIKey: "private-summary-key"}, TriggerRunes: 100, KeepRecentMessages: 2, MaxTokens: 50}}
	data, err := json.Marshal(policy)
	require.NoError(t, err)
	require.NotContains(t, string(data), "private-summary-key")
	require.Contains(t, string(data), "TriggerRunes")
}
