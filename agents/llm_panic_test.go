package agents_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/agents"
	"github.com/kart-io/goagent/agents/cot"
	"github.com/kart-io/goagent/agents/got"
	"github.com/kart-io/goagent/agents/metacot"
	"github.com/kart-io/goagent/agents/sot"
	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/tools"
)

// panicOnPromptLLM 模拟 LLM 客户端（或其中间件）已把 panic 恢复为 *PanicError 返回：
// 最后一条消息包含 marker 时返回 *PanicError（plain 为 true 时返回普通错误），否则返回 reply；
// afterPanic 统计 panic 之后的调用次数。
type panicOnPromptLLM struct {
	marker     string
	reply      string
	plain      bool
	panicked   atomic.Bool
	afterPanic atomic.Int64
}

func (m *panicOnPromptLLM) Chat(_ context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	if m.panicked.Load() {
		m.afterPanic.Add(1)
	}
	if strings.Contains(messages[len(messages)-1].Content, m.marker) {
		if m.plain {
			return nil, errLLM
		}
		m.panicked.Store(true)
		return nil, agentErrors.NewPanicError("LLM 客户端", "llm panic")
	}
	return &llm.CompletionResponse{Content: m.reply}, nil
}
func (m *panicOnPromptLLM) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return m.Chat(ctx, req.Messages)
}
func (*panicOnPromptLLM) Provider() constants.Provider { return constants.ProviderCustom }
func (*panicOnPromptLLM) IsAvailable() bool            { return true }

type fastInvoker interface {
	InvokeFast(context.Context, *core.AgentInput) (*core.AgentOutput, error)
}

type generatorRunner interface {
	RunGenerator(context.Context, *core.AgentInput) core.Generator[*core.AgentOutput]
}

// 推理 Agent 在 LLM 失败时的降级分支（默认答案、中性评分、跳过该步）不得吞掉 *PanicError：
// Invoke / InvokeFast / RunGenerator 都须返回该错误并以失败结束，且 panic 之后不再调用模型。
func TestReasoningAgentLLMPanicErrorNotDegraded(t *testing.T) {
	const thoughts = "- First follow-up thought that is long enough\n- Second follow-up thought that is long enough"
	const questions = "Q: Should we improve the first part of the comparison?\nQ: Should we improve the second part?"
	cases := map[string]func(m *panicOnPromptLLM) (agentRunnable, string){
		"sot/weighted_aggregation": func(m *panicOnPromptLLM) (agentRunnable, string) {
			m.marker, m.reply = "Synthesize them into a coherent answer", "1. First point: detail\n2. Second point: detail"
			return sot.NewSoTAgent(sot.SoTConfig{Name: "sot", LLM: m, AggregationStrategy: "weighted"}), "任务"
		},
		"got/generate_thoughts": func(m *panicOnPromptLLM) (agentRunnable, string) {
			m.marker, m.reply = "Generate 2-3 different follow-up thoughts", thoughts
			return got.NewGoTAgent(got.GoTConfig{Name: "got", LLM: m}), "任务"
		},
		"got/evaluate_thought": func(m *panicOnPromptLLM) (agentRunnable, string) {
			m.marker, m.reply = "Rate the following thought", thoughts
			return got.NewGoTAgent(got.GoTConfig{Name: "got", LLM: m, FastEvaluation: false}), "任务"
		},
		"cot/reasoning_with_tools": func(m *panicOnPromptLLM) (agentRunnable, string) {
			m.marker, m.reply = "Tool execution results", "Step 1: 需要计算\nStep 2: USE_TOOL: calculator 2 + 2\nTherefore, the final answer is: 4"
			calc := tools.NewBaseTool("calculator", "计算", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				return &interfaces.ToolOutput{Result: 4, Success: true}, nil
			})
			return cot.NewCoTAgent(cot.CoTConfig{Name: "cot", LLM: m, Tools: []interfaces.Tool{calc}}), "What is 2 + 2?"
		},
	}
	metacotMarkers := map[string]string{
		"decompose":  "Decompose this complex question",
		"followup":   "What follow-up questions would help",
		"synthesize": "Synthesize a complete answer to",
		"critique":   "Critically evaluate this answer",
		"refine":     "Given this answer and critique",
	}
	for name, marker := range metacotMarkers {
		cases["metacot/"+name] = func(m *panicOnPromptLLM) (agentRunnable, string) {
			m.marker, m.reply = marker, questions
			return metacot.NewMetaCoTAgent(metacot.MetaCoTConfig{
				Name: "metacot", LLM: m, MaxDepth: 1, AutoDecompose: true, SelfCritique: true,
			}), "Compare and contrast option A and option B"
		}
	}

	// 普通错误仍走原降级分支，本次执行成功
	for name, build := range cases {
		t.Run(name+"/plain_error", func(t *testing.T) {
			m := &panicOnPromptLLM{}
			agent, task := build(m)
			m.plain = true
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			out, err := agent.Invoke(ctx, &core.AgentInput{Task: task})
			require.NoError(t, err)
			require.Equal(t, interfaces.StatusSuccess, out.Status)
		})
	}

	for name, build := range cases {
		for _, mode := range []string{"invoke", "invoke_fast", "generator"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				m := &panicOnPromptLLM{}
				agent, task := build(m)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				input := &core.AgentInput{Task: task}

				var out *core.AgentOutput
				var err error
				switch mode {
				case "invoke":
					out, err = agent.Invoke(ctx, input)
				case "invoke_fast":
					f, ok := agent.(fastInvoker)
					require.True(t, ok)
					out, err = f.InvokeFast(ctx, input)
				case "generator":
					g, ok := agent.(generatorRunner)
					require.True(t, ok)
					for o, e := range g.RunGenerator(ctx, input) {
						out = o
						if e != nil {
							err = e
							break
						}
					}
				}

				require.True(t, m.panicked.Load(), "用例未触达目标 LLM 调用")
				var panicErr *agentErrors.PanicError
				require.ErrorAs(t, err, &panicErr)
				require.Equal(t, "llm panic", panicErr.Value)
				if out != nil {
					require.Equal(t, interfaces.StatusFailed, out.Status)
				}
				require.Zero(t, m.afterPanic.Load(), "panic 后不得继续调用模型")
			})
		}
	}
}

// panicRouter 的 Route 返回 *PanicError；routed 统计被调用次数。
type panicRouter struct {
	agents.AgentRouter
	routed atomic.Int64
}

func (r *panicRouter) Route(context.Context, agents.Task, map[string]core.Agent) (string, error) {
	r.routed.Add(1)
	return "", agentErrors.NewPanicError("路由策略", "router panic")
}

// HybridRouter 中某个策略返回 *PanicError 时直接返回该错误，不再投票或退回 fallback。
func TestHybridRouterStrategyPanicErrorNotDegraded(t *testing.T) {
	fallback := &panicRouter{}
	h := agents.NewHybridRouter(fallback)
	h.AddStrategy(agents.NewLLMRouter(&panicOnPromptLLM{marker: ""}), 1)

	_, err := h.Route(context.Background(), agents.Task{ID: "t"}, map[string]core.Agent{})
	var panicErr *agentErrors.PanicError
	require.ErrorAs(t, err, &panicErr)
	require.Equal(t, "llm panic", panicErr.Value)
	require.Zero(t, fallback.routed.Load(), "不得退回 fallback")
}
