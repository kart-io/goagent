package tools_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
	httptool "github.com/kart-io/goagent/tools/http"
	"github.com/kart-io/goagent/tools/shell"
)

type declaredTool struct {
	*dispatchTestTool
	concurrent bool
}

func (t *declaredTool) SupportsConcurrentCalls() bool { return t.concurrent }

// 包装只转述内层工具的声明：未声明的工具不再因经过 Dispatcher 就被当作可并发。
func TestDispatchedToolConcurrencyFollowsInner(t *testing.T) {
	plain := func() *dispatchTestTool {
		return &dispatchTestTool{name: "tool", schema: `{"type":"object"}`, run: func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Success: true}, nil
		}}
	}
	for _, tc := range []struct {
		name string
		tool interfaces.Tool
		want bool
	}{
		{"声明可并发", &declaredTool{dispatchTestTool: plain(), concurrent: true}, true},
		{"声明不可并发", &declaredTool{dispatchTestTool: plain(), concurrent: false}, false},
		{"未声明", plain(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := newTestDispatcher(t, tc.tool, tools.DispatchConfig{}).Tools()
			require.Len(t, wrapped, 1)
			c, ok := wrapped[0].(interfaces.ConcurrentCallTool)
			require.True(t, ok)
			require.Equal(t, tc.want, c.SupportsConcurrentCalls())
		})
	}
}

type interval struct{ start, end time.Time }

type intervalRecorder struct {
	mu   sync.Mutex
	seen map[string]interval
}

func (r *intervalRecorder) tool(name string, concurrent *bool) interfaces.Tool {
	inner := &dispatchTestTool{name: name, schema: `{"type":"object"}`, run: func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		start := time.Now()
		time.Sleep(50 * time.Millisecond)
		r.mu.Lock()
		r.seen[name] = interval{start, time.Now()}
		r.mu.Unlock()
		return &interfaces.ToolOutput{Success: true, Result: name}, nil
	}}
	if concurrent == nil {
		return inner
	}
	return &declaredTool{dispatchTestTool: inner, concurrent: *concurrent}
}

func overlaps(a, b interval) bool { return a.start.Before(b.end) && b.start.Before(a.end) }

// 一轮中只要有未声明并发的工具，整轮串行；只有声明可并发的读工具时才重叠执行。
func TestParallelRoundWithUndeclaredToolRunsSerially(t *testing.T) {
	yes := true
	for _, tc := range []struct {
		name       string
		undeclared bool
	}{
		{"含未声明工具", true},
		{"只有读工具", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &intervalRecorder{seen: map[string]interval{}}
			registry := tools.NewRegistry()
			require.NoError(t, registry.Register(rec.tool("read_a", &yes)))
			require.NoError(t, registry.Register(rec.tool("read_b", &yes)))
			calls := []string{"read_a", "read_b"}
			if tc.undeclared {
				require.NoError(t, registry.Register(rec.tool("write", nil)))
				calls = append(calls, "write")
			}
			d, err := tools.NewDispatcher(registry, tools.DispatchConfig{})
			require.NoError(t, err)
			round := mocks.ToolCallReply("并发轮次")
			for _, name := range calls {
				round.Reply.ToolCalls = append(round.Reply.ToolCalls, mocks.ToolCall("call-"+name, name, `{}`))
			}
			model := mocks.NewScriptedClient(round, mocks.TextReply("结束", "完成"))
			agent, err := builder.NewSimpleBuilder(model).WithTools(d.Tools()...).WithParallelToolCalls(4).WithMaxIterations(3).Build()
			require.NoError(t, err)
			_, err = agent.AsRunnable().Invoke(context.Background(), "开始")
			require.NoError(t, err)
			require.Len(t, rec.seen, len(calls))
			if tc.undeclared {
				for i, a := range calls {
					for _, b := range calls[i+1:] {
						require.False(t, overlaps(rec.seen[a], rec.seen[b]), "%s 与 %s 不能重叠", a, b)
					}
				}
				return
			}
			require.True(t, overlaps(rec.seen["read_a"], rec.seen["read_b"]), "两个读工具应并发执行")
		})
	}
}

// 有副作用的内置工具声明只执行一次：崩溃后不自动重跑，交给宿主核对。
func TestShellAndHTTPPostDeclareSingleAttempt(t *testing.T) {
	sh := shell.NewShellTool([]string{"echo"}, 0)
	p, ok := any(sh).(tools.DurablePolicyProvider)
	require.True(t, ok)
	require.Equal(t, 1, p.DurableOperationPolicy().MaxAttempts)
	require.False(t, p.DurableOperationPolicy().RetryInterrupted)

	api := httptool.NewAPITool("", 0, nil)
	cp, ok := any(api).(tools.DurableCallPolicyProvider)
	require.True(t, ok)
	for _, tc := range []struct {
		method string
		single bool
	}{
		{"", false},
		{"GET", false},
		{"get", false},
		{"HEAD", false},
		{"POST", true},
		{"PUT", true},
		{"DELETE", true},
		{"PATCH", true},
	} {
		args := map[string]any{"url": "https://example.com"}
		if tc.method != "" {
			args["method"] = tc.method
		}
		policy, declared := cp.DurableCallPolicy(args)
		require.Equal(t, tc.single, declared, tc.method)
		if tc.single {
			require.Equal(t, 1, policy.MaxAttempts, tc.method)
			require.False(t, policy.RetryInterrupted, tc.method)
		}
	}
}
