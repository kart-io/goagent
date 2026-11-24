package providers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/usage"
	agentllm "github.com/kart-io/goagent/llm"
)

// 客户端级推理默认值与单次请求推理配置须一致：单次请求接受的配置，作为客户端默认值时构造也应接受；反之亦然。
// 未指定 reasoning 的行用 effort=none（DeepSeek 只接受它）；Gemini 不接受 none，改用 low。
func TestClientReasoningMatchesPerRequestSupport(t *testing.T) {
	presets := []struct {
		name      string
		newClient func(...agentllm.ClientOption) (agentllm.Client, error)
		reasoning *usage.ReasoningConfig
	}{
		{name: "openai", newClient: func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewOpenAIWithOptions(o...) }},
		{name: "minimax", newClient: func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewMiniMaxWithOptions(o...) }},
		{name: "deepseek", newClient: func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewDeepSeekWithOptions(o...) }},
		{name: "gemini", newClient: func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewGeminiWithOptions(o...) }, reasoning: &usage.ReasoningConfig{Effort: "low"}},
		{name: "anthropic", newClient: func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewAnthropicWithOptions(o...) }},
		{name: "kimi", newClient: func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewKimiWithOptions(o...) }},
		{name: "siliconflow", newClient: func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewSiliconFlowWithOptions(o...) }},
		{name: "ollama", newClient: func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewOllamaWithOptions(o...) }},
	}
	for _, tt := range presets {
		t.Run(tt.name, func(t *testing.T) {
			reasoning := usage.ReasoningConfig{Effort: "none"}
			if tt.reasoning != nil {
				reasoning = *tt.reasoning
			}
			plain, err := tt.newClient(agentllm.WithAPIKey("k"))
			require.NoError(t, err)
			perRequestErr := agentllm.ValidateReasoningRequest(plain, &agentllm.CompletionRequest{Reasoning: &reasoning})

			client, ctorErr := tt.newClient(agentllm.WithAPIKey("k"), agentllm.WithReasoning(reasoning))
			if perRequestErr != nil {
				require.ErrorContains(t, perRequestErr, "does not support explicit reasoning")
				require.ErrorContains(t, ctorErr, "does not support")
				return
			}
			require.NoError(t, ctorErr, "单次请求支持推理配置时，客户端级默认值不应在构造时被拒绝")
			require.NoError(t, agentllm.ValidateReasoningRequest(client, &agentllm.CompletionRequest{}))
		})
	}
}

// MiniMax 客户端级推理默认值按与单次请求相同的方式写入请求体。
func TestMiniMaxClientReasoningDefault(t *testing.T) {
	tests := []struct {
		name          string
		model         string
		reasoning     usage.ReasoningConfig
		wantField     string
		wantValue     string
		unwantedField string
	}{
		{name: "M3 默认 none 关闭 thinking", model: "MiniMax-M3", reasoning: usage.ReasoningConfig{Effort: "none"}, wantField: "thinking", wantValue: `{"type":"disabled"}`, unwantedField: "reasoning_effort"},
		{name: "M3 默认 low 发送 reasoning_effort", model: "MiniMax-M3", reasoning: usage.ReasoningConfig{Effort: "low"}, wantField: "reasoning_effort", wantValue: `"low"`, unwantedField: "thinking"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, tripper := newCapturedMiniMax(t, agentllm.WithModel(tt.model), agentllm.WithReasoning(tt.reasoning))

			_, err := agentllm.Complete(context.Background(), p, &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("hi")}})
			require.NoError(t, err)

			require.Len(t, tripper.bodies, 1)
			require.JSONEq(t, tt.wantValue, string(tripper.bodies[0][tt.wantField]))
			require.NotContains(t, tripper.bodies[0], tt.unwantedField)
		})
	}
}
