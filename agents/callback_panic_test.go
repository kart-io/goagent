package agents_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/agents/base"
	"github.com/kart-io/goagent/agents/got"
	"github.com/kart-io/goagent/agents/metacot"
	"github.com/kart-io/goagent/agents/pot"
	"github.com/kart-io/goagent/agents/react"
	"github.com/kart-io/goagent/agents/sot"
	"github.com/kart-io/goagent/agents/tot"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/observe"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// scriptLLM 返回固定内容；err 非空时每次调用都失败。
type scriptLLM struct {
	content string
	err     error
}

func (m scriptLLM) Chat(context.Context, []llm.Message) (*llm.CompletionResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &llm.CompletionResponse{Content: m.content}, nil
}
func (m scriptLLM) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return m.Chat(ctx, req.Messages)
}
func (scriptLLM) Provider() constants.Provider { return constants.ProviderCustom }
func (scriptLLM) IsAvailable() bool            { return true }

// strategy 是 BaseReasoningAgent 的最小策略：err 非空时失败。
type strategy struct{ err error }

func (s strategy) Execute(context.Context, *core.AgentInput, llm.Client, []interfaces.Tool, map[string]interfaces.Tool, *core.AgentOutput) (interface{}, error) {
	return "ok", s.err
}
func (s strategy) ExecuteWithGenerator(ctx context.Context, input *core.AgentInput, c llm.Client, ts []interfaces.Tool, byName map[string]interfaces.Tool, out *core.AgentOutput, _ func(*core.AgentOutput, error) bool, _ time.Time) (interface{}, error) {
	return s.Execute(ctx, input, c, ts, byName, out)
}

// panicOnCallback 在 panicOn 指定的回调中 panic，calls 记录被调用的回调总数。
type panicOnCallback struct {
	*core.BaseCallback
	panicOn string
	calls   int
}

func (c *panicOnCallback) hit(name string) error {
	c.calls++
	if c.panicOn == name {
		panic("回调 panic")
	}
	return nil
}
func (c *panicOnCallback) OnStart(context.Context, any) error       { return c.hit("start") }
func (c *panicOnCallback) OnAgentFinish(context.Context, any) error { return c.hit("finish") }
func (c *panicOnCallback) OnError(context.Context, error) error     { return c.hit("error") }
func (c *panicOnCallback) OnLLMError(context.Context, error) error  { return c.hit("error") }

// 各推理 Agent 的回调 panic 都转为 *PanicError 替代原结果返回，且不再调用后续回调。
func TestAgentCallbackPanicIsPanicError(t *testing.T) {
	build := map[string]func(c llm.Client, s strategy) agentRunnable{
		"react": func(c llm.Client, _ strategy) agentRunnable {
			return react.NewReActAgent(react.ReActConfig{Name: "react", LLM: c})
		},
		"sot": func(c llm.Client, _ strategy) agentRunnable {
			return sot.NewSoTAgent(sot.SoTConfig{Name: "sot", LLM: c})
		},
		"got": func(c llm.Client, _ strategy) agentRunnable {
			return got.NewGoTAgent(got.GoTConfig{Name: "got", LLM: c})
		},
		"metacot": func(c llm.Client, _ strategy) agentRunnable {
			return metacot.NewMetaCoTAgent(metacot.MetaCoTConfig{Name: "metacot", LLM: c})
		},
		"pot": func(c llm.Client, _ strategy) agentRunnable {
			return pot.NewPoTAgent(pot.PoTConfig{Name: "pot", LLM: c})
		},
		"tot": func(c llm.Client, _ strategy) agentRunnable {
			return tot.NewToTAgent(tot.ToTConfig{Name: "tot", LLM: c})
		},
		"reasoning": func(c llm.Client, s strategy) agentRunnable {
			return base.NewBaseReasoningAgent("reasoning", "", nil, c, nil, s)
		},
	}
	cases := []struct {
		panicOn string
		llm     scriptLLM
		fail    error
		agents  []string
	}{
		{panicOn: "start", agents: []string{"react", "sot", "got", "metacot", "pot", "tot", "reasoning"}},
		// 执行失败后 OnError（ReAct 为 OnLLMError）自身 panic
		{panicOn: "error", llm: scriptLLM{err: errLLM}, fail: errLLM, agents: []string{"react", "sot", "got", "metacot", "pot", "tot", "reasoning"}},
		{panicOn: "finish", llm: scriptLLM{content: "Final Answer: 完成"}, agents: []string{"react", "reasoning"}},
	}
	for _, tc := range cases {
		for _, name := range tc.agents {
			t.Run(tc.panicOn+"/"+name, func(t *testing.T) {
				first := &panicOnCallback{BaseCallback: core.NewBaseCallback(), panicOn: tc.panicOn}
				second := &panicOnCallback{BaseCallback: core.NewBaseCallback()}
				agent := build[name](tc.llm, strategy{err: tc.fail}).WithCallbacks(first, second)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_, err := agent.Invoke(ctx, &core.AgentInput{Task: "任务"})
				var panicErr *agentErrors.PanicError
				require.ErrorAs(t, err, &panicErr)
				require.Equal(t, "回调 panic", panicErr.Value)
				require.Equal(t, "panic", observe.ErrorKind(err))
				if tc.fail != nil {
					require.NotErrorIs(t, err, tc.fail, "回调 panic 替代原错误返回")
				}
				// 第二个回调只收到 panic 之前的通知
				require.Equal(t, first.calls-1, second.calls)
			})
		}
	}
}
