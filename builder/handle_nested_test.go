package builder

import (
	"context"
	"sync"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// 工具内运行的子 Agent 继承父 Agent 的 ctx，但父 Handle 的输入属于父 Agent，子 Agent 不能领取。
func TestNestedAgentDoesNotClaimParentHandleInputs(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	childModel := newScriptModel(answer("child answer"), answer("stolen wake"))
	child, err := NewSimpleBuilder(childModel).WithTools(plainTool()).WithMaxIterations(2).
		WithRequestHook(func(context.Context, *ModelRequest) error {
			once.Do(func() {
				close(entered)
				<-release
			})
			return nil
		}).Build()
	require.NoError(t, err)
	delegate := tools.NewBaseTool("delegate", "test", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		out, err := child.AsRunnable().Invoke(ctx, "child task")
		if err != nil {
			return nil, err
		}
		return &interfaces.ToolOutput{Success: true, Result: out.Result}, nil
	})
	parentModel := newScriptModel(callTool("call-1", "delegate"), answer("parent answer"))
	parent, err := NewSimpleBuilder(parentModel).WithTools(delegate).WithMaxIterations(4).Build()
	require.NoError(t, err)
	h := parent.Start(context.Background(), "parent task")
	<-entered
	require.NoError(t, h.Steer("for the parent"))
	close(release)
	require.Equal(t, "parent answer", waitAnswer(t, h).Result)
	require.NotContains(t, requestText(childModel.request(0)), "for the parent")
	require.Equal(t, 1, childModel.count())
	require.Contains(t, requestText(parentModel.request(1)), "for the parent")
}

// 第二轮的工具里运行的子 Agent 不能继承父 Agent 上一轮的对话。
func TestNestedAgentDoesNotInheritParentFollowupHistory(t *testing.T) {
	childModel := newScriptModel(answer("child answer"))
	child, err := NewSimpleBuilder(childModel).WithTools(plainTool()).WithMaxIterations(2).Build()
	require.NoError(t, err)
	delegate := tools.NewBaseTool("delegate", "test", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		out, err := child.AsRunnable().Invoke(ctx, "child task")
		if err != nil {
			return nil, err
		}
		return &interfaces.ToolOutput{Success: true, Result: out.Result}, nil
	})
	parentModel := newScriptModel(answer("secret from first turn"), callTool("call-1", "delegate"), answer("second turn done"))
	started, release := make(chan struct{}), make(chan struct{})
	parent, err := NewSimpleBuilder(parentModel).WithTools(delegate).WithMaxIterations(4).
		WithTurnStopping(func() func(context.Context, *Stopping) (string, error) {
			var once sync.Once
			return func(context.Context, *Stopping) (string, error) {
				once.Do(func() {
					close(started)
					<-release
				})
				return "", nil
			}
		}()).Build()
	require.NoError(t, err)
	h := parent.Start(context.Background(), "first")
	<-started
	require.NoError(t, h.Followup("second"))
	close(release)
	require.Equal(t, "second turn done", waitAnswer(t, h).Result)
	require.NotContains(t, requestText(childModel.request(0)), "secret from first turn")
}
