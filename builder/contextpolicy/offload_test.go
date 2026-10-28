package contextpolicy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

// 模型抄错引用键时不能中止执行：返回可读的错误结果和合法键，正确键仍可读取；恢复进程重建出同一别名。
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
	call := llm.ToolCall{ID: "c1", Type: "function"}
	call.Function.Name, call.Function.Arguments = "fetch", "{}"
	turn := Turn{Messages: []llm.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
		{Role: "tool", ToolCallID: "c1", Content: strings.Repeat("x", 50)},
	}}
	p := newPolicy()
	out, err := p.BeforeTurn(ctx, turn)
	require.NoError(t, err)
	var shown struct {
		Key string `json:"context_result"`
	}
	require.NoError(t, json.Unmarshal([]byte(out.Messages[2].Content), &shown))
	require.Equal(t, "r1", shown.Key)

	resumed, err := newPolicy().BeforeTurn(ctx, turn)
	require.NoError(t, err)
	require.Equal(t, out.Messages[2].Content, resumed.Messages[2].Content, "恢复时请求必须逐字节一致")

	read := func(key string) map[string]any {
		got, err := p.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"key": key, "offset": float64(0)}})
		require.NoError(t, err)
		require.True(t, got.Success)
		var body map[string]any
		require.NoError(t, json.Unmarshal([]byte(got.Result.(string)), &body))
		return body
	}
	require.Equal(t, "xxxxx", read("r1")["text"])
	for _, bad := range []string{"r2", "R1", " r1", "", out.Messages[2].Content} {
		body := read(bad)
		require.Equal(t, "unknown context_result key", body["error"], bad)
		require.Equal(t, []any{"r1"}, body["valid_keys"], bad)
		require.NotContains(t, body, "text", bad)
	}
	// 别名只在本实例内解析：另一执行的实例读不到。
	other, err := newPolicy().Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"key": "r1"}})
	require.NoError(t, err)
	require.Contains(t, other.Result, "unknown context_result key")
}
