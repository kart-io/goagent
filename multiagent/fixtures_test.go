package multiagent

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// record 是单元测试用的类型化状态：Log 记录执行过的 Step，N 供路由读取。
type record struct {
	Log []string
	N   int
}

// mark 返回把自身名字追加到 Log 的 Step。
func mark(name string) Step[record] {
	return Step[record]{Name: name, Run: core.NewRunnableFunc(func(_ context.Context, r record) (record, error) {
		r.Log = append(append([]string(nil), r.Log...), name)
		r.N++
		return r, nil
	})}
}

func cloneRecord(r record) record { r.Log = append([]string(nil), r.Log...); return r }

// sessions 统计会话的创建与关闭次数。
type sessions struct{ opened, closed atomic.Int32 }

// fake 返回不调用模型的会话工厂：answer 根据输入计算 Result。
func (s *sessions) fake(answer func(context.Context, any) (any, error)) AgentFactory {
	return func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		s.opened.Add(1)
		return tools.OwnedRunnable[any, *builder.AgentOutput]{
			Runnable: core.NewRunnableFunc(func(ctx context.Context, in any) (*builder.AgentOutput, error) {
				result, err := answer(ctx, in)
				if err != nil {
					return nil, err
				}
				return &builder.AgentOutput{Result: result}, nil
			}),
			Close: func(context.Context) error { s.closed.Add(1); return nil },
		}, nil
	}
}

// scripted 返回真实 Builder 驱动的会话工厂；同一角色的 ScriptedClient 在各次会话之间共享以保留脚本进度。
func (s *sessions) scripted(client llm.Client, scope string, configure func(*builder.SimpleAgentBuilder) *builder.SimpleAgentBuilder) AgentFactory {
	return func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		b := builder.NewSimpleBuilder(client)
		if scope != "" {
			b = b.WithDurableScope(scope)
		}
		if configure != nil {
			b = configure(b)
		}
		agent, err := b.Build()
		if err != nil {
			return tools.OwnedRunnable[any, *builder.AgentOutput]{}, err
		}
		s.opened.Add(1)
		return tools.OwnedRunnable[any, *builder.AgentOutput]{
			Runnable: agent.AsRunnable(),
			Close:    func(ctx context.Context) error { s.closed.Add(1); return agent.Shutdown(ctx) },
		}, nil
	}
}

// withTools 把工具经独立 Dispatcher 包装后交给 Agent，满足持久接入契约。
func withTools(t *testing.T, cfg tools.DispatchConfig, list ...interfaces.Tool) func(*builder.SimpleAgentBuilder) *builder.SimpleAgentBuilder {
	t.Helper()
	reg := tools.NewRegistry()
	for _, tool := range list {
		require.NoError(t, reg.Register(tool))
	}
	d, err := tools.NewDispatcher(reg, cfg)
	require.NoError(t, err)
	return func(b *builder.SimpleAgentBuilder) *builder.SimpleAgentBuilder {
		return b.WithTools(d.Tools()...).WithMaxIterations(4)
	}
}

// countingTool 返回计数的只读查询工具。
func countingTool(t *testing.T, name, result string, calls *atomic.Int32) interfaces.Tool {
	t.Helper()
	tool, err := tools.NewFunctionToolBuilder(name).WithDescription("查询 " + name).
		WithArgsSchema(`{"type":"object","properties":{}}`).
		WithFunction(func(context.Context, map[string]interface{}) (interface{}, error) {
			calls.Add(1)
			return result, nil
		}).Build()
	require.NoError(t, err)
	return tool
}
