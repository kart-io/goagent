package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

type completionFunc func(context.Context, *llm.CompletionRequest) (*llm.CompletionResponse, error)

func (f completionFunc) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return f(ctx, req)
}

func fakeJudge(content string) *modelJudge {
	return &modelJudge{provider: "fake", model: "test-model", client: completionFunc(func(context.Context, *llm.CompletionRequest) (*llm.CompletionResponse, error) {
		return &llm.CompletionResponse{Content: content, FinishReason: "stop"}, nil
	})}
}

func TestModelDecisionValidation(t *testing.T) {
	for _, content := range []string{
		"```json\n{}\n```", `null`, `{}`, `{"decision":"handoff","reason":"r"}`,
		`{"decision":"handoff","reason":"r","source":null}`,
		`{"decision":"handoff","reason":"r","source":"","command":"send"}`,
		`{"decision":"handoff","reason":"r","source":""} {}`,
		`{"decision":"restart","reason":"r","source":""}`,
		`{"decision":"reply","reason":"r","source":"invented"}`,
		`{"decision":"handoff","reason":" ","source":""}`,
		`{"decision":"handoff","reason":"r","source":"demo-faq-v1/hours"}`,
	} {
		_, err := decodeDecision("auto-reply", content)
		require.Error(t, err, content)
	}
	_, err := decodeDecision("auto-reply", `{"decision":"reply","reason":"matching FAQ","source":"demo-faq-v1/hours"}`)
	require.NoError(t, err)
}

func TestLiveModelCannotBypassDevOpsPolicy(t *testing.T) {
	ctx := context.Background()
	store, b := testStores(t)
	b.model = fakeJudge(`{"decision":"restart","reason":"incorrect recommendation","source":""}`)
	g, err := devopsGraph(ctx, store, b)
	require.NoError(t, err)
	s, err := g.Run(ctx, "r", devopsState{IncidentID: "i", Service: "demo-api", Healthy: true}, "")
	require.NoError(t, err)
	require.True(t, s.Done)
	require.Equal(t, "escalated", s.State.Outcome)
	noEffect(t, b, "restart_service", "i")
}

func TestLiveModelCannotBypassArticleBlock(t *testing.T) {
	ctx := context.Background()
	store, b := testStores(t)
	b.model = fakeJudge(`{"decision":"accept","reason":"incorrect recommendation","source":""}`)
	g, err := articleGraph(ctx, store, b)
	require.NoError(t, err)
	s, err := g.Run(ctx, "r", articleState{ArticleID: "a", Title: "DEMO_BLOCK", Body: strings.Repeat("文", 80)}, "")
	require.NoError(t, err)
	require.Equal(t, "rejected", s.State.Decision)
	noEffect(t, b, "publish_article", "a")
}

func TestLiveModelFailureProducesNoEffect(t *testing.T) {
	for _, mode := range []string{"network", "invalid", "truncated", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			store, b := testStores(t)
			b.model = &modelJudge{provider: "fake", model: "test-model", client: completionFunc(func(ctx context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
				switch mode {
				case "network":
					return nil, errors.New("provider unavailable")
				case "invalid":
					return &llm.CompletionResponse{Content: "not JSON", FinishReason: "stop"}, nil
				case "canceled":
					return nil, context.Canceled
				default:
					return &llm.CompletionResponse{Content: `{}`, FinishReason: "length"}, nil
				}
			})}
			g, err := replyGraph(ctx, store, b)
			require.NoError(t, err)
			_, err = g.Run(ctx, "r", replyState{MessageID: "m", Recipient: "demo-user", Message: "几点营业？"}, "")
			require.Error(t, err)
			noEffect(t, b, "send_reply", "m")
			require.EqualValues(t, 1, b.model.calls.Load())
		})
	}
}

func TestLiveDecisionCheckpointPreventsRepeatedModelCall(t *testing.T) {
	ctx := context.Background()
	store, b := testStores(t)
	b.model = fakeJudge(`{"decision":"reply","reason":"hours paraphrase","source":"demo-faq-v1/hours"}`)
	b.after = func() error { return errors.New("ambiguous send") }
	g, err := replyGraph(ctx, store, b)
	require.NoError(t, err)
	_, err = g.Run(ctx, "r", replyState{MessageID: "m", Recipient: "demo-user", Message: "几点开门？"}, "")
	require.ErrorContains(t, err, "ambiguous send")
	first := readEffect(t, b, "send_reply", "m")
	b.after = nil
	g, err = replyGraph(ctx, store, b)
	require.NoError(t, err)
	s, err := g.Run(ctx, "r", replyState{}, "")
	require.NoError(t, err)
	require.EqualValues(t, 1, b.model.calls.Load())
	require.Equal(t, first.Receipt, s.State.Receipt)
	require.Equal(t, "demo-faq-v1/hours", s.State.Source)
	require.Equal(t, "reply", s.State.ModelReview.Decision)
	// Offline or changed model cannot silently resume the live plan.
	b.model = nil
	offline, err := replyGraph(ctx, store, b)
	require.NoError(t, err)
	_, err = offline.Run(ctx, "r", replyState{}, "")
	require.Error(t, err)
}

func TestModelBudgetAndInputBound(t *testing.T) {
	m := fakeJudge(`{"decision":"handoff","reason":"unknown","source":""}`)
	_, err := m.judge(context.Background(), "auto-reply", strings.Repeat("x", 16001))
	require.Error(t, err)
	require.Zero(t, m.calls.Load())
	_, err = m.judge(context.Background(), "auto-reply", "question")
	require.NoError(t, err)
	_, err = m.judge(context.Background(), "auto-reply", "question")
	require.ErrorContains(t, err, "budget")
	require.EqualValues(t, 1, m.calls.Load())
}
