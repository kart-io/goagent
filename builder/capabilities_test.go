package builder

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func TestBuildRejectsUnavailableLoopFeatures(t *testing.T) {
	cases := []struct {
		name      string
		configure func(*SimpleAgentBuilder)
	}{
		{"PreStep", func(b *SimpleAgentBuilder) {
			b.WithPreStep(func(context.Context, *Step) (PreStepResult, error) { return PreStepResult{}, nil })
		}},
		{"RequestHook", func(b *SimpleAgentBuilder) {
			b.WithRequestHook(func(context.Context, *ModelRequest) error { return nil })
		}},
		{"TurnStopping", func(b *SimpleAgentBuilder) {
			b.WithTurnStopping(func(context.Context, *Stopping) (string, error) { return "", nil })
		}},
		{"PromptSections", func(b *SimpleAgentBuilder) {
			b.WithPromptSections(PromptSection{Text: func(PromptScope) string { return "说明" }})
		}},
		{"PromptContexts", func(b *SimpleAgentBuilder) {
			b.WithPromptContexts(PromptContext{Text: func(PromptScope) string { return "说明" }})
		}},
	}
	for _, tc := range cases {
		for _, mode := range []string{"无工具", "无原生接口", "完整工具流"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				var client llm.Client = nativeModel()
				if mode == "无原生接口" {
					client = NewMockLLMClient()
				}
				b := NewSimpleBuilder(client)
				cb := NewMockCallback()
				b.WithCallbacks(cb)
				if mode == "完整工具流" {
					b.WithTools(NewMockTool("lookup", "合成查询"))
				}
				tc.configure(b)
				_, err := b.BuildContext(context.Background())
				if mode == "完整工具流" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, tc.name)
					require.False(t, cb.onStartCalled)
				}
			})
		}
	}
}

func TestBuildAllowsPlainRetryAndFinalValidation(t *testing.T) {
	checked := false
	model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Done: true, Content: "完成"}}}
	a, err := NewSimpleBuilder(model).WithRequestError(1, RetryTransient(1, nil)).WithOutputValidator(func(context.Context, *AgentOutput) error { checked = true; return nil }).Build()
	require.NoError(t, err)
	out, err := a.AsRunnable().Invoke(context.Background(), "请求")
	require.NoError(t, err)
	require.Equal(t, "完成", out.Result)
	require.True(t, checked)
}
