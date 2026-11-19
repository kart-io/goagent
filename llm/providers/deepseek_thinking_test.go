package providers

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/usage"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
)

type streamClient interface {
	CompleteStream(context.Context, *agentllm.CompletionRequest) (<-chan *agentllm.StreamChunk, error)
	CompleteToolsStream(context.Context, *agentllm.CompletionRequest, []interfaces.Tool) (<-chan *agentllm.StreamChunk, error)
}

func newCapturedDeepSeek(t *testing.T, opts ...agentllm.ClientOption) (*DeepSeekProvider, *captureTripper) {
	t.Helper()
	t.Setenv("DEEPSEEK_BASE_URL", "")
	t.Setenv("DEEPSEEK_MODEL", "")
	tripper := &captureTripper{}
	opts = append([]agentllm.ClientOption{agentllm.WithAPIKey("k"), agentllm.WithHTTPClient(&http.Client{Transport: tripper})}, opts...)
	p, err := NewDeepSeekWithOptions(opts...)
	require.NoError(t, err)
	return p, tripper
}

// sendAllPaths 依次走 Complete、CompleteStream 与 CompleteToolsStream，各发一个请求。
func sendAllPaths(t *testing.T, p streamClient, req func() *agentllm.CompletionRequest) {
	t.Helper()
	client, ok := p.(agentllm.Client)
	require.True(t, ok)
	_, err := agentllm.Complete(context.Background(), client, req())
	require.NoError(t, err)
	ch, err := p.CompleteStream(context.Background(), req())
	drainStream(t, ch, err)
	ch, err = p.CompleteToolsStream(context.Background(), req(), nil)
	drainStream(t, ch, err)
}

func TestDeepSeekThinkingDisabledOnlyForNoneEffort(t *testing.T) {
	none := &usage.ReasoningConfig{Effort: "none"}
	tests := []struct {
		name         string
		opts         []agentllm.ClientOption
		reasoning    *usage.ReasoningConfig
		wantThinking bool
	}{
		{name: "request effort none", reasoning: none, wantThinking: true},
		{name: "client effort none", opts: []agentllm.ClientOption{agentllm.WithReasoning(*none)}, wantThinking: true},
		{name: "no reasoning"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, tripper := newCapturedDeepSeek(t, tt.opts...)
			sendAllPaths(t, p, func() *agentllm.CompletionRequest {
				return &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("hi")}, Reasoning: tt.reasoning}
			})

			require.Len(t, tripper.bodies, 3)
			for _, body := range tripper.bodies {
				require.NotContains(t, body, "reasoning_effort")
				if tt.wantThinking {
					require.JSONEq(t, `{"type":"disabled"}`, string(body["thinking"]))
				} else {
					require.NotContains(t, body, "thinking")
				}
			}
		})
	}
}

func TestDeepSeekRejectsOtherReasoningConfig(t *testing.T) {
	tests := []struct {
		name      string
		reasoning usage.ReasoningConfig
	}{
		{name: "effort low", reasoning: usage.ReasoningConfig{Effort: "low"}},
		{name: "thinking budget", reasoning: usage.ReasoningConfig{BudgetTokens: 1024, BudgetSet: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, tripper := newCapturedDeepSeek(t)
			req := &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("hi")}, Reasoning: &tt.reasoning}

			_, err := p.CompleteToolsStream(context.Background(), req, nil)
			require.True(t, agentErrors.IsCode(err, agentErrors.CodeInvalidConfig), "err = %v", err)
			require.Error(t, p.ValidateReasoning(req))
			require.Empty(t, tripper.bodies)
		})
	}
}
