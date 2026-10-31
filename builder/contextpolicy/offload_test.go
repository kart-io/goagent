package contextpolicy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func toolRound(id, name string) llm.Message {
	call := llm.ToolCall{ID: id, Type: "function"}
	call.Function.Name, call.Function.Arguments = name, "{}"
	return llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}}
}

// 模型抄错引用键时不能中止执行：返回可读的错误结果和合法键，正确键仍可读取；新实例按同一对话得到逐字节相同的引用，
// 并且只有从对话重建引用表（Track）后才能读取。
func TestOffloadAliasStableAndMissRecoverable(t *testing.T) {
	file, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	results, err := NewDurableResultStore(file)
	require.NoError(t, err)
	ctx := WithResultScope(durable.WithJournal(durable.WithExecutionKey(context.Background(), "exec"), file), "exec")
	newPolicy := func() *Offload {
		p, err := NewOffload(OffloadConfig{Store: results, MaxToolResultRunes: 10, ReadPageRunes: 5})
		require.NoError(t, err)
		return p
	}
	messages := []llm.Message{{Role: "user", Content: "q"}, toolRound("c1", "fetch"), {Role: "tool", ToolCallID: "c1", Content: strings.Repeat("x", 50)}}
	p := newPolicy()
	out, err := p.Reduce(ctx, messages, false)
	require.NoError(t, err)
	var shown struct {
		Key string `json:"context_result"`
	}
	require.NoError(t, json.Unmarshal([]byte(out[2].Content), &shown))
	require.Equal(t, "r1", shown.Key)
	require.Equal(t, strings.Repeat("x", 50), messages[2].Content, "不修改调用方的对话")

	resumed := newPolicy()
	again, err := resumed.Reduce(ctx, messages, false)
	require.NoError(t, err)
	require.Equal(t, out, again, "同一对话的结果逐字节一致")

	read := func(p *Offload, key string) map[string]any {
		got, err := p.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"key": key, "offset": float64(0)}})
		require.NoError(t, err)
		require.True(t, got.Success)
		var body map[string]any
		require.NoError(t, json.Unmarshal([]byte(got.Result.(string)), &body))
		return body
	}
	require.Equal(t, "xxxxx", read(p, "r1")["text"])
	for _, bad := range []string{"r2", "R1", " r1", "", out[2].Content} {
		body := read(p, bad)
		require.Equal(t, "unknown context_result key", body["error"], bad)
		require.Equal(t, []any{"r1"}, body["valid_keys"], bad)
		require.NotContains(t, body, "text", bad)
	}
	fresh := newPolicy()
	require.Equal(t, "unknown context_result key", read(fresh, "r1")["error"], "引用表属于实例")
	fresh.Track(ctx, out)
	require.Equal(t, "xxxxx", read(fresh, "r1")["text"], "从对话中的引用重建")
	other := WithResultScope(ctx, "other")
	got, err := fresh.Invoke(other, &interfaces.ToolInput{Args: map[string]any{"key": "r1"}})
	require.NoError(t, err)
	require.Contains(t, got.Result, "unknown context_result key", "引用表按结果作用域区分")
}

// 改写只换掉较早的长结果：保留工具调用配对、最近 KeepRecentToolRounds 轮（整轮）与排除的工具；已换成引用的结果不再处理；
// final 时还原原文。
func TestOffloadReduce(t *testing.T) {
	ctx := WithResultScope(context.Background(), "exec")
	p, err := NewOffload(OffloadConfig{Store: NewMemoryStore(), MaxToolResultRunes: 1, KeepRecentToolRounds: 1, ExcludeTools: []string{"secret"}})
	require.NoError(t, err)
	round := toolRound("a", "secret")
	round.ToolCalls = append(round.ToolCalls, toolRound("b", "lookup").ToolCalls...)
	round.ReasoningContent = "reason"
	latest := toolRound("c", "lookup")
	latest.ToolCalls = append(latest.ToolCalls, toolRound("d", "lookup").ToolCalls...)
	messages := []llm.Message{
		llm.SystemMessage("instructions"), round,
		{Role: "tool", ToolCallID: "a", Content: "excluded"}, {Role: "tool", ToolCallID: "b", Content: "old result"},
		latest, {Role: "tool", ToolCallID: "c", Content: "latest one"}, {Role: "tool", ToolCallID: "d", Content: "latest two"},
	}
	out, err := p.Reduce(ctx, messages, false)
	require.NoError(t, err)
	require.Equal(t, messages[:3], out[:3])
	require.Equal(t, "b", out[3].ToolCallID)
	require.Contains(t, out[3].Content, `"context_result":"r1"`)
	require.Equal(t, messages[4:], out[4:], "最近一轮整轮保留")

	again, err := p.Reduce(ctx, out, false)
	require.NoError(t, err)
	require.Equal(t, out, again)
	require.EqualValues(t, 1, p.Stats()["offloaded_results"])

	restored, err := p.Reduce(ctx, out, true)
	require.NoError(t, err)
	require.Equal(t, messages, restored)

	_, err = p.Reduce(context.Background(), messages, false)
	require.ErrorContains(t, err, "scope")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = p.Invoke(cancelled, &interfaces.ToolInput{Args: map[string]any{"key": "r1"}})
	require.ErrorIs(t, err, context.Canceled)
}

// offset 越界与未知键一致：直接调用和经 Dispatcher（未开启参数错误恢复）调用都返回合法范围，不中止执行。
func TestOffloadInvalidOffsetRecoverable(t *testing.T) {
	ctx := WithResultScope(context.Background(), "exec")
	p, err := NewOffload(OffloadConfig{Store: NewMemoryStore(), MaxToolResultRunes: 10, ReadPageRunes: 5})
	require.NoError(t, err)
	_, err = p.Reduce(ctx, []llm.Message{toolRound("c1", "fetch"), {Role: "tool", ToolCallID: "c1", Content: strings.Repeat("x", 50)}}, false)
	require.NoError(t, err)
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(p))
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
	require.NoError(t, err)
	for name, invoke := range map[string]func(*interfaces.ToolInput) (*interfaces.ToolOutput, error){
		"直接调用":         func(in *interfaces.ToolInput) (*interfaces.ToolOutput, error) { return p.Invoke(ctx, in) },
		"经 Dispatcher": func(in *interfaces.ToolInput) (*interfaces.ToolOutput, error) { return d.Tools()[0].Invoke(ctx, in) },
	} {
		out, err := invoke(&interfaces.ToolInput{Args: map[string]any{"key": "r1", "offset": float64(51)}})
		require.NoError(t, err, name)
		require.True(t, out.Success, name)
		require.JSONEq(t, `{"error":"invalid offset","min_offset":0,"max_offset":50}`, out.Result.(string), name)
		out, err = invoke(&interfaces.ToolInput{Args: map[string]any{"key": "r1", "offset": float64(50)}})
		require.NoError(t, err, name)
		require.Contains(t, out.Result, `"done":true`, name)
	}
}
