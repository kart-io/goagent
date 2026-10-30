package planning

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/testing/panictest"
)

// panicStepAgent 的 Invoke panic，模拟宿主实现的 Agent。
type panicStepAgent struct{ *core.BaseAgent }

func (panicStepAgent) Invoke(context.Context, *core.AgentInput) (*core.AgentOutput, error) {
	panic("step agent boom")
}

// okStepAgent 正常完成。
type okStepAgent struct{ *core.BaseAgent }

func (okStepAgent) Invoke(context.Context, *core.AgentInput) (*core.AgentOutput, error) {
	return &core.AgentOutput{Result: "ok", Status: "success"}, nil
}

// 同一层的步骤在 goroutine 中并行执行：Agent panic 时进程不得退出，记为该步骤失败，同层其他步骤照常完成。
func TestAgentExecutorStepPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		executor := NewAgentExecutor(&MockLogger{}, WithRetryPolicy(RetryPolicy{}))
		executor.RegisterAgent("boom", panicStepAgent{core.NewBaseAgent("boom", "", nil)})
		executor.RegisterAgent("ok", okStepAgent{core.NewBaseAgent("ok", "", nil)})
		result, err := executor.Execute(context.Background(), &Plan{ID: "p", Steps: []*Step{
			{ID: "s1", Agent: "boom"},
			{ID: "s2", Agent: "ok"},
		}})
		if err == nil || result.FailedSteps != 1 || result.CompletedSteps != 1 {
			t.Fatalf("应只有 boom 步骤失败：err=%v result=%+v", err, result)
		}
		if !result.StepResults["s2"].Success || result.StepResults["s1"].Error == "" {
			t.Fatalf("步骤结果不符合预期：%+v %+v", result.StepResults["s1"], result.StepResults["s2"])
		}
	})
}
