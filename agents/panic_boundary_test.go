package agents_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/agents/base"
	"github.com/kart-io/goagent/agents/cot"
	"github.com/kart-io/goagent/agents/got"
	"github.com/kart-io/goagent/agents/metacot"
	"github.com/kart-io/goagent/agents/pot"
	"github.com/kart-io/goagent/agents/react"
	"github.com/kart-io/goagent/agents/sot"
	"github.com/kart-io/goagent/agents/tot"
	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/testing/panictest"
	"github.com/kart-io/goagent/tools"
)

// promptLLM 按最后一条消息决定响应：reply 返回内容，可在其中 panic；prompts 记录收到的全部提示。
type promptLLM struct {
	mu      sync.Mutex
	prompts []string
	reply   func(n int, prompt string) string
}

func (m *promptLLM) Chat(_ context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	prompt := messages[len(messages)-1].Content
	m.mu.Lock()
	m.prompts = append(m.prompts, prompt)
	n := len(m.prompts)
	m.mu.Unlock()
	return &llm.CompletionResponse{Content: m.reply(n, prompt)}, nil
}
func (m *promptLLM) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return m.Chat(ctx, req.Messages)
}
func (*promptLLM) Provider() constants.Provider { return constants.ProviderCustom }
func (*promptLLM) IsAvailable() bool            { return true }

func (m *promptLLM) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.prompts)
}

// toolPanicCallback 在 panicOn 指定的工具回调中 panic，并统计 OnAgentFinish 次数。
type toolPanicCallback struct {
	*core.BaseCallback
	panicOn  string
	finishes int
}

func (c *toolPanicCallback) hit(name string) error {
	if c.panicOn == name {
		panic("工具回调 panic")
	}
	return nil
}
func (c *toolPanicCallback) OnToolStart(context.Context, string, interface{}) error {
	return c.hit("tool_start")
}
func (c *toolPanicCallback) OnToolEnd(context.Context, string, interface{}) error {
	return c.hit("tool_end")
}
func (c *toolPanicCallback) OnToolError(context.Context, string, error) error {
	return c.hit("tool_error")
}
func (c *toolPanicCallback) OnAgentFinish(context.Context, interface{}) error {
	c.finishes++
	return nil
}

func reactToolLLM() *promptLLM {
	return &promptLLM{reply: func(n int, _ string) string {
		if n == 1 {
			return "Thought: 需要查询\nAction: lookup\nAction Input: {}"
		}
		return "Final Answer: 完成"
	}}
}

func lookupTool(err error) interfaces.Tool {
	return tools.NewBaseTool("lookup", "查询", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		if err != nil {
			return nil, err
		}
		return &interfaces.ToolOutput{Result: "结果", Success: true}, nil
	})
}

// ReAct 工具回调 panic 时中止本次执行：返回 *PanicError、状态失败，不写入观察、不再调用模型、不触发 OnAgentFinish。
func TestReActToolCallbackPanicAbortsRun(t *testing.T) {
	errTool := errors.New("工具失败")
	for _, tc := range []struct {
		panicOn string
		toolErr error
	}{
		{panicOn: "tool_start"},
		{panicOn: "tool_end"},
		{panicOn: "tool_error", toolErr: errTool},
	} {
		for _, mode := range []string{"invoke", "stream"} {
			t.Run(tc.panicOn+"/"+mode, func(t *testing.T) {
				model := reactToolLLM()
				cb := &toolPanicCallback{BaseCallback: core.NewBaseCallback(), panicOn: tc.panicOn}
				agent := react.NewReActAgent(react.ReActConfig{Name: "react", LLM: model, Tools: []interfaces.Tool{lookupTool(tc.toolErr)}}).WithCallbacks(cb)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				var out *core.AgentOutput
				var err error
				if mode == "invoke" {
					out, err = agent.Invoke(ctx, &core.AgentInput{Task: "任务"})
				} else {
					ch, sErr := agent.Stream(ctx, &core.AgentInput{Task: "任务"})
					require.NoError(t, sErr)
					for chunk := range ch {
						if chunk.Done {
							out, err = chunk.Data, chunk.Error
						}
					}
				}

				var panicErr *agentErrors.PanicError
				require.ErrorAs(t, err, &panicErr)
				require.Equal(t, "工具回调 panic", panicErr.Value)
				require.NotNil(t, out)
				require.Equal(t, interfaces.StatusFailed, out.Status)
				require.Equal(t, 1, model.calls(), "回调 panic 后不得再调用模型")
				require.Zero(t, cb.finishes)
				for _, s := range out.Steps {
					require.NotContains(t, s.Result, "panic", "panic 不得写入观察")
				}
			})
		}
	}
}

// ReAct 工具自身返回的普通错误仍作为观察交还模型。
func TestReActToolErrorBecomesObservation(t *testing.T) {
	model := reactToolLLM()
	agent := react.NewReActAgent(react.ReActConfig{Name: "react", LLM: model, Tools: []interfaces.Tool{lookupTool(errors.New("工具失败"))}})
	out, err := agent.Invoke(context.Background(), &core.AgentInput{Task: "任务"})
	require.NoError(t, err)
	require.Equal(t, interfaces.StatusSuccess, out.Status)
	require.Equal(t, 2, model.calls())
	require.Contains(t, model.prompts[1], "Error: 工具失败")
}

// panicErrTool 模拟经框架包装（如 Dispatcher）的工具：工具 panic 已被转换为 *PanicError 返回。
func panicErrTool(name string) interfaces.Tool {
	return tools.NewBaseTool(name, name, `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return nil, agentErrors.NewPanicError("工具 "+name, "工具 panic")
	})
}

// 推理 Agent 调用的工具返回 *PanicError 时中止本次执行，不当作普通工具失败继续推理。
func TestReasoningAgentToolPanicErrorAbortsRun(t *testing.T) {
	cases := map[string]func() (agentRunnable, *promptLLM){
		"cot": func() (agentRunnable, *promptLLM) {
			m := &promptLLM{reply: func(int, string) string {
				return "Step 1: 需要计算\nStep 2: USE_TOOL: calculator 2 + 2\nTherefore, the final answer is: 4"
			}}
			return cot.NewCoTAgent(cot.CoTConfig{Name: "cot", LLM: m, Tools: []interfaces.Tool{panicErrTool("calculator")}}), m
		},
		"tot": func() (agentRunnable, *promptLLM) {
			m := &promptLLM{reply: func(int, string) string {
				return "Step 1: calculate the total cost first\nStep 2: calculate the remaining budget"
			}}
			return tot.NewToTAgent(tot.ToTConfig{Name: "tot", LLM: m, EvaluationMethod: "heuristic", Tools: []interfaces.Tool{panicErrTool("calculator")}}), m
		},
		"metacot": func() (agentRunnable, *promptLLM) {
			m := &promptLLM{reply: func(int, string) string {
				return "1. What is the definition of this concept?\n2. What is the history of this concept?"
			}}
			return metacot.NewMetaCoTAgent(metacot.MetaCoTConfig{Name: "metacot", LLM: m, Tools: []interfaces.Tool{panicErrTool("search")}}), m
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			agent, _ := build()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			out, err := agent.Invoke(ctx, &core.AgentInput{Task: "What is the answer?"})
			var panicErr *agentErrors.PanicError
			require.ErrorAs(t, err, &panicErr)
			require.Equal(t, "工具 panic", panicErr.Value)
			if out != nil {
				require.Equal(t, interfaces.StatusFailed, out.Status)
			}
		})
	}
}

// panicStrategy 的 Execute 直接 panic。
type panicStrategy struct{ strategy }

func (panicStrategy) Execute(context.Context, *core.AgentInput, llm.Client, []interfaces.Tool, map[string]interfaces.Tool, *core.AgentOutput) (interface{}, error) {
	panic("strategy panic")
}

func llmPanicsOn(marker string) *promptLLM {
	return &promptLLM{reply: func(_ int, prompt string) string {
		if marker == "" || strings.Contains(prompt, marker) {
			panic("llm panic")
		}
		return ""
	}}
}

// 推理 Agent 后台 goroutine 中宿主代码 panic 时进程不退出，panic 以 *PanicError 交给错误出口，本次执行以失败结束。
func TestReasoningAgentGoroutinePanicIsPanicError(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		requirePanic := func(t *testing.T, err error, want string) {
			t.Helper()
			var panicErr *agentErrors.PanicError
			require.ErrorAs(t, err, &panicErr)
			require.Equal(t, want, panicErr.Value)
		}
		stream := func(t *testing.T, agent agentRunnable) (*core.AgentOutput, error) {
			t.Helper()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ch, err := agent.Stream(ctx, &core.AgentInput{Task: "任务"})
			require.NoError(t, err)
			var out *core.AgentOutput
			for chunk := range ch {
				if chunk.Done {
					out, err = chunk.Data, chunk.Error
				}
			}
			return out, err
		}

		// 并行 worker：GoT 节点处理、SoT 要点展开中的 LLM panic
		workers := map[string]agentRunnable{
			"got": got.NewGoTAgent(got.GoTConfig{Name: "got", LLM: llmPanicsOn("Provide your analysis"), ParallelExecution: true}),
			"sot": sot.NewSoTAgent(sot.SoTConfig{Name: "sot", LLM: llmPanicsOn("Elaborate on the following point")}),
		}
		for name, agent := range workers {
			t.Run("worker/"+name, func(t *testing.T) {
				out, err := agent.Invoke(context.Background(), &core.AgentInput{Task: "任务"})
				requirePanic(t, err, "llm panic")
				require.NotNil(t, out)
				require.Equal(t, interfaces.StatusFailed, out.Status)
			})
		}

		// Stream 的生产者 goroutine 中 strategy / LLM panic
		streams := map[string]agentRunnable{
			"reasoning": base.NewBaseReasoningAgent("reasoning", "", nil, scriptLLM{}, nil, panicStrategy{}),
			"got":       got.NewGoTAgent(got.GoTConfig{Name: "got", LLM: llmPanicsOn("")}),
			"sot":       sot.NewSoTAgent(sot.SoTConfig{Name: "sot", LLM: llmPanicsOn("")}),
			"tot":       tot.NewToTAgent(tot.ToTConfig{Name: "tot", LLM: llmPanicsOn("")}),
			"metacot":   metacot.NewMetaCoTAgent(metacot.MetaCoTConfig{Name: "metacot", LLM: llmPanicsOn("")}),
			"pot":       pot.NewPoTAgent(pot.PoTConfig{Name: "pot", LLM: llmPanicsOn("")}),
			"react":     react.NewReActAgent(react.ReActConfig{Name: "react", LLM: llmPanicsOn("")}),
		}
		for name, agent := range streams {
			t.Run("stream/"+name, func(t *testing.T) {
				_, err := stream(t, agent)
				want := "llm panic"
				if name == "reasoning" {
					want = "strategy panic"
				}
				requirePanic(t, err, want)
			})
		}
	})
}
