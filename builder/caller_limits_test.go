package builder

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

func TestMaxIterationsZeroIsUnlimited(t *testing.T) {
	const rounds = 15
	steps := make([]step, 0, rounds+1)
	for i := range rounds {
		steps = append(steps, callTool(fmt.Sprintf("c%d", i), "lookup"))
	}
	model := newScriptModel(append(steps, answer("完成"))...)
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "问")
	require.NoError(t, err)
	require.Equal(t, "完成", out.Result)
	require.Equal(t, rounds+1, model.count(), "旧默认 10 轮会在第 10 轮报错")
}

func TestToolsKeptOnLastRoundWhenUnlimited(t *testing.T) {
	for _, tc := range []struct {
		name      string
		max       int
		finalOnly any
	}{
		{"不限时最后一轮仍带工具", 0, nil},
		{"设置上限时最后一轮去工具", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := newScriptModel(callTool("c1", "lookup"), answer("完成"))
			agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithMaxIterations(tc.max).Build()
			require.NoError(t, err)
			out, err := agent.AsRunnable().Invoke(context.Background(), "问")
			require.NoError(t, err)
			require.Equal(t, tc.finalOnly, out.Metadata["final_answer_only"])
		})
	}
}

func TestBuilderTimeoutZeroIsUnlimited(t *testing.T) {
	for _, tc := range []struct {
		name     string
		timeout  time.Duration
		deadline bool
	}{
		{"默认不限时", -1, false},
		{"显式 0 不限时", 0, false},
		{"显式时限", time.Minute, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got bool
			model := newScriptModel(step{run: func(ctx context.Context, send func(*llm.StreamChunk) bool) {
				_, got = ctx.Deadline()
				answer("好").run(ctx, send)
			}})
			b := NewSimpleBuilder(model).WithTools(plainTool())
			if tc.timeout >= 0 {
				b.WithTimeout(tc.timeout)
			}
			agent, err := b.Build()
			require.NoError(t, err)
			_, err = agent.AsRunnable().Invoke(context.Background(), "问")
			require.NoError(t, err)
			require.Equal(t, tc.deadline, got)
		})
	}
}

func TestBuilderRejectsNegativeLimits(t *testing.T) {
	for name, set := range map[string]func(*SimpleAgentBuilder){
		"MaxIterations":   func(b *SimpleAgentBuilder) { b.WithMaxIterations(-1) },
		"Timeout":         func(b *SimpleAgentBuilder) { b.WithTimeout(-time.Second) },
		"PersistTimeout":  func(b *SimpleAgentBuilder) { b.WithPersistTimeout(-time.Second) },
		"ModelResends":    func(b *SimpleAgentBuilder) { b.WithInterruptedModelResends(-1) },
		"RequestRecovery": func(b *SimpleAgentBuilder) { b.WithRequestError(-1) },
	} {
		t.Run(name, func(t *testing.T) {
			b := NewSimpleBuilder(NewMockLLMClient())
			set(b)
			_, err := b.Build()
			require.Error(t, err)
		})
	}
}

func TestPersistTimeoutRequiredDefault(t *testing.T) {
	for _, tc := range []struct {
		set, want time.Duration
	}{
		{0, 5 * time.Second},
		{time.Minute, time.Minute},
	} {
		c := AgentConfig{PersistTimeout: tc.set}
		require.Equal(t, tc.want, c.persistTimeout())
	}
}

func TestRequestErrorZeroRecoveriesNeverRetries(t *testing.T) {
	down := &llm.ProviderError{StatusCode: 503, Err: fmt.Errorf("unavailable")}
	model := newScriptModel(failing(down), answer("不会到这里"))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithRequestError(0, RetryTransient(5, func(int) time.Duration { return 0 })).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "问")
	require.Error(t, err)
	require.Equal(t, 1, model.count())
}

func TestRetrievalZeroLimitsAreUnlimited(t *testing.T) {
	docs := make([]string, 6)
	for i := range docs {
		docs[i] = fmt.Sprintf("%06d", i)
	}
	evidence, metadata, err := retrieveContext(context.Background(), staticRetriever(docs...), RetrievalConfig{}, "q")
	require.NoError(t, err)
	require.Equal(t, 6, metadata["retrieval_document_count"], "旧默认只取 4 篇")
	require.Equal(t, 36, metadata["retrieval_context_runes"])
	require.Equal(t, false, metadata["retrieval_truncated"])
	require.Contains(t, evidence, "000005")
}

func TestInterruptedModelResendsZeroMeansOneSend(t *testing.T) {
	f := newResendFixture(t, 5)
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("丢失", 7), answerTurn("好的", 7))}
	agent, err := NewSimpleBuilder(model).WithDurableScope("resend").WithInterruptedModelResends(0).Build()
	require.NoError(t, err)
	r := agent.AsRunnable()
	*f.crash = true
	_, err = r.Invoke(f.ctx, "q")
	require.ErrorContains(t, err, "crash")
	*f.crash = false
	_, err = r.Invoke(f.ctx, "q")
	require.Error(t, err, "不重发时中断的请求交由宿主处理")
	require.Equal(t, 1, model.calls)
}

func TestInterruptedResendTwiceMeansThreeSends(t *testing.T) {
	for name, enable := range map[string]func(*SimpleAgentBuilder){
		"重发 2 次": func(b *SimpleAgentBuilder) { b.WithInterruptedModelResends(2) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newResendFixture(t, 5)
			model := &flakyModel{nativeEventModel: nativeModel(answerTurn("一", 7), answerTurn("二", 7), answerTurn("三", 7), answerTurn("四", 7))}
			b := NewSimpleBuilder(model).WithDurableScope("resend")
			enable(b)
			agent, err := b.Build()
			require.NoError(t, err)
			r := agent.AsRunnable()
			*f.crash = true
			for range 3 {
				_, err = r.Invoke(f.ctx, "q")
				require.ErrorContains(t, err, "crash")
			}
			*f.crash = false
			_, err = r.Invoke(f.ctx, "q")
			require.Error(t, err, "第 3 次发送后不再重发")
			require.Equal(t, 3, model.calls)
		})
	}
}

func staticRetriever(contents ...string) documentRetrieverFunc {
	return func(context.Context, string) ([]*interfaces.Document, error) {
		docs := make([]*interfaces.Document, len(contents))
		for i, c := range contents {
			docs[i] = &interfaces.Document{PageContent: c}
		}
		return docs, nil
	}
}
