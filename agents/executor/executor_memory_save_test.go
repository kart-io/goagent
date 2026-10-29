package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/agents/base"
	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
)

// saveContextCases 覆盖 SaveContext 的三种失败：panic、返回 PanicError、返回普通错误
var saveContextCases = []struct {
	name      string
	setup     func(*mock.Call)
	wantPanic bool
}{
	{
		name:      "SaveContext 发生 panic",
		setup:     func(c *mock.Call) { c.Run(func(mock.Arguments) { panic("memory boom") }).Return(nil) },
		wantPanic: true,
	},
	{
		name:      "SaveContext 返回 PanicError",
		setup:     func(c *mock.Call) { c.Return(agentErrors.NewPanicError("自定义 memory", "boom")) },
		wantPanic: true,
	},
	{
		name:  "SaveContext 返回普通错误",
		setup: func(c *mock.Call) { c.Return(errors.New("存储不可用")) },
	},
}

func newSaveContextExecutor(setup func(*mock.Call)) *AgentExecutor {
	mockAgent := new(MockAgent)
	mockAgent.On("Invoke", mock.Anything, mock.Anything).Return(&agentcore.AgentOutput{
		Result:   "answer",
		Status:   "success",
		Metadata: map[string]interface{}{},
	}, nil)
	mockMemory := new(MockMemory)
	mockMemory.On("LoadHistory", mock.Anything, "s1").Return([]map[string]interface{}{}, nil)
	setup(mockMemory.On("SaveContext", mock.Anything, "s1", mock.Anything, mock.Anything))

	return NewAgentExecutor(ExecutorConfig{Agent: mockAgent, Memory: mockMemory, MaxIterations: 15})
}

func TestExecute_SaveContextFailure(t *testing.T) {
	for _, tt := range saveContextCases {
		t.Run(tt.name, func(t *testing.T) {
			executor := newSaveContextExecutor(tt.setup)

			output, err := executor.Execute(context.Background(), &agentcore.AgentInput{Task: "q", SessionID: "s1"})

			if tt.wantPanic {
				require.Error(t, err)
				assert.True(t, base.IsPanic(err), "PanicError 必须沿错误链返回: %v", err)
				require.NotNil(t, output)
				assert.Equal(t, "failed", output.Status)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "success", output.Status)
			assert.Equal(t, "answer", output.Result)
		})
	}
}

func TestRunGenerator_SaveContextFailure(t *testing.T) {
	for _, tt := range saveContextCases {
		t.Run(tt.name, func(t *testing.T) {
			executor := newSaveContextExecutor(tt.setup)

			var outputs []*agentcore.AgentOutput
			var errs []error
			for output, err := range executor.RunGenerator(context.Background(), &agentcore.AgentInput{Task: "q", SessionID: "s1"}) {
				outputs = append(outputs, output)
				errs = append(errs, err)
			}

			if tt.wantPanic {
				require.Len(t, outputs, 2, "成功输出之后须再发出一条失败终态")
				assert.NoError(t, errs[0])
				last := outputs[len(outputs)-1]
				require.NotNil(t, last)
				assert.Equal(t, "failed", last.Status)
				require.Error(t, errs[len(errs)-1])
				assert.True(t, base.IsPanic(errs[len(errs)-1]), "PanicError 必须沿错误链返回: %v", errs[len(errs)-1])
				return
			}
			require.Len(t, outputs, 1)
			assert.NoError(t, errs[0])
			assert.Equal(t, "success", outputs[0].Status)
		})
	}
}
