package providers

import (
	"slices"
	"strings"

	"github.com/sashabaranov/go-openai"
)

// openAIModelLimits 是 OpenAI 推理模型在 Chat Completions 上的参数约束。规则与 go-openai 的
// ReasoningValidator 一致：它按模型名在发送前拒绝请求，与走哪个服务无关，所以这里也只看模型名。
type openAIModelLimits struct {
	// completionTokens 表示输出上限只能发 max_completion_tokens。
	completionTokens bool
	// omitSampling 表示服务端只接受默认采样参数，temperature、top_p 都不发送。
	omitSampling bool
}

// openAIReasoningPrefixes 是 o 系列与 gpt-5 的模型名前缀：不接受 max_tokens，采样参数固定为默认值。
var openAIReasoningPrefixes = []string{"o1", "o3", "o4", "gpt-5"}

// openAIGPT6Models 是 SDK 逐个列出的 gpt-6 型号：不接受 max_tokens，开启推理时不接受采样参数。
var openAIGPT6Models = []string{
	openai.GPT6Dot2Astra, openai.GPT6Dot2Sol, openai.GPT6Dot1Sol, openai.GPT6Dot1Terra,
	openai.GPT6Astra, openai.GPT6Sol, openai.GPT6Luna,
}

func openAIModelLimitsFor(model, effort string) openAIModelLimits {
	if slices.Contains(openAIGPT6Models, model) {
		return openAIModelLimits{completionTokens: true, omitSampling: effort != openai.ReasoningEffortNone}
	}
	for _, prefix := range openAIReasoningPrefixes {
		if strings.HasPrefix(model, prefix) {
			return openAIModelLimits{completionTokens: true, omitSampling: true}
		}
	}
	return openAIModelLimits{}
}
