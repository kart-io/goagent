package providers

import (
	"testing"

	agentErrors "github.com/kart-io/goagent/errors"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestGemini阻断响应错误分类(t *testing.T) {
	_, err := decodeGeminiResponse(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{FinishReason: genai.FinishReasonMaxTokens}}})
	require.Equal(t, agentErrors.CodeLLMResponse, agentErrors.GetCode(err))
	var detail *agentErrors.AgentError
	require.ErrorAs(t, err, &detail)
	require.Equal(t, `gemini: incomplete or blocked response (finish="MAX_TOKENS")`, detail.Message)
	require.Equal(t, "gemini", detail.Component)
	require.Equal(t, "parse_response", detail.Operation)
	require.Equal(t, agentllm.ReasonUnknown, agentllm.ClassifyError(err).Reason, "协议错误不新增 ProviderError 重试身份")
}
