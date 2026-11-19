package builder

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

type toolEventSink struct {
	mu     sync.Mutex
	events []observe.Event
}

func (s *toolEventSink) Observe(_ context.Context, e observe.Event) {
	if e.Type == "tool_start" || e.Type == "tool_end" || e.Type == "tool_error" {
		s.mu.Lock()
		s.events = append(s.events, e)
		s.mu.Unlock()
	}
}

func lookupBody(fn func() (*interfaces.ToolOutput, error)) func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) { return fn() }
}

func dispatchedLookup(t *testing.T) []interfaces.Tool {
	t.Helper()
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tools.NewBaseTool("lookup", "查询", `{"type":"object"}`, lookupBody(func() (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Result: "ok", Success: true}, nil
	}))))
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
	require.NoError(t, err)
	return d.Tools()
}

// WithTools 的工具与 Dispatcher 派发的工具一样发出 tool 生命周期事件，且每次调用只记一次。
func TestToolCallsEmitLifecycleEventsOnce(t *testing.T) {
	plain := func(fn func() (*interfaces.ToolOutput, error)) func(*testing.T) []interfaces.Tool {
		return func(*testing.T) []interfaces.Tool {
			return []interfaces.Tool{tools.NewBaseTool("lookup", "查询", `{"type":"object"}`, lookupBody(fn))}
		}
	}
	for _, tc := range []struct {
		name  string
		tools func(*testing.T) []interfaces.Tool
		want  []string
		kind  string
	}{
		{"普通工具成功", plain(func() (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "ok", Success: true}, nil
		}), []string{"tool_start", "tool_end"}, ""},
		{"普通工具失败", plain(func() (*interfaces.ToolOutput, error) {
			return nil, errors.New("下游不可用")
		}), []string{"tool_start", "tool_error"}, "execution"},
		{"普通工具 panic", plain(func() (*interfaces.ToolOutput, error) {
			panic("工具崩溃")
		}), []string{"tool_start", "tool_error"}, "panic"},
		{"Dispatcher 工具经 WithTools 传入", dispatchedLookup, []string{"tool_start", "tool_end"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := nativeModel(
				[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c1", "lookup", `{}`)}}},
				[]*llm.StreamChunk{{Delta: "完成"}, {Done: true, FinishReason: "stop"}},
			)
			agent, err := NewSimpleBuilder(model).WithTools(tc.tools(t)...).WithMaxIterations(2).Build()
			require.NoError(t, err)
			sink := &toolEventSink{}
			func() {
				defer func() { _ = recover() }()
				_, _ = agent.AsRunnable().Invoke(observe.WithObserver(context.Background(), sink), "查询")
			}()

			var types []string
			for _, e := range sink.events {
				types = append(types, e.Type)
				require.Equal(t, "agent/lookup", e.Path)
			}
			require.Equal(t, tc.want, types)
			require.Equal(t, tc.kind, sink.events[len(sink.events)-1].ErrorKind)
		})
	}
}
