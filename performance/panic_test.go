package performance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/testing/panictest"
)

// panicInvokeAgent 的 Invoke 对 Task 为 "boom" 的输入 panic，模拟宿主实现的 Agent。
type panicInvokeAgent struct{ *core.BaseAgent }

func (panicInvokeAgent) Invoke(_ context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	if input.Task == "boom" {
		panic("agent boom")
	}
	return &core.AgentOutput{Result: input.Task, Status: "success"}, nil
}

func requirePerfPanic(t *testing.T, err error, value string) {
	t.Helper()
	var p *agentErrors.PanicError
	if !errors.As(err, &p) || p.Value != value {
		t.Fatalf("应返回 PanicError(%q)：%v", value, err)
	}
}

// Acquire 在等待 goroutine 中调用工厂：工厂 panic 时进程不得退出，Acquire 返回可识别的 PanicError。
func TestAgentPoolFactoryPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		pool, err := NewAgentPool(func() (core.Agent, error) { panic("factory boom") }, PoolConfig{MaxSize: 1, AcquireTimeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		_, err = pool.Acquire(context.Background())
		requirePerfPanic(t, err, "factory boom")
	})
}

// BatchExecutor 的 worker goroutine 中 Agent panic 时进程不得退出：该输入记为 PanicError，其他输入照常完成。
func TestBatchExecutorAgentPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		agent := panicInvokeAgent{core.NewBaseAgent("a", "", nil)}
		result := NewBatchExecutor(agent, BatchConfig{MaxConcurrency: 2, ErrorPolicy: ErrorPolicyContinue}).
			Execute(context.Background(), []*core.AgentInput{{Task: "ok"}, {Task: "boom"}})
		if len(result.Errors) != 1 || result.Errors[0].Index != 1 {
			t.Fatalf("应只有 boom 失败：%+v", result.Errors)
		}
		requirePerfPanic(t, result.Errors[0].Error, "agent boom")
		if result.Results[0] == nil || result.Results[0].Result != "ok" {
			t.Fatalf("其他输入应照常完成：%+v", result.Results)
		}
	})
}
