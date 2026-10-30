package agents_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/agents/got"
	"github.com/kart-io/goagent/agents/metacot"
	"github.com/kart-io/goagent/agents/pot"
	"github.com/kart-io/goagent/agents/react"
	"github.com/kart-io/goagent/agents/sot"
	"github.com/kart-io/goagent/agents/tot"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

var errLLM = errors.New("LLM 失败")

type agentRunnable = core.Runnable[*core.AgentInput, *core.AgentOutput]

// countingLLM 记录调用次数；err 非空时每次都失败，否则返回 content；onCall 在每次调用时执行。
type countingLLM struct {
	calls   atomic.Int64
	content string
	err     error
	onCall  func()
}

func (m *countingLLM) Chat(context.Context, []llm.Message) (*llm.CompletionResponse, error) {
	m.calls.Add(1)
	if m.onCall != nil {
		m.onCall()
	}
	if m.err != nil {
		return nil, m.err
	}
	return &llm.CompletionResponse{Content: m.content}, nil
}
func (m *countingLLM) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return m.Chat(ctx, req.Messages)
}
func (*countingLLM) Provider() constants.Provider { return constants.ProviderCustom }
func (*countingLLM) IsAvailable() bool            { return true }

// 各推理 Agent 在默认配置下遇到持续失败的 LLM 时，须在有限次调用内返回错误，而不是空转到 ctx 截止。
func TestAgentPersistentLLMFailureReturnsPromptly(t *testing.T) {
	build := map[string]func(c llm.Client) agentRunnable{
		"got": func(c llm.Client) agentRunnable { return got.NewGoTAgent(got.GoTConfig{Name: "got", LLM: c}) },
		"tot": func(c llm.Client) agentRunnable { return tot.NewToTAgent(tot.ToTConfig{Name: "tot", LLM: c}) },
		"sot": func(c llm.Client) agentRunnable { return sot.NewSoTAgent(sot.SoTConfig{Name: "sot", LLM: c}) },
		"metacot": func(c llm.Client) agentRunnable {
			return metacot.NewMetaCoTAgent(metacot.MetaCoTConfig{Name: "metacot", LLM: c})
		},
		"pot":   func(c llm.Client) agentRunnable { return pot.NewPoTAgent(pot.PoTConfig{Name: "pot", LLM: c}) },
		"react": func(c llm.Client) agentRunnable { return react.NewReActAgent(react.ReActConfig{Name: "react", LLM: c}) },
	}
	for name, newAgent := range build {
		t.Run(name, func(t *testing.T) {
			c := &countingLLM{err: errLLM}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			start := time.Now()
			_, err := newAgent(c).Invoke(ctx, &core.AgentInput{Task: "任务"})
			elapsed := time.Since(start)
			t.Logf("耗时 %v，LLM 调用 %d 次", elapsed, c.calls.Load())
			require.ErrorIs(t, err, errLLM)
			require.NoError(t, ctx.Err(), "须在 ctx 截止前返回")
			require.Less(t, elapsed, time.Second)
			require.LessOrEqual(t, c.calls.Load(), int64(20))
		})
	}
}

// GoT 构图时一轮扩展没有新增节点（LLM 失败或输出无法解析）即停止，不再重复扩展同一批候选。
func TestGoTGraphBuildStopsWithoutProgress(t *testing.T) {
	cases := map[string]*countingLLM{
		"LLM 失败": {err: errLLM},
		"空输出":    {content: ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = got.NewGoTAgent(got.GoTConfig{Name: "got", LLM: c}).Invoke(ctx, &core.AgentInput{Task: "任务"})
			require.NoError(t, ctx.Err(), "须在 ctx 截止前返回")
			// 根节点扩展 1 次 + 根节点执行 1 次
			require.Equal(t, int64(2), c.calls.Load())
		})
	}
}

// GoT 构图期间 ctx 被取消时返回 ctx 错误，不再带着半张图继续执行。
func TestGoTGraphBuildRespectsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &countingLLM{content: "- 第一个后续思考，足够长的内容\n- 第二个后续思考，足够长的内容", onCall: cancel}
	_, err := got.NewGoTAgent(got.GoTConfig{Name: "got", LLM: c}).Invoke(ctx, &core.AgentInput{Task: "任务"})
	require.ErrorIs(t, err, context.Canceled)
}
