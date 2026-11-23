package builder

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

const unboundedWarning = "Agent 回合没有任何上限"

func TestUnboundedTurnWarning(t *testing.T) {
	withBudget := func(t *testing.T) context.Context {
		c, err := budget.New(budget.Config{RootKey: "warn", Version: "v1", MaxModelRequests: 10})
		require.NoError(t, err)
		return budget.WithController(context.Background(), c)
	}
	withDeadline := func(t *testing.T) context.Context {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		t.Cleanup(cancel)
		return ctx
	}
	for _, tc := range []struct {
		name    string
		tools   bool
		maxIter int
		timeout time.Duration
		ctx     func(*testing.T) context.Context
		warn    bool
	}{
		{"带工具且无任何上限时警告", true, 0, 0, nil, true},
		{"没有工具时不警告", false, 0, 0, nil, false},
		{"设置迭代上限时不警告", true, 5, 0, nil, false},
		{"设置回合时限时不警告", true, 0, time.Minute, nil, false},
		{"ctx 带截止时间时不警告", true, 0, 0, withDeadline, false},
		{"ctx 带预算时不警告", true, 0, 0, withBudget, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			var b *AgentBuilder[*core.AgentState]
			if tc.tools {
				b = NewSimpleBuilder(newScriptModel(answer("完成"), answer("完成"))).WithTools(plainTool())
			} else {
				b = NewSimpleBuilder(&eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "完成"}, {Done: true, FinishReason: "stop"}}})
			}
			b = b.WithLogger(debugLogger(&buf)).WithMaxIterations(tc.maxIter).WithTimeout(tc.timeout)
			agent, err := b.Build()
			require.NoError(t, err)
			ctx := context.Background()
			if tc.ctx != nil {
				ctx = tc.ctx(t)
			}
			for range 2 {
				_, err = agent.AsRunnable().Invoke(ctx, "问")
				require.NoError(t, err)
			}
			want := 0
			if tc.warn {
				want = 1
			}
			require.Equal(t, want, strings.Count(buf.String(), unboundedWarning), "同一 Agent 至多警告一次：\n%s", buf.String())
			if tc.warn {
				require.Contains(t, buf.String(), "level=WARN")
			}
		})
	}
}

// 执行闸门之前就检查，多个调用方同时进入时也只警告一次。
func TestUnboundedTurnWarningOnceUnderConcurrency(t *testing.T) {
	const callers = 16
	steps := make([]step, callers)
	for i := range steps {
		steps[i] = answer("完成")
	}
	var buf syncBuffer
	agent, err := NewSimpleBuilder(newScriptModel(steps...)).WithTools(plainTool()).WithLogger(slog.New(slog.NewTextHandler(&buf, nil))).Build()
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			_, err := agent.AsRunnable().Invoke(context.Background(), "问")
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, strings.Count(buf.String(), unboundedWarning))
}

func TestUnboundedTurnWarningOnStreamPath(t *testing.T) {
	var buf bytes.Buffer
	agent, err := NewSimpleBuilder(newScriptModel(answer("完成"))).WithTools(plainTool()).WithLogger(debugLogger(&buf)).Build()
	require.NoError(t, err)
	runToEnd(t, agent, context.Background())
	require.Equal(t, 1, strings.Count(buf.String(), unboundedWarning))
	require.Contains(t, buf.String(), "agent=")
}

// 子 Agent 继承外层 ctx：外层设了时限，内层即使自身不限也不警告。
func TestUnboundedTurnWarningNestedAgent(t *testing.T) {
	for _, tc := range []struct {
		name         string
		outerTimeout time.Duration
		innerWarns   int
	}{
		{"外层有时限时内层不警告", time.Minute, 0},
		{"外层也不限时内层照常警告", 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var outerLog, innerLog bytes.Buffer
			inner, err := NewSimpleBuilder(newScriptModel(answer("内层"))).WithTools(plainTool()).WithLogger(debugLogger(&innerLog)).Build()
			require.NoError(t, err)
			delegate := tools.NewBaseTool("delegate", "test", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				out, err := inner.AsRunnable().Invoke(ctx, "子问题")
				if err != nil {
					return nil, err
				}
				return &interfaces.ToolOutput{Success: true, Result: out.Result}, nil
			})
			outer, err := NewSimpleBuilder(newScriptModel(callTool("c1", "delegate"), answer("完成"))).
				WithTools(delegate).WithTimeout(tc.outerTimeout).WithLogger(debugLogger(&outerLog)).Build()
			require.NoError(t, err)
			out, err := outer.AsRunnable().Invoke(context.Background(), "问")
			require.NoError(t, err)
			require.Equal(t, "完成", out.Result)
			require.Equal(t, tc.innerWarns, strings.Count(innerLog.String(), unboundedWarning), innerLog.String())
		})
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
